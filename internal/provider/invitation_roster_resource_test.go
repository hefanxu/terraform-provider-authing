package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func rosterModel(name, policy string) InvitationRosterModel {
	m := InvitationRosterModel{ID: types.StringUnknown(), Name: types.StringValue(name), PolicyID: types.StringNull()}
	if policy != "" {
		m.PolicyID = types.StringValue(policy)
	}
	return m
}

func TestInvitationRosterLifecycleAndImport(t *testing.T) {
	ctx := context.Background()
	present, name, policy, deletes := false, "", "", 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-invitation-roster" {
			if r.Method != http.MethodGet || r.URL.Query().Get("rosterId") != "roster-1" || r.URL.Query().Get("withAssignedPolicy") != "true" || len(r.URL.Query()) != 2 {
				t.Errorf("GET %s %s", r.Method, r.URL.RawQuery)
			}
			if !present {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"statusCode":404}`)
				return
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"rosterId":"roster-1","name":%q,"policyId":%q}}`, name, policy)
			return
		}
		var body map[string]any
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Errorf("invalid POST %s", r.URL.Path)
			return
		}
		switch r.URL.Path {
		case "/api/v3/create-invitation-roster":
			if !reflect.DeepEqual(body, map[string]any{"name": "Initial"}) {
				t.Errorf("create payload %v", body)
			}
			present, name = true, "Initial"
			fmt.Fprint(w, `{"statusCode":200,"data":{"rosterId":"roster-1","name":"Initial"}}`)
		case "/api/v3/update-invitation-roster":
			if !reflect.DeepEqual(body, map[string]any{"rosterId": "roster-1", "name": "Updated", "policyId": "policy-1"}) {
				t.Errorf("update payload %v", body)
			}
			name, policy = "Updated", "policy-1"
			fmt.Fprint(w, `{"statusCode":200,"data":{"rosterId":"roster-1","name":"Updated","policyId":"policy-1"}}`)
		case "/api/v3/update-invitation-roster-batch-unbind-policy":
			if !reflect.DeepEqual(body, map[string]any{"policyId": "policy-1", "rosterIds": []any{"roster-1"}}) {
				t.Errorf("unbind payload %v", body)
			}
			policy = ""
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		case "/api/v3/delete-invitation-roster":
			deletes++
			if !reflect.DeepEqual(body, map[string]any{"id": "roster-1"}) {
				t.Errorf("delete payload %v", body)
			}
			present = false
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	})
	svc := &InvitationRosterResource{client: c}
	created := resource.CreateResponse{State: objectState(t, svc, &InvitationRosterModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, rosterModel("Initial", ""))}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	var got InvitationRosterModel
	if d := created.State.Get(ctx, &got); d.HasError() || got.ID.ValueString() != "roster-1" || got.Name.ValueString() != "Initial" || !got.PolicyID.IsNull() {
		t.Fatalf("create %+v %v", got, d)
	}
	planned := rosterModel("Updated", "policy-1")
	planned.ID = got.ID
	updated := resource.UpdateResponse{State: created.State}
	svc.Update(ctx, resource.UpdateRequest{Plan: objectPlan(t, svc, planned), State: created.State}, &updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	if d := updated.State.Get(ctx, &got); d.HasError() || got.PolicyID.ValueString() != "policy-1" {
		t.Fatalf("update %+v %v", got, d)
	}
	name = "Drift"
	read := resource.ReadResponse{State: updated.State}
	svc.Read(ctx, resource.ReadRequest{State: updated.State}, &read)
	if d := read.State.Get(ctx, &got); d.HasError() || read.Diagnostics.HasError() || got.Name.ValueString() != "Drift" {
		t.Fatalf("drift %+v %v %v", got, d, read.Diagnostics)
	}
	imported := resource.ImportStateResponse{State: objectState(t, svc, &InvitationRosterModel{})}
	svc.ImportState(ctx, resource.ImportStateRequest{ID: "roster-1"}, &imported)
	ir := resource.ReadResponse{State: imported.State}
	svc.Read(ctx, resource.ReadRequest{State: imported.State}, &ir)
	if d := ir.State.Get(ctx, &got); d.HasError() || ir.Diagnostics.HasError() || got.Name.ValueString() != "Drift" || got.PolicyID.ValueString() != "policy-1" {
		t.Fatalf("import %+v %v %v", got, d, ir.Diagnostics)
	}
	planned = got
	planned.PolicyID = types.StringNull()
	unbound := resource.UpdateResponse{State: ir.State}
	svc.Update(ctx, resource.UpdateRequest{Plan: objectPlan(t, svc, planned), State: ir.State}, &unbound)
	if unbound.Diagnostics.HasError() {
		t.Fatal(unbound.Diagnostics)
	}
	if d := unbound.State.Get(ctx, &got); d.HasError() || !got.PolicyID.IsNull() {
		t.Fatalf("unbind %+v %v", got, d)
	}
	deleted := resource.DeleteResponse{State: unbound.State}
	svc.Delete(ctx, resource.DeleteRequest{State: unbound.State}, &deleted)
	if deleted.Diagnostics.HasError() || deletes != 1 {
		t.Fatalf("delete %v %d", deleted.Diagnostics, deletes)
	}
	svc.Delete(ctx, resource.DeleteRequest{State: unbound.State}, &deleted)
	if deleted.Diagnostics.HasError() || deletes != 1 {
		t.Fatalf("idempotent %v %d", deleted.Diagnostics, deletes)
	}
}

func TestInvitationRosterCreateReadbackFailureReportsRecoverableID(t *testing.T) {
	ctx := context.Background()
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/create-invitation-roster":
			fmt.Fprint(w, `{"statusCode":200,"data":{"rosterId":"recover-me"}}`)
		case "/api/v3/get-invitation-roster":
			fmt.Fprint(w, `{"statusCode":503}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	svc := &InvitationRosterResource{client: c}
	resp := resource.CreateResponse{State: objectState(t, svc, &InvitationRosterModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, rosterModel("Initial", ""))}, &resp)
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics[0].Detail(), "recover-me") || !strings.Contains(resp.Diagnostics[0].Detail(), "Import") {
		t.Fatalf("missing recovery ID: %v", resp.Diagnostics)
	}
}

func TestInvitationRosterReadRejectsMalformedAndTransientResponses(t *testing.T) {
	for _, raw := range []string{`{"statusCode":200,"data":{"name":"Initial"}}`, `{"statusCode":200,"data":{"rosterId":"other","name":"Initial"}}`, `{"statusCode":200,"data":{"rosterId":"roster-1"}}`, `{"statusCode":503}`, `not-json`} {
		t.Run(raw, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, raw) })
			svc := &InvitationRosterResource{client: c}
			m := rosterModel("Initial", "")
			m.ID = types.StringValue("roster-1")
			state := objectState(t, svc, &m)
			resp := resource.ReadResponse{State: state}
			svc.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
			if !resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
				t.Fatalf("read silently lost state: %v", resp.Diagnostics)
			}
		})
	}
}

func TestInvitationRosterDeleteRejectsFalseSuccess(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-invitation-roster" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"rosterId":"roster-1","name":"Initial"}}`)
			return
		}
		if r.URL.Path != "/api/v3/delete-invitation-roster" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":false}}`)
	})
	svc := &InvitationRosterResource{client: c}
	m := rosterModel("Initial", "")
	m.ID = types.StringValue("roster-1")
	state := objectState(t, svc, &m)
	resp := resource.DeleteResponse{State: state}
	svc.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics[0].Detail(), "success") {
		t.Fatalf("false success accepted or unclear: %v", resp.Diagnostics)
	}
}

func TestInvitationRosterCreatePolicyRequiresGETConfirmation(t *testing.T) {
	ctx := context.Background()
	updates := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/create-invitation-roster":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if !reflect.DeepEqual(body, map[string]any{"name": "Initial"}) {
				t.Errorf("create payload %v", body)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"rosterId":"roster-1","name":"Initial"}}`)
		case "/api/v3/update-invitation-roster":
			updates++
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if !reflect.DeepEqual(body, map[string]any{"rosterId": "roster-1", "policyId": "policy-1"}) {
				t.Errorf("association payload %v", body)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"rosterId":"roster-1","policyId":"policy-1"}}`)
		case "/api/v3/get-invitation-roster":
			fmt.Fprint(w, `{"statusCode":200,"data":{"rosterId":"roster-1","name":"Initial"}}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	})
	svc := &InvitationRosterResource{client: c}
	resp := resource.CreateResponse{State: objectState(t, svc, &InvitationRosterModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, rosterModel("Initial", "policy-1"))}, &resp)
	if updates != 1 || !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics[0].Detail(), "roster-1") {
		t.Fatalf("unconfirmed policy persisted %v, updates %d", resp.Diagnostics, updates)
	}
}

func TestInvitationRosterUpdateRejectsChangedRemotePolicy(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/get-invitation-roster" {
			t.Errorf("unsafe update %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"rosterId":"roster-1","name":"Initial","policyId":"other-policy"}}`)
	})
	svc := &InvitationRosterResource{client: c}
	old := rosterModel("Initial", "policy-1")
	old.ID = types.StringValue("roster-1")
	state := objectState(t, svc, &old)
	planned := old
	planned.PolicyID = types.StringNull()
	resp := resource.UpdateResponse{State: state}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: objectPlan(t, svc, planned), State: state}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("unbound changed remote policy")
	}
}

func TestInvitationRosterReadRejectsConflictingPolicyIDs(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"statusCode":200,"data":{"rosterId":"roster-1","name":"Initial","policyId":"other","assignedPolicy":{"policyId":"policy-1"}}}`)
	})
	svc := &InvitationRosterResource{client: c}
	m := rosterModel("Initial", "policy-1")
	m.ID = types.StringValue("roster-1")
	state := objectState(t, svc, &m)
	resp := resource.ReadResponse{State: state}
	svc.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	if !resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
		t.Fatalf("conflicting policy IDs accepted: %v", resp.Diagnostics)
	}
}

func TestInvitationRosterReadNestedAssignedPolicy(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/get-invitation-roster" || r.URL.Query().Get("withAssignedPolicy") != "true" {
			t.Errorf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"rosterId":"roster-1","name":"Initial","assignedPolicy":{"policyId":"policy-1"}}}`)
	})
	svc := &InvitationRosterResource{client: c}
	m := rosterModel("Initial", "policy-1")
	m.ID = types.StringValue("roster-1")
	state := objectState(t, svc, &m)
	resp := resource.ReadResponse{State: state}
	svc.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	var got InvitationRosterModel
	if d := resp.State.Get(context.Background(), &got); d.HasError() || resp.Diagnostics.HasError() || got.PolicyID.ValueString() != "policy-1" {
		t.Fatalf("nested policy lost: %+v, %v, %v", got, d, resp.Diagnostics)
	}
}
