package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func membershipModel() TenantMembershipModel {
	return TenantMembershipModel{ID: types.StringUnknown(), TenantID: types.StringValue("tenant:/one"), LinkUserID: types.StringValue("user:/one"), MemberID: types.StringUnknown()}
}

func TestTenantMembershipLifecycleExactDetach(t *testing.T) {
	present := false
	member := "member/one"
	deletes := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/add-tenant-users":
			if r.Method != http.MethodPost {
				t.Errorf("add method %s", r.Method)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(body, map[string]any{"tenantId": "tenant:/one", "linkUserIds": []any{"user:/one"}}) {
				t.Errorf("add payload %v", body)
			}
			present = true
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		case "/api/v3/get-tenant-user":
			if r.Method != http.MethodGet || r.URL.Query().Get("tenantId") != "tenant:/one" || r.URL.Query().Get("linkUserId") != "user:/one" || len(r.URL.Query()) != 2 {
				t.Errorf("get request %s %s", r.Method, r.URL.RawQuery)
			}
			if !present {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"statusCode":404}`)
				return
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":%q,"isTenantAdmin":false}}`, member)
		case "/api/v3/remove-tenant-users":
			deletes++
			if r.Method != http.MethodPost {
				t.Errorf("remove method %s", r.Method)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(body, map[string]any{"tenantId": "tenant:/one", "memberIds": []any{"member/two"}}) {
				t.Errorf("remove payload %v", body)
			}
			present = false
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	})
	svc := &TenantMembershipResource{client: c}
	ctx := context.Background()
	out := resource.CreateResponse{State: objectState(t, svc, &TenantMembershipModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, membershipModel())}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	var got TenantMembershipModel
	if d := out.State.Get(ctx, &got); d.HasError() {
		t.Fatal(d)
	}
	if got.ID.ValueString() != membershipID("tenant:/one", "user:/one") || got.MemberID.ValueString() != member {
		t.Fatalf("created state %+v", got)
	}
	member = "member/two"
	rd := resource.ReadResponse{State: out.State}
	svc.Read(ctx, resource.ReadRequest{State: out.State}, &rd)
	if rd.Diagnostics.HasError() {
		t.Fatal(rd.Diagnostics)
	}
	if d := rd.State.Get(ctx, &got); d.HasError() || got.MemberID.ValueString() != member {
		t.Fatalf("drift %+v %v", got, d)
	}
	del := resource.DeleteResponse{State: rd.State}
	svc.Delete(ctx, resource.DeleteRequest{State: rd.State}, &del)
	if del.Diagnostics.HasError() || deletes != 1 {
		t.Fatalf("detach %v %d", del.Diagnostics, deletes)
	}
	missing := resource.ReadResponse{State: rd.State}
	svc.Read(ctx, resource.ReadRequest{State: rd.State}, &missing)
	if missing.Diagnostics.HasError() || !missing.State.Raw.IsNull() {
		t.Fatalf("missing %v", missing.Diagnostics)
	}
	del = resource.DeleteResponse{State: rd.State}
	svc.Delete(ctx, resource.DeleteRequest{State: rd.State}, &del)
	if del.Diagnostics.HasError() || deletes != 1 {
		t.Fatalf("idempotent %v %d", del.Diagnostics, deletes)
	}
}

func TestTenantMembershipCreateRefusesExistingMembership(t *testing.T) {
	calls := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-tenant-user" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"owned-elsewhere"}}`)
			return
		}
		calls++
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	})
	svc := &TenantMembershipResource{client: c}
	ctx := context.Background()
	out := resource.CreateResponse{State: objectState(t, svc, &TenantMembershipModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, membershipModel())}, &out)
	if !out.Diagnostics.HasError() || calls != 0 {
		t.Fatalf("adopted existing membership: %v, calls %d", out.Diagnostics, calls)
	}
}

func TestTenantMembershipReadMissingVsErrors(t *testing.T) {
	cases := []struct {
		name     string
		httpCode int
		response string
		missing  bool
	}{
		{"http404", 404, `{"statusCode":404}`, true},
		{"business404", 200, `{"statusCode":404}`, true},
		{"business500", 200, `{"statusCode":500}`, false},
		{"http500", 500, `{"statusCode":500}`, false},
		{"wrongTenant", 200, `{"statusCode":200,"data":{"tenantId":"other","linkUserId":"user:/one","memberId":"m"}}`, false},
		{"wrongUser", 200, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"other","memberId":"m"}}`, false},
		{"missingMember", 200, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one"}}`, false},
		{"nullData", 200, `{"statusCode":200,"data":null}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.httpCode); fmt.Fprint(w, tc.response) })
			svc := &TenantMembershipResource{client: c}
			m := membershipModel()
			m.ID = types.StringValue(membershipID("tenant:/one", "user:/one"))
			m.MemberID = types.StringValue("m")
			st := objectState(t, svc, m)
			out := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
			if tc.missing {
				if out.Diagnostics.HasError() || !out.State.Raw.IsNull() {
					t.Fatalf("not removed: %v", out.Diagnostics)
				}
				return
			}
			if !out.Diagnostics.HasError() || out.State.Raw.IsNull() {
				t.Fatalf("lost state on failure: %v", out.Diagnostics)
			}
		})
	}
}

func TestTenantMembershipCreateRejectsFalseSuccessAndUnconfirmed(t *testing.T) {
	for _, tc := range []struct{ name, add, get string }{
		{"false", `{"statusCode":200,"data":{"success":false}}`, `{"statusCode":404}`},
		{"missingSuccess", `{"statusCode":200}`, `{"statusCode":404}`},
		{"add500", `{"statusCode":500}`, `{"statusCode":404}`},
		{"unconfirmed", `{"statusCode":200,"data":{"success":true}}`, `{"statusCode":404}`},
		{"wrongIdentity", `{"statusCode":200,"data":{"success":true}}`, `{"statusCode":200,"data":{"tenantId":"other","linkUserId":"user:/one","memberId":"m"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-tenant-user" {
					reads++
					if reads == 1 {
						fmt.Fprint(w, `{"statusCode":404}`)
					} else {
						fmt.Fprint(w, tc.get)
					}
					return
				}
				if r.URL.Path != "/api/v3/add-tenant-users" {
					t.Errorf("unexpected %s", r.URL.Path)
				}
				fmt.Fprint(w, tc.add)
			})
			svc := &TenantMembershipResource{client: c}
			ctx := context.Background()
			out := resource.CreateResponse{State: objectState(t, svc, &TenantMembershipModel{})}
			svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, membershipModel())}, &out)
			var m TenantMembershipModel
			if d := out.State.Get(ctx, &m); d.HasError() || !out.Diagnostics.HasError() || !m.ID.IsNull() {
				t.Fatalf("false success persisted: %+v %v", m, out.Diagnostics)
			}
		})
	}
}

func TestTenantMembershipDeleteFailuresKeepState(t *testing.T) {
	for _, tc := range []struct {
		name, pre, remove, post string
		removes                 int
	}{
		{"preflight500", `{"statusCode":500}`, ``, ``, 0},
		{"staleMember", `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"changed"}}`, ``, ``, 0},
		{"falseRemove", ``, `{"statusCode":200,"data":{"success":false}}`, ``, 1},
		{"remove500", ``, `{"statusCode":500}`, ``, 1},
		{"stillPresent", ``, `{"statusCode":200,"data":{"success":true}}`, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"m"}}`, 1},
		{"verify500", ``, `{"statusCode":200,"data":{"success":true}}`, `{"statusCode":500}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, removes := 0, 0
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/remove-tenant-users" {
					removes++
					fmt.Fprint(w, tc.remove)
					return
				}
				if r.URL.Path != "/api/v3/get-tenant-user" {
					t.Errorf("unexpected %s", r.URL.Path)
					return
				}
				reads++
				if reads == 1 && tc.pre != "" {
					fmt.Fprint(w, tc.pre)
				} else if reads > 1 && tc.post != "" {
					fmt.Fprint(w, tc.post)
				} else {
					fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"m"}}`)
				}
			})
			svc := &TenantMembershipResource{client: c}
			m := membershipModel()
			m.ID = types.StringValue(membershipID("tenant:/one", "user:/one"))
			m.MemberID = types.StringValue("m")
			st := objectState(t, svc, m)
			out := resource.DeleteResponse{State: st}
			svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
			if !out.Diagnostics.HasError() || removes != tc.removes {
				t.Fatalf("unsafe detach: %v removes=%d", out.Diagnostics, removes)
			}
		})
	}
}

func TestTenantMembershipReplacementAndInvalidInput(t *testing.T) {
	svc := NewTenantMembershipResource()
	s := resource.SchemaResponse{}
	svc.Schema(context.Background(), resource.SchemaRequest{}, &s)
	for _, name := range []string{"tenant_id", "link_user_id"} {
		attr, ok := s.Schema.Attributes[name].(schema.StringAttribute)
		if !ok || !attr.IsRequired() || len(attr.PlanModifiers) == 0 {
			t.Errorf("%s must require replacement", name)
		}
	}
	calls := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, `{"statusCode":200}`) })
	svc = &TenantMembershipResource{client: c}
	for _, bad := range []TenantMembershipModel{
		{ID: types.StringUnknown(), TenantID: types.StringValue(""), LinkUserID: types.StringValue("user"), MemberID: types.StringUnknown()},
		{ID: types.StringUnknown(), TenantID: types.StringValue("tenant"), LinkUserID: types.StringValue(""), MemberID: types.StringUnknown()},
	} {
		out := resource.CreateResponse{State: objectState(t, svc, &TenantMembershipModel{})}
		svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, bad)}, &out)
		if !out.Diagnostics.HasError() || calls != 0 {
			t.Fatalf("invalid input caused API calls: %v %d", out.Diagnostics, calls)
		}
	}
}

func TestTenantMembershipImportReadPopulatesMember(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/get-tenant-user" || r.URL.Query().Get("tenantId") != "t:./?" || r.URL.Query().Get("linkUserId") != "u:./?" {
			t.Errorf("unexpected import lookup %s %s", r.URL.Path, r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"t:./?","linkUserId":"u:./?","memberId":"member-imported"}}`)
	})
	svc := &TenantMembershipResource{client: c}
	ctx := context.Background()
	id := membershipID("t:./?", "u:./?")
	imported := resource.ImportStateResponse{State: objectState(t, svc, &TenantMembershipModel{})}
	svc.ImportState(ctx, resource.ImportStateRequest{ID: id}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	read := resource.ReadResponse{State: imported.State}
	svc.Read(ctx, resource.ReadRequest{State: imported.State}, &read)
	var m TenantMembershipModel
	if d := read.State.Get(ctx, &m); d.HasError() || read.Diagnostics.HasError() || m.ID.ValueString() != id || m.MemberID.ValueString() != "member-imported" {
		t.Fatalf("import read %+v %v", m, read.Diagnostics)
	}
}

func TestTenantMembershipCreatePreflightFailureDoesNotAdd(t *testing.T) {
	calls := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, `{"statusCode":500}`) })
	svc := &TenantMembershipResource{client: c}
	ctx := context.Background()
	out := resource.CreateResponse{State: objectState(t, svc, &TenantMembershipModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, membershipModel())}, &out)
	var m TenantMembershipModel
	if d := out.State.Get(ctx, &m); d.HasError() || !out.Diagnostics.HasError() || !m.ID.IsNull() || calls != 1 {
		t.Fatalf("failed preflight added membership: %+v %v calls=%d", m, out.Diagnostics, calls)
	}
}

func TestTenantMembershipImport(t *testing.T) {
	svc := NewTenantMembershipResource()
	ctx := context.Background()
	id := membershipID("t:./?", "u:./?")
	out := resource.ImportStateResponse{State: objectState(t, svc, &TenantMembershipModel{})}
	svc.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: id}, &out)
	var m TenantMembershipModel
	if d := out.State.Get(ctx, &m); d.HasError() || out.Diagnostics.HasError() || m.ID.ValueString() != id || m.TenantID.ValueString() != "t:./?" || m.LinkUserID.ValueString() != "u:./?" {
		t.Fatalf("import %+v %v", m, out.Diagnostics)
	}
	for _, bad := range []string{"bad", membershipID("", "user"), membershipID("tenant", "")} {
		out := resource.ImportStateResponse{State: objectState(t, svc, &TenantMembershipModel{})}
		svc.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: bad}, &out)
		if !out.Diagnostics.HasError() {
			t.Fatalf("accepted %q", bad)
		}
	}
}
