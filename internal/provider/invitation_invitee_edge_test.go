package provider

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"net/http"
	"strings"
	"testing"
)

func TestInvitationInviteeListRejectsIncompleteAndUnsafePages(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"transient", `{"statusCode":503}`}, {"notfound_collection", `{"statusCode":404}`}, {"malformed", `not-json`},
		{"missing_list", `{"statusCode":200,"data":{}}`},
		{"missing_id", `{"statusCode":200,"data":{"list":[{"rosterId":"roster-1","name":"A","email":"a@example.com"}]}}`},
		{"cross_roster", `{"statusCode":200,"data":{"list":[{"rosterId":"other","inviteeId":"invitee-1","name":"A","email":"a@example.com"}]}}`},
		{"short_before_total", `{"statusCode":200,"data":{"totalCount":2,"list":[{"rosterId":"roster-1","inviteeId":"invitee-1","name":"A","email":"a@example.com"}]}}`},
		{"duplicate_id", `{"statusCode":200,"data":{"list":[{"rosterId":"roster-1","inviteeId":"invitee-1","name":"A","email":"a@example.com"},{"rosterId":"roster-1","inviteeId":"invitee-1","name":"B","email":"b@example.com"}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.raw) })
			svc := &InvitationInviteeResource{client: c}
			m := inviteeModel("roster-1", "A", "a@example.com", "")
			m.ID, m.InviteeID = types.StringValue(invitationInviteeID("roster-1", "invitee-1")), types.StringValue("invitee-1")
			state := objectState(t, svc, &m)
			read := resource.ReadResponse{State: state}
			svc.Read(context.Background(), resource.ReadRequest{State: state}, &read)
			if !read.Diagnostics.HasError() || read.State.Raw.IsNull() {
				t.Fatalf("lost state: %v", read.Diagnostics)
			}
			created := resource.CreateResponse{State: objectState(t, svc, &InvitationInviteeModel{})}
			svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, inviteeModel("roster-1", "A", "a@example.com", ""))}, &created)
			if !created.Diagnostics.HasError() {
				t.Fatal("created despite untrusted preflight")
			}
		})
	}
}
func TestInvitationInviteePreflightNeverAdoptsExistingEmail(t *testing.T) {
	writes := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/list-invitation-invitees" {
			writes++
			t.Errorf("unexpected write %s", r.URL.Path)
		}
		fmt.Fprintf(w, `{"statusCode":200,"data":{"list":[%s]}}`, inviteeRow("roster-1", "existing", "A", "ADA@example.com", ""))
	})
	svc := &InvitationInviteeResource{client: c}
	resp := resource.CreateResponse{State: objectState(t, svc, &InvitationInviteeModel{})}
	svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, inviteeModel("roster-1", "Ada", "ada@example.com", ""))}, &resp)
	if !resp.Diagnostics.HasError() || writes != 0 {
		t.Fatalf("adopted existing row: %v writes %d", resp.Diagnostics, writes)
	}
}
func TestInvitationInviteeCreateReadbackFailureReportsImportID(t *testing.T) {
	lists := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/list-invitation-invitees":
			lists++
			if lists == 1 {
				fmt.Fprint(w, `{"statusCode":200,"data":{"list":[]}}`)
			} else {
				fmt.Fprint(w, `{"statusCode":503}`)
			}
		case "/api/v3/create-invitation-invitee":
			fmt.Fprintf(w, `{"statusCode":200,"data":%s}`, inviteeRow("roster-1", "recover-me", "A", "a@example.com", ""))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	svc := &InvitationInviteeResource{client: c}
	resp := resource.CreateResponse{State: objectState(t, svc, &InvitationInviteeModel{})}
	svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, inviteeModel("roster-1", "A", "a@example.com", ""))}, &resp)
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics[0].Detail(), invitationInviteeID("roster-1", "recover-me")) {
		t.Fatalf("missing recovery ID: %v", resp.Diagnostics)
	}
}
func TestInvitationInviteeDeleteRejectsFalseSuccessAndWrongState(t *testing.T) {
	deletes := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/list-invitation-invitees" {
			fmt.Fprintf(w, `{"statusCode":200,"data":{"list":[%s]}}`, inviteeRow("roster-1", "invitee-1", "A", "a@example.com", ""))
			return
		}
		deletes++
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":false}}`)
	})
	svc := &InvitationInviteeResource{client: c}
	m := inviteeModel("roster-1", "A", "a@example.com", "")
	m.ID = types.StringValue(invitationInviteeID("roster-1", "invitee-1"))
	m.InviteeID = types.StringValue("invitee-1")
	state := objectState(t, svc, &m)
	resp := resource.DeleteResponse{State: state}
	svc.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
	if !resp.Diagnostics.HasError() || deletes != 1 {
		t.Fatalf("false success accepted: %v %d", resp.Diagnostics, deletes)
	}
	m.InviteeID = types.StringValue("different")
	bad := objectState(t, svc, &m)
	resp = resource.DeleteResponse{State: bad}
	svc.Delete(context.Background(), resource.DeleteRequest{State: bad}, &resp)
	if !resp.Diagnostics.HasError() || deletes != 1 {
		t.Fatalf("mismatched ID deleted: %v %d", resp.Diagnostics, deletes)
	}
}
func TestInvitationInviteeImportRejectsMalformedID(t *testing.T) {
	svc := &InvitationInviteeResource{}
	for _, id := range []string{"roster-1/invitee-1", "ii1.bad", invitationInviteeID("roster-1", "")} {
		resp := resource.ImportStateResponse{State: objectState(t, svc, &InvitationInviteeModel{})}
		svc.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		if !resp.Diagnostics.HasError() {
			t.Fatalf("accepted %q", id)
		}
	}
}
