package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func invitationModel() InvitationPolicyModel {
	return InvitationPolicyModel{ID: types.StringUnknown(), Name: types.StringValue("Initial"), EnabledIdentifierVerify: types.BoolValue(false), EnabledInfoFill: types.BoolValue(true), RegisterInfoFillMsg: types.StringValue("")}
}

func TestInvitationPolicyLifecycleAndImport(t *testing.T) {
	ctx := context.Background()
	present, name, info, message := false, "", true, ""
	deletes := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/create-invitation-policy", "/api/v3/update-invitation-policy":
			if r.Method != http.MethodPost {
				t.Errorf("method %s", r.Method)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			expected := map[string]any{"name": "Initial", "enabledIdentifierVerify": false, "enabledInfoFill": true, "registerInfoFillMsg": ""}
			if r.URL.Path == "/api/v3/update-invitation-policy" {
				expected = map[string]any{"policyId": "policy-1", "name": "Updated"}
			}
			if !reflect.DeepEqual(body, expected) {
				t.Errorf("payload %v want %v", body, expected)
			}
			present, name = true, body["name"].(string)
			fmt.Fprint(w, `{"statusCode":200,"data":{"policyId":"policy-1"}}`)
		case "/api/v3/get-invitation-policy":
			if r.Method != http.MethodGet || r.URL.Query().Get("policyId") != "policy-1" || len(r.URL.Query()) != 1 {
				t.Errorf("GET %s %s", r.Method, r.URL.RawQuery)
			}
			if !present {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"statusCode":404}`)
				return
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"policyId":"policy-1","name":%q,"enabledIdentifierVerify":false,"enabledInfoFill":%t,"registerInfoFillMsg":%q}}`, name, info, message)
		case "/api/v3/delete-invitation-policies-batch":
			deletes++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if r.Method != http.MethodPost || !reflect.DeepEqual(body, map[string]any{"policyIds": []any{"policy-1"}}) {
				t.Errorf("delete %s %v", r.Method, body)
			}
			present = false
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	})
	svc := &InvitationPolicyResource{client: c}
	created := resource.CreateResponse{State: objectState(t, svc, &InvitationPolicyModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, invitationModel())}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	var got InvitationPolicyModel
	if d := created.State.Get(ctx, &got); d.HasError() || got.ID.ValueString() != "policy-1" || got.Name.ValueString() != "Initial" {
		t.Fatalf("create %+v %v", got, d)
	}
	planned := got
	planned.Name = types.StringValue("Updated")
	updated := resource.UpdateResponse{State: created.State}
	svc.Update(ctx, resource.UpdateRequest{Plan: objectPlan(t, svc, planned), State: created.State}, &updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	info = false
	message = "Drift"
	read := resource.ReadResponse{State: updated.State}
	svc.Read(ctx, resource.ReadRequest{State: updated.State}, &read)
	if d := read.State.Get(ctx, &got); d.HasError() || read.Diagnostics.HasError() || got.Name.ValueString() != "Updated" || got.EnabledInfoFill.ValueBool() || got.RegisterInfoFillMsg.ValueString() != "Drift" {
		t.Fatalf("drift %+v %v %v", got, d, read.Diagnostics)
	}
	imported := resource.ImportStateResponse{State: objectState(t, svc, &InvitationPolicyModel{})}
	svc.ImportState(ctx, resource.ImportStateRequest{ID: "policy-1"}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	importRead := resource.ReadResponse{State: imported.State}
	svc.Read(ctx, resource.ReadRequest{State: imported.State}, &importRead)
	if d := importRead.State.Get(ctx, &got); d.HasError() || importRead.Diagnostics.HasError() || got.Name.ValueString() != "Updated" {
		t.Fatalf("import %+v %v %v", got, d, importRead.Diagnostics)
	}
	deleted := resource.DeleteResponse{State: read.State}
	svc.Delete(ctx, resource.DeleteRequest{State: read.State}, &deleted)
	if deleted.Diagnostics.HasError() || deletes != 1 {
		t.Fatalf("delete %v %d", deleted.Diagnostics, deletes)
	}
	svc.Delete(ctx, resource.DeleteRequest{State: read.State}, &deleted)
	if deleted.Diagnostics.HasError() || deletes != 1 {
		t.Fatalf("idempotent %v %d", deleted.Diagnostics, deletes)
	}
}
