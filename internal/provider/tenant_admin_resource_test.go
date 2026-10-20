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

func adminModel() TenantAdminModel {
	return TenantAdminModel{ID: types.StringUnknown(), TenantID: types.StringValue("tenant:/one"), LinkUserID: types.StringValue("user:/one"), MemberID: types.StringUnknown()}
}
func adminStateModel(member string) TenantAdminModel {
	m := adminModel()
	m.ID = types.StringValue(adminID(m.TenantID.ValueString(), m.LinkUserID.ValueString()))
	m.MemberID = types.StringValue(member)
	return m
}
func TestTenantAdminLifecycleAndImport(t *testing.T) {
	admin := false
	calls := []string{}
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/v3/get-tenant-user":
			if r.Method != http.MethodGet || r.URL.Query().Get("tenantId") != "tenant:/one" || r.URL.Query().Get("linkUserId") != "user:/one" || len(r.URL.Query()) != 2 {
				t.Errorf("lookup %s %s", r.Method, r.URL.RawQuery)
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"member/one","isTenantAdmin":%t}}`, admin)
		case "/api/v3/set-tenant-admin", "/api/v3/delete-tenant-admin":
			if r.Method != http.MethodPost {
				t.Errorf("mutation method %s", r.Method)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			expected := map[string]any{"tenantId": "tenant:/one", "memberIds": []any{"member/one"}}
			if r.URL.Path == "/api/v3/delete-tenant-admin" {
				expected = map[string]any{"tenantId": "tenant:/one", "memberId": "member/one"}
			}
			if !reflect.DeepEqual(body, expected) {
				t.Errorf("payload %s: %v", r.URL.Path, body)
			}
			admin = r.URL.Path == "/api/v3/set-tenant-admin"
			fmt.Fprint(w, `{"statusCode":200,"message":"ok"}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	svc := &TenantAdminResource{client: c}
	ctx := context.Background()
	out := resource.CreateResponse{State: objectState(t, svc, &TenantAdminModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, adminModel())}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	var m TenantAdminModel
	if d := out.State.Get(ctx, &m); d.HasError() || m.ID.ValueString() != adminID("tenant:/one", "user:/one") || m.MemberID.ValueString() != "member/one" {
		t.Fatalf("created %+v %v", m, d)
	}
	imported := resource.ImportStateResponse{State: objectState(t, svc, &TenantAdminModel{})}
	svc.ImportState(ctx, resource.ImportStateRequest{ID: m.ID.ValueString()}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	read := resource.ReadResponse{State: imported.State}
	svc.Read(ctx, resource.ReadRequest{State: imported.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	var im TenantAdminModel
	if d := read.State.Get(ctx, &im); d.HasError() || im.MemberID.ValueString() != "member/one" || im.ID.ValueString() != m.ID.ValueString() {
		t.Fatalf("import read %+v %v", im, d)
	}
	del := resource.DeleteResponse{State: read.State}
	svc.Delete(ctx, resource.DeleteRequest{State: read.State}, &del)
	if del.Diagnostics.HasError() || admin {
		t.Fatalf("delete %v admin=%t", del.Diagnostics, admin)
	}
	gone := resource.ReadResponse{State: read.State}
	svc.Read(ctx, resource.ReadRequest{State: read.State}, &gone)
	if gone.Diagnostics.HasError() || !gone.State.Raw.IsNull() {
		t.Fatalf("not removed: %v", gone.Diagnostics)
	}
	if !reflect.DeepEqual(calls, []string{"/api/v3/get-tenant-user", "/api/v3/set-tenant-admin", "/api/v3/get-tenant-user", "/api/v3/get-tenant-user", "/api/v3/get-tenant-user", "/api/v3/delete-tenant-admin", "/api/v3/get-tenant-user", "/api/v3/get-tenant-user"}) {
		t.Fatalf("calls %v", calls)
	}
}
func TestTenantAdminCreatePreflightAndReadback(t *testing.T) {
	cases := []struct {
		name, pre, mutation, post string
		mutations                 int
	}{
		{"membershipAbsent", `{"statusCode":404}`, ``, ``, 0},
		{"alreadyAdmin", `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"m","isTenantAdmin":true}}`, ``, ``, 0},
		{"preflightFailure", `{"statusCode":500}`, ``, ``, 0},
		{"preflightWrongUser", `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"other","memberId":"m","isTenantAdmin":false}}`, ``, ``, 0},
		{"falseResult", ``, `{"statusCode":200,"data":{"success":false}}`, ``, 1},
		{"failedResult", ``, `{"statusCode":422}`, ``, 1},
		{"unconfirmed", ``, `{"statusCode":200}`, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"m","isTenantAdmin":false}}`, 1},
		{"swappedMember", ``, `{"statusCode":200}`, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"replacement","isTenantAdmin":true}}`, 1},
		{"readbackFailure", ``, `{"statusCode":200}`, `{"statusCode":503}`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reads, mutations := 0, 0
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/set-tenant-admin" {
					mutations++
					fmt.Fprint(w, tc.mutation)
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
					fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"m","isTenantAdmin":false}}`)
				}
			})
			svc := &TenantAdminResource{client: c}
			out := resource.CreateResponse{State: objectState(t, svc, &TenantAdminModel{})}
			svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, adminModel())}, &out)
			var m TenantAdminModel
			if d := out.State.Get(context.Background(), &m); d.HasError() || !out.Diagnostics.HasError() || !m.ID.IsNull() || mutations != tc.mutations {
				t.Fatalf("unsafe create %+v %v mutations=%d", m, out.Diagnostics, mutations)
			}
		})
	}
}
func TestTenantAdminReadMissingVsFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		httpCode int
		body     string
		missing  bool
	}{
		{"http404", 404, `{"statusCode":404}`, true},
		{"business404", 200, `{"statusCode":404}`, true},
		{"revoked", 200, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"m","isTenantAdmin":false}}`, true},
		{"http500", 500, `{"statusCode":500}`, false},
		{"business503", 200, `{"statusCode":503}`, false},
		{"wrongMember", 200, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"replacement","isTenantAdmin":true}}`, false},
		{"missingAdminFlag", 200, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"m"}}`, false},
		{"nullData", 200, `{"statusCode":200,"data":null}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.httpCode); fmt.Fprint(w, tc.body) })
			svc := &TenantAdminResource{client: c}
			st := objectState(t, svc, adminStateModel("m"))
			out := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
			if tc.missing {
				if out.Diagnostics.HasError() || !out.State.Raw.IsNull() {
					t.Fatalf("not removed %v", out.Diagnostics)
				}
			} else if !out.Diagnostics.HasError() || out.State.Raw.IsNull() {
				t.Fatalf("lost state %v", out.Diagnostics)
			}
		})
	}
}
func TestTenantAdminDeleteNeverRevokesWrongMember(t *testing.T) {
	for _, tc := range []struct {
		name, pre, mutation, post string
		mutations                 int
		failure                   bool
	}{
		{"missing", `{"statusCode":404}`, ``, ``, 0, false},
		{"alreadyRevoked", `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"m","isTenantAdmin":false}}`, ``, ``, 0, false},
		{"preflightFailure", `{"statusCode":503}`, ``, ``, 0, true},
		{"wrongMember", `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"replacement","isTenantAdmin":true}}`, ``, ``, 0, true},
		{"falseResult", ``, `{"statusCode":200,"data":{"success":false}}`, ``, 1, true},
		{"failedResult", ``, `{"statusCode":500}`, ``, 1, true},
		{"stillAdmin", ``, `{"statusCode":200}`, ``, 1, true},
		{"readbackFailure", ``, `{"statusCode":200}`, `{"statusCode":503}`, 1, true},
		{"replacementAfterMutation", ``, `{"statusCode":200}`, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"replacement","isTenantAdmin":true}}`, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, mutations := 0, 0
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/delete-tenant-admin" {
					mutations++
					fmt.Fprint(w, tc.mutation)
					return
				}
				if r.URL.Path != "/api/v3/get-tenant-user" {
					t.Errorf("unsafe endpoint %s", r.URL.Path)
					return
				}
				reads++
				if reads == 1 && tc.pre != "" {
					fmt.Fprint(w, tc.pre)
				} else if reads > 1 && tc.post != "" {
					fmt.Fprint(w, tc.post)
				} else {
					fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"tenant:/one","linkUserId":"user:/one","memberId":"m","isTenantAdmin":true}}`)
				}
			})
			svc := &TenantAdminResource{client: c}
			st := objectState(t, svc, adminStateModel("m"))
			out := resource.DeleteResponse{State: st}
			svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
			if out.Diagnostics.HasError() != tc.failure || mutations != tc.mutations {
				t.Fatalf("delete %v mutations=%d", out.Diagnostics, mutations)
			}
		})
	}
}
func TestTenantAdminSchemaAndInvalidImport(t *testing.T) {
	svc := NewTenantAdminResource()
	s := resource.SchemaResponse{}
	svc.Schema(context.Background(), resource.SchemaRequest{}, &s)
	for _, key := range []string{"tenant_id", "link_user_id"} {
		a, ok := s.Schema.Attributes[key].(schema.StringAttribute)
		if !ok || !a.IsRequired() || len(a.PlanModifiers) == 0 {
			t.Errorf("%s not replace-only", key)
		}
	}
	for _, bad := range []string{"bad", adminID("", "u"), adminID("t", "")} {
		out := resource.ImportStateResponse{State: objectState(t, svc, &TenantAdminModel{})}
		svc.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: bad}, &out)
		if !out.Diagnostics.HasError() {
			t.Errorf("accepted %q", bad)
		}
	}
}
