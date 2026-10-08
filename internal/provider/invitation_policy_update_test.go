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

func TestInvitationPolicyUpdateFalseAndExplicitEmpty(t *testing.T) {
	var payload map[string]any
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/update-invitation-policy":
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"policyId":"policy-1"}}`)
		case "/api/v3/get-invitation-policy":
			fmt.Fprint(w, `{"statusCode":200,"data":{"policyId":"policy-1","name":"Initial","enabledIdentifierVerify":false,"enabledInfoFill":false,"registerInfoFillMsg":""}}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	svc := &InvitationPolicyResource{client: c}
	prior := invitationModel()
	prior.ID = types.StringValue("policy-1")
	prior.EnabledIdentifierVerify = types.BoolValue(true)
	prior.RegisterInfoFillMsg = types.StringValue("old")
	planned := prior
	planned.EnabledIdentifierVerify = types.BoolValue(false)
	planned.EnabledInfoFill = types.BoolValue(false)
	planned.RegisterInfoFillMsg = types.StringValue("")
	st := objectState(t, svc, prior)
	out := resource.UpdateResponse{State: st}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: objectPlan(t, svc, planned), State: st}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	want := map[string]any{"policyId": "policy-1", "enabledIdentifierVerify": false, "enabledInfoFill": false, "registerInfoFillMsg": ""}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("update payload %v want %v", payload, want)
	}
}
func TestInvitationPolicyRejectsInvalidNameBeforeWrite(t *testing.T) {
	calls := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, `{"statusCode":200}`) })
	svc := &InvitationPolicyResource{client: c}
	for _, name := range []string{"", strings.Repeat("x", 201)} {
		m := invitationModel()
		m.Name = types.StringValue(name)
		out := resource.CreateResponse{State: objectState(t, svc, &InvitationPolicyModel{})}
		svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, m)}, &out)
		if !out.Diagnostics.HasError() || calls != 0 {
			t.Fatalf("invalid name %q caused write: %v calls=%d", name, out.Diagnostics, calls)
		}
	}
}
