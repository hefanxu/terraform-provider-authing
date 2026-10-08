package provider

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"net/http"
	"strings"
	"testing"
)

func TestInvitationInviteeCreateRejectsNonRoundtrippingEmail(t *testing.T) {
	lists := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/list-invitation-invitees":
			lists++
			if lists == 1 {
				fmt.Fprint(w, `{"statusCode":200,"data":{"list":[]}}`)
			} else {
				fmt.Fprintf(w, `{"statusCode":200,"data":{"list":[%s]}}`, inviteeRow("roster-1", "invitee-1", "Ada", "ada@example.com", ""))
			}
		case "/api/v3/create-invitation-invitee":
			fmt.Fprintf(w, `{"statusCode":200,"data":%s}`, inviteeRow("roster-1", "invitee-1", "Ada", "ada@example.com", ""))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	svc := &InvitationInviteeResource{client: c}
	resp := resource.CreateResponse{State: objectState(t, svc, &InvitationInviteeModel{})}
	svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, inviteeModel("roster-1", "Ada", "ADA@example.com", ""))}, &resp)
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics[0].Detail(), invitationInviteeID("roster-1", "invitee-1")) {
		t.Fatalf("non-roundtripping email accepted or no recovery ID: %v", resp.Diagnostics)
	}
}
