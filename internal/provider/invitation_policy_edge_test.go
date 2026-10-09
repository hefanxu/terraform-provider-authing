package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestInvitationPolicyCreateReadbackFailureReportsRecoverableID(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/create-invitation-policy" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"policyId":"policy-1"}}`)
			return
		}
		fmt.Fprint(w, `{"statusCode":500}`)
	})
	svc := &InvitationPolicyResource{client: c}
	out := resource.CreateResponse{State: objectState(t, svc, &InvitationPolicyModel{})}
	svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, invitationModel())}, &out)
	if !out.Diagnostics.HasError() || !strings.Contains(out.Diagnostics.Errors()[0].Detail(), "policy-1") {
		t.Fatalf("created policy ID must be recoverable for import: %v", out.Diagnostics)
	}
}

func TestInvitationPolicyReadMissingVsFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    int
		body    string
		missing bool
	}{
		{"http404", 404, `{"statusCode":404}`, true},
		{"business404", 200, `{"statusCode":404}`, true},
		{"business500", 200, `{"statusCode":500}`, false},
		{"http500", 500, `{"statusCode":500}`, false},
		{"wrongID", 200, `{"statusCode":200,"data":{"policyId":"other","name":"n","enabledIdentifierVerify":false,"enabledInfoFill":true}}`, false},
		{"nullData", 200, `{"statusCode":200,"data":null}`, false},
		{"missingBoolean", 200, `{"statusCode":200,"data":{"policyId":"policy-1","name":"n","enabledInfoFill":false}}`, false},
		{"malformed", 200, `oops`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); fmt.Fprint(w, tc.body) })
			svc := &InvitationPolicyResource{client: c}
			m := invitationModel()
			m.ID = types.StringValue("policy-1")
			st := objectState(t, svc, m)
			out := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
			if tc.missing {
				if out.Diagnostics.HasError() || !out.State.Raw.IsNull() {
					t.Fatalf("not removed %v", out.Diagnostics)
				}
			} else if !out.Diagnostics.HasError() || out.State.Raw.IsNull() {
				t.Fatalf("lost state on failure %v", out.Diagnostics)
			}
		})
	}
}
func TestInvitationPolicyNullIsNotEmpty(t *testing.T) {
	var body map[string]any
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/create-invitation-policy":
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"policyId":"policy-1"}}`)
		case "/api/v3/get-invitation-policy":
			fmt.Fprint(w, `{"statusCode":200,"data":{"policyId":"policy-1","name":"Initial","enabledIdentifierVerify":false,"enabledInfoFill":false,"registerInfoFillMsg":null}}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	svc := &InvitationPolicyResource{client: c}
	m := invitationModel()
	m.EnabledInfoFill = types.BoolNull()
	m.EnabledIdentifierVerify = types.BoolNull()
	m.RegisterInfoFillMsg = types.StringNull()
	out := resource.CreateResponse{State: objectState(t, svc, &InvitationPolicyModel{})}
	svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, m)}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	if len(body) != 1 || body["name"] != "Initial" {
		t.Fatalf("nulls serialized: %v", body)
	}
	var got InvitationPolicyModel
	if d := out.State.Get(context.Background(), &got); d.HasError() || !got.RegisterInfoFillMsg.IsNull() || got.EnabledInfoFill.IsNull() || got.EnabledInfoFill.ValueBool() {
		t.Fatalf("null versus false %+v %v", got, d)
	}
}
func TestInvitationPolicyFailedMutationsPreserveState(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint, answer string
		status                 int
	}{
		{"createBadStatus", "/api/v3/create-invitation-policy", `{"statusCode":500}`, 200},
		{"createMissingID", "/api/v3/create-invitation-policy", `{"statusCode":200,"data":{}}`, 200},
		{"createUnconfirmed", "/api/v3/get-invitation-policy", `{"statusCode":404}`, 200},
		{"updateBadStatus", "/api/v3/update-invitation-policy", `{"statusCode":500}`, 200},
		{"updateUnconfirmed", "/api/v3/get-invitation-policy", `{"statusCode":404}`, 200},
		{"deleteFalse", "/api/v3/delete-invitation-policies-batch", `{"statusCode":200,"data":{"success":false}}`, 200},
		{"deleteMissingSuccess", "/api/v3/delete-invitation-policies-batch", `{"statusCode":200,"data":{}}`, 200},
		{"deleteStillPresent", "/api/v3/delete-invitation-policies-batch", `{"statusCode":200,"data":{"success":true}}`, 200},
		{"deletePreflight500", "/api/v3/get-invitation-policy", `{"statusCode":500}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deletes := 0
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/delete-invitation-policies-batch" {
					deletes++
				}
				if r.URL.Path == tc.endpoint {
					fmt.Fprint(w, tc.answer)
					return
				}
				switch r.URL.Path {
				case "/api/v3/create-invitation-policy", "/api/v3/update-invitation-policy":
					fmt.Fprint(w, `{"statusCode":200,"data":{"policyId":"policy-1"}}`)
				case "/api/v3/get-invitation-policy":
					fmt.Fprint(w, `{"statusCode":200,"data":{"policyId":"policy-1","name":"Initial","enabledIdentifierVerify":false,"enabledInfoFill":true,"registerInfoFillMsg":""}}`)
				default:
					t.Errorf("unexpected %s", r.URL.Path)
				}
			})
			svc := &InvitationPolicyResource{client: c}
			ctx := context.Background()
			m := invitationModel()
			if len(tc.name) >= 6 && tc.name[:6] == "create" {
				out := resource.CreateResponse{State: objectState(t, svc, &InvitationPolicyModel{})}
				svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, m)}, &out)
				var got InvitationPolicyModel
				if d := out.State.Get(ctx, &got); d.HasError() || !out.Diagnostics.HasError() || !got.ID.IsNull() {
					t.Fatalf("create persisted %+v %v", got, out.Diagnostics)
				}
			} else if len(tc.name) >= 6 && tc.name[:6] == "update" {
				m.ID = types.StringValue("policy-1")
				prior := objectState(t, svc, m)
				m.Name = types.StringValue("Updated")
				out := resource.UpdateResponse{State: prior}
				svc.Update(ctx, resource.UpdateRequest{State: prior, Plan: objectPlan(t, svc, m)}, &out)
				if !out.Diagnostics.HasError() || out.State.Raw.IsNull() {
					t.Fatalf("update failure %v", out.Diagnostics)
				}
			} else {
				m.ID = types.StringValue("policy-1")
				prior := objectState(t, svc, m)
				out := resource.DeleteResponse{State: prior}
				svc.Delete(ctx, resource.DeleteRequest{State: prior}, &out)
				if !out.Diagnostics.HasError() || (tc.name == "deletePreflight500" && deletes != 0) {
					t.Fatalf("delete failure %v deletes=%d", out.Diagnostics, deletes)
				}
			}
		})
	}
}
func TestInvitationPolicyCreateRejectsUnconfirmedFields(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/create-invitation-policy":
			fmt.Fprint(w, `{"statusCode":200,"data":{"policyId":"policy-1"}}`)
		case "/api/v3/get-invitation-policy":
			fmt.Fprint(w, `{"statusCode":200,"data":{"policyId":"policy-1","name":"Different","enabledIdentifierVerify":false,"enabledInfoFill":true,"registerInfoFillMsg":""}}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	svc := &InvitationPolicyResource{client: c}
	out := resource.CreateResponse{State: objectState(t, svc, &InvitationPolicyModel{})}
	svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, invitationModel())}, &out)
	if !out.Diagnostics.HasError() {
		t.Fatal("persisted a policy whose name was not confirmed")
	}
}
func TestInvitationPolicyImportRejectsEmptyID(t *testing.T) {
	svc := NewInvitationPolicyResource()
	out := resource.ImportStateResponse{State: objectState(t, svc, &InvitationPolicyModel{})}
	svc.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: ""}, &out)
	if !out.Diagnostics.HasError() {
		t.Fatal("accepted empty ID")
	}
}
