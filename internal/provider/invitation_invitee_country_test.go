package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"net/http"
	"testing"
)

func TestInvitationInviteeUpdatePreservesRemotePhoneCountryCode(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/list-invitation-invitees":
			fmt.Fprint(w, `{"statusCode":200,"data":{"list":[{"rosterId":"roster-1","inviteeId":"invitee-1","name":"Ada","email":"ada@example.com","phone":"123","phoneCountryCode":"+44"}]}}`)
		case "/api/v3/edit-invitation-invitee":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["phoneCountryCode"] != "+44" {
				t.Errorf("lost remote phone country code: %v", body)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	svc := &InvitationInviteeResource{client: c}
	old := inviteeModel("roster-1", "Ada", "ada@example.com", "123")
	old.ID = types.StringValue(invitationInviteeID("roster-1", "invitee-1"))
	old.InviteeID = types.StringValue("invitee-1")
	state := objectState(t, svc, &old)
	plan := old
	plan.Name = types.StringValue("Updated")
	resp := resource.UpdateResponse{State: state}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: objectPlan(t, svc, plan), State: state}, &resp)
	// Readback remains Ada, so update must fail; preservation is asserted on the wire.
	if !resp.Diagnostics.HasError() {
		t.Fatal("unconfirmed update accepted")
	}
}
