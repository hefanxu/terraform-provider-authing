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

func inviteeModel(roster, name, email, phone string) InvitationInviteeModel {
	m := InvitationInviteeModel{ID: types.StringUnknown(), RosterID: types.StringValue(roster), InviteeID: types.StringUnknown(), Name: types.StringValue(name), Email: types.StringValue(email), Phone: types.StringNull()}
	if phone != "" {
		m.Phone = types.StringValue(phone)
	}
	return m
}
func inviteeRow(roster, id, name, email, phone string) string {
	b, _ := json.Marshal(map[string]string{"rosterId": roster, "inviteeId": id, "name": name, "email": email, "phone": phone})
	return string(b)
}
func TestInvitationInviteeLifecyclePaginationImportAndExactDelete(t *testing.T) {
	ctx := context.Background()
	present := false
	name := "Ada"
	phone := ""
	deletes := 0
	lists := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("non POST %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode %v", err)
			return
		}
		switch r.URL.Path {
		case "/api/v3/list-invitation-invitees":
			lists++
			if body["rosterId"] != "roster-1" || body["limit"] != float64(50) || len(body) != 3 {
				t.Errorf("list payload %v", body)
			}
			page := body["page"].(float64)
			rows := make([]string, 0)
			if page == 1 {
				for i := 0; i < 50; i++ {
					rows = append(rows, inviteeRow("roster-1", fmt.Sprintf("other-%d", i), "Other", "other@example.com", ""))
				}
			}
			if page == 2 && present {
				rows = append(rows, inviteeRow("roster-1", "invitee-1", name, "ada@example.com", phone))
			}
			total := 50
			if present {
				total++
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"totalCount":%d,"list":[%s]}}`, total, strings.Join(rows, ","))
		case "/api/v3/create-invitation-invitee":
			if !reflect.DeepEqual(body, map[string]any{"rosterId": "roster-1", "name": "Ada", "email": "ada@example.com"}) {
				t.Errorf("create %v", body)
			}
			present = true
			fmt.Fprintf(w, `{"statusCode":200,"data":%s}`, inviteeRow("roster-1", "invitee-1", name, "ada@example.com", phone))
		case "/api/v3/edit-invitation-invitee":
			if !reflect.DeepEqual(body, map[string]any{"rosterId": "roster-1", "inviteeId": "invitee-1", "name": "Updated", "email": "ada@example.com", "phone": "123"}) {
				t.Errorf("edit %v", body)
			}
			name = "Updated"
			phone = "123"
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		case "/api/v3/batch-delete-invitation-invitees":
			deletes++
			if !reflect.DeepEqual(body, map[string]any{"rosterId": "roster-1", "inviteeIds": []any{"invitee-1"}}) {
				t.Errorf("delete %v", body)
			}
			present = false
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	})
	svc := &InvitationInviteeResource{client: c}
	created := resource.CreateResponse{State: objectState(t, svc, &InvitationInviteeModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, inviteeModel("roster-1", "Ada", "ada@example.com", ""))}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	var got InvitationInviteeModel
	if d := created.State.Get(ctx, &got); d.HasError() || got.ID.ValueString() != invitationInviteeID("roster-1", "invitee-1") || got.InviteeID.ValueString() != "invitee-1" {
		t.Fatalf("created %+v %v", got, d)
	}
	plan := got
	plan.Name = types.StringValue("Updated")
	plan.Phone = types.StringValue("123")
	updated := resource.UpdateResponse{State: created.State}
	svc.Update(ctx, resource.UpdateRequest{Plan: objectPlan(t, svc, plan), State: created.State}, &updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	name = "Drift"
	read := resource.ReadResponse{State: updated.State}
	svc.Read(ctx, resource.ReadRequest{State: updated.State}, &read)
	if d := read.State.Get(ctx, &got); d.HasError() || read.Diagnostics.HasError() || got.Name.ValueString() != "Drift" {
		t.Fatalf("drift %+v %v %v", got, d, read.Diagnostics)
	}
	imported := resource.ImportStateResponse{State: objectState(t, svc, &InvitationInviteeModel{})}
	svc.ImportState(ctx, resource.ImportStateRequest{ID: invitationInviteeID("roster-1", "invitee-1")}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	ir := resource.ReadResponse{State: imported.State}
	svc.Read(ctx, resource.ReadRequest{State: imported.State}, &ir)
	if d := ir.State.Get(ctx, &got); d.HasError() || ir.Diagnostics.HasError() || got.Name.ValueString() != "Drift" || got.Phone.ValueString() != "123" {
		t.Fatalf("import %+v %v %v", got, d, ir.Diagnostics)
	}
	deleted := resource.DeleteResponse{State: ir.State}
	svc.Delete(ctx, resource.DeleteRequest{State: ir.State}, &deleted)
	if deleted.Diagnostics.HasError() || deletes != 1 || lists < 8 {
		t.Fatalf("delete %v, deletes %d, lists %d", deleted.Diagnostics, deletes, lists)
	}
	svc.Delete(ctx, resource.DeleteRequest{State: ir.State}, &deleted)
	if deleted.Diagnostics.HasError() || deletes != 1 {
		t.Fatalf("idempotent %v %d", deleted.Diagnostics, deletes)
	}
}
