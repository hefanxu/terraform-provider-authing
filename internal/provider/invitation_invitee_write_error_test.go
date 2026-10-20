package provider

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"net/http"
	"testing"
)

func TestInvitationInviteeUpdateRejectsFailedWrite(t *testing.T) {
	for _, failure := range []string{`{"statusCode":403}`, `{"statusCode":200,"data":{"success":false}}`, `{"statusCode":503}`} {
		t.Run(failure, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/list-invitation-invitees" {
					fmt.Fprintf(w, `{"statusCode":200,"data":{"list":[%s]}}`, inviteeRow("roster-1", "invitee-1", "Ada", "ada@example.com", ""))
					return
				}
				if r.URL.Path != "/api/v3/edit-invitation-invitee" {
					t.Errorf("unexpected %s", r.URL.Path)
				}
				fmt.Fprint(w, failure)
			})
			svc := &InvitationInviteeResource{client: c}
			old := inviteeModel("roster-1", "Ada", "ada@example.com", "")
			old.ID = types.StringValue(invitationInviteeID("roster-1", "invitee-1"))
			old.InviteeID = types.StringValue("invitee-1")
			state := objectState(t, svc, &old)
			plan := old
			plan.Name = types.StringValue("Changed")
			resp := resource.UpdateResponse{State: state}
			svc.Update(context.Background(), resource.UpdateRequest{Plan: objectPlan(t, svc, plan), State: state}, &resp)
			if !resp.Diagnostics.HasError() {
				t.Fatalf("accepted failed write: %s", failure)
			}
			var got InvitationInviteeModel
			if d := resp.State.Get(context.Background(), &got); d.HasError() || got.Name.ValueString() != "Ada" {
				t.Fatalf("lost prior state %+v %v", got, d)
			}
		})
	}
}
