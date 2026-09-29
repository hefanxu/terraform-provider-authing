package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func assignmentModel() DataPolicyAssignmentModel {
	return DataPolicyAssignmentModel{ID: types.StringUnknown(), PolicyID: types.StringValue("policy:one"), TargetType: types.StringValue("USER"), TargetID: types.StringValue("user/one")}
}
func TestDataPolicyAssignmentLifecycle(t *testing.T) {
	authorized := false
	calls := []string{}
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/v3/authorize-data-policies":
			if r.Method != http.MethodPost {
				t.Error(r.Method)
			}
			var b struct {
				PolicyIDs  []string `json:"policyIds"`
				TargetList []struct {
					ID   string `json:"id"`
					Type string `json:"type"`
				} `json:"targetList"`
			}
			if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
				t.Error(err)
			}
			if len(b.PolicyIDs) != 1 || b.PolicyIDs[0] != "policy:one" || len(b.TargetList) != 1 || b.TargetList[0].ID != "user/one" || b.TargetList[0].Type != "USER" {
				t.Errorf("authorize payload: %+v", b)
			}
			authorized = true
			fmt.Fprint(w, `{"statusCode":200}`)
		case "/api/v3/list-data-policy-targets":
			q := r.URL.Query()
			if r.Method != http.MethodGet || q.Get("policyId") != "policy:one" || q.Get("page") != "1" || q.Get("limit") != "50" {
				t.Errorf("list query: %s %s", r.Method, r.URL.RawQuery)
			}
			if authorized {
				fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":2,"list":[{"targetIdentifier":"other","targetType":"USER"},{"targetIdentifier":"user/one","targetType":"USER"}]}}`)
			} else {
				fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":1,"list":[{"targetIdentifier":"other","targetType":"USER"}]}}`)
			}
		case "/api/v3/revoke-data-policy":
			var b map[string]string
			if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
				t.Error(err)
			}
			if b["policyId"] != "policy:one" || b["targetIdentifier"] != "user/one" || b["targetType"] != "USER" || len(b) != 3 {
				t.Errorf("revoke payload: %v", b)
			}
			authorized = false
			fmt.Fprint(w, `{"statusCode":200}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	svc := &DataPolicyAssignmentResource{client: c}
	ctx := context.Background()
	plan := objectPlan(t, svc, assignmentModel())
	st := objectState(t, svc, &DataPolicyAssignmentModel{})
	create := resource.CreateResponse{State: st}
	svc.Create(ctx, resource.CreateRequest{Plan: plan}, &create)
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	var got DataPolicyAssignmentModel
	if d := create.State.Get(ctx, &got); d.HasError() {
		t.Fatal(d)
	}
	if got.ID.ValueString() == "" || got.ID.IsUnknown() {
		t.Fatal("missing ID")
	}
	read := resource.ReadResponse{State: create.State}
	svc.Read(ctx, resource.ReadRequest{State: create.State}, &read)
	if read.Diagnostics.HasError() || read.State.Raw.IsNull() {
		t.Fatalf("read: %v", read.Diagnostics)
	}
	del := resource.DeleteResponse{State: read.State}
	svc.Delete(ctx, resource.DeleteRequest{State: read.State}, &del)
	if del.Diagnostics.HasError() {
		t.Fatal(del.Diagnostics)
	}
	missing := resource.ReadResponse{State: read.State}
	svc.Read(ctx, resource.ReadRequest{State: read.State}, &missing)
	if missing.Diagnostics.HasError() || !missing.State.Raw.IsNull() {
		t.Fatalf("missing: %v", missing.Diagnostics)
	}
	del = resource.DeleteResponse{State: read.State}
	svc.Delete(ctx, resource.DeleteRequest{State: read.State}, &del)
	if del.Diagnostics.HasError() {
		t.Fatal(del.Diagnostics)
	}
	for _, part := range []string{"POST /api/v3/authorize-data-policies", "POST /api/v3/revoke-data-policy"} {
		if !strings.Contains(strings.Join(calls, ";"), part) {
			t.Fatal(calls)
		}
	}
}

func TestDataPolicyAssignmentPaginationAndFailures(t *testing.T) {
	status := 200
	page := 0
	present := true
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/list-data-policy-targets" {
			t.Errorf("unexpected %s", r.URL.Path)
			return
		}
		page++
		if status != 200 {
			fmt.Fprintf(w, `{"statusCode":%d,"message":"failure"}`, status)
			return
		}
		n := r.URL.Query().Get("page")
		if n == "1" {
			fmt.Fprintf(w, `{"statusCode":200,"data":{"totalCount":51,"list":[%s]}}`, strings.TrimSuffix(strings.Repeat(`{"targetIdentifier":"other","targetType":"USER"},`, 50), ","))
			return
		}
		if n != "2" {
			t.Errorf("unexpected page %s", n)
		}
		if present {
			fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":51,"list":[{"targetIdentifier":"user/one","targetType":"USER"}]}}`)
		} else {
			fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":51,"list":[{"targetIdentifier":"other","targetType":"USER"}]}}`)
		}
	})
	svc := &DataPolicyAssignmentResource{client: c}
	m := assignmentModel()
	m.ID = types.StringValue(assignmentID("policy:one", "USER", "user/one"))
	st := objectState(t, svc, m)
	read := func() resource.ReadResponse {
		t.Helper()
		out := resource.ReadResponse{State: st}
		svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
		return out
	}
	out := read()
	if out.Diagnostics.HasError() || out.State.Raw.IsNull() || page != 2 {
		t.Fatalf("page read: %v %d", out.Diagnostics, page)
	}
	present = false
	page = 0
	out = read()
	if out.Diagnostics.HasError() || !out.State.Raw.IsNull() || page != 2 {
		t.Fatalf("absent read: %v %d", out.Diagnostics, page)
	}
	for _, code := range []int{404, 500} {
		status = code
		page = 0
		out = read()
		if !out.Diagnostics.HasError() || out.State.Raw.IsNull() {
			t.Fatalf("error %d lost state: %v", code, out.Diagnostics)
		}
	}
}

func TestDataPolicyAssignmentImport(t *testing.T) {
	svc := NewDataPolicyAssignmentResource()
	ctx := context.Background()
	id := assignmentID("p:./?", "ROLE", "id:./?")
	out := resource.ImportStateResponse{State: objectState(t, svc, &DataPolicyAssignmentModel{})}
	svc.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: id}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	var m DataPolicyAssignmentModel
	if d := out.State.Get(ctx, &m); d.HasError() {
		t.Fatal(d)
	}
	if m.ID.ValueString() != id || m.PolicyID.ValueString() != "p:./?" || m.TargetType.ValueString() != "ROLE" || m.TargetID.ValueString() != "id:./?" {
		t.Fatalf("import: %+v", m)
	}
	for _, bad := range []string{"garbage", assignmentID("p", "INVALID", "id"), assignmentID("", "USER", "id")} {
		out := resource.ImportStateResponse{State: objectState(t, svc, &DataPolicyAssignmentModel{})}
		svc.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: bad}, &out)
		if !out.Diagnostics.HasError() {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestDataPolicyAssignmentCreateRejectsUnconfirmedAuthorization(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/list-data-policy-targets" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":1,"list":[{"targetIdentifier":"user/one","targetType":"GROUP"}]}}`)
			return
		}
		fmt.Fprint(w, `{"statusCode":200}`)
	})
	svc := &DataPolicyAssignmentResource{client: c}
	ctx := context.Background()
	out := resource.CreateResponse{State: objectState(t, svc, &DataPolicyAssignmentModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, assignmentModel())}, &out)
	var got DataPolicyAssignmentModel
	if d := out.State.Get(ctx, &got); d.HasError() || !out.Diagnostics.HasError() || !got.ID.IsNull() {
		t.Fatalf("unconfirmed create wrote state: %+v %v", got, out.Diagnostics)
	}
}

func TestDataPolicyAssignmentDeleteOnlyRevokesExactMembership(t *testing.T) {
	status := 500
	calls := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/list-data-policy-targets":
			fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":2,"list":[{"targetIdentifier":"user/one","targetType":"GROUP"},{"targetIdentifier":"user/one","targetType":"USER"}]}}`)
		case "/api/v3/revoke-data-policy":
			calls++
			fmt.Fprintf(w, `{"statusCode":%d,"message":"failure"}`, status)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	svc := &DataPolicyAssignmentResource{client: c}
	m := assignmentModel()
	m.ID = types.StringValue(assignmentID("policy:one", "USER", "user/one"))
	st := objectState(t, svc, m)
	out := resource.DeleteResponse{State: st}
	svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
	if !out.Diagnostics.HasError() || calls != 1 {
		t.Fatalf("revoke failure not reported: %v, calls %d", out.Diagnostics, calls)
	}
	status = 200
	out = resource.DeleteResponse{State: st}
	svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
	if !out.Diagnostics.HasError() {
		t.Fatal("revoke returning success but leaving membership must fail")
	}
}

func TestDataPolicyAssignmentReplacementSchema(t *testing.T) {
	svc := NewDataPolicyAssignmentResource()
	s := resource.SchemaResponse{}
	svc.Schema(context.Background(), resource.SchemaRequest{}, &s)
	for _, key := range []string{"policy_id", "target_type", "target_id"} {
		attr, ok := s.Schema.Attributes[key].(schema.StringAttribute)
		if !ok || !attr.IsRequired() || len(attr.PlanModifiers) == 0 {
			t.Fatalf("%s must be required and force replacement", key)
		}
	}
}

func TestDataPolicyAssignmentRejectsInvalidCreateWithoutAPICall(t *testing.T) {
	calls := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"statusCode":200}`)
	})
	svc := &DataPolicyAssignmentResource{client: c}
	for _, m := range []DataPolicyAssignmentModel{
		{ID: types.StringUnknown(), PolicyID: types.StringValue(""), TargetType: types.StringValue("USER"), TargetID: types.StringValue("id")},
		{ID: types.StringUnknown(), PolicyID: types.StringValue("policy"), TargetType: types.StringValue("INVALID"), TargetID: types.StringValue("id")},
		{ID: types.StringUnknown(), PolicyID: types.StringValue("policy"), TargetType: types.StringValue("USER"), TargetID: types.StringValue("")},
	} {
		out := resource.CreateResponse{State: objectState(t, svc, &DataPolicyAssignmentModel{})}
		svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, m)}, &out)
		if !out.Diagnostics.HasError() || calls != 0 {
			t.Fatalf("invalid create was sent: %v calls=%d", out.Diagnostics, calls)
		}
	}
}

func TestDataPolicyAssignmentCreateAndDeleteErrors(t *testing.T) {
	code := 500
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"statusCode":%d,"message":"failure"}`, code)
	})
	svc := &DataPolicyAssignmentResource{client: c}
	ctx := context.Background()
	st := objectState(t, svc, &DataPolicyAssignmentModel{})
	out := resource.CreateResponse{State: st}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, assignmentModel())}, &out)
	var unchanged DataPolicyAssignmentModel
	if d := out.State.Get(ctx, &unchanged); d.HasError() || !out.Diagnostics.HasError() || !unchanged.ID.IsNull() {
		t.Fatalf("false create: %v %+v", out.Diagnostics, unchanged)
	}
	m := assignmentModel()
	m.ID = types.StringValue(assignmentID("policy:one", "USER", "user/one"))
	st = objectState(t, svc, m)
	del := resource.DeleteResponse{State: st}
	svc.Delete(ctx, resource.DeleteRequest{State: st}, &del)
	if !del.Diagnostics.HasError() {
		t.Fatal("lost delete failure")
	}
}
