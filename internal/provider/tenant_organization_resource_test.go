package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func tenantOrgModel() TenantOrganizationModel {
	return TenantOrganizationModel{ID: types.StringUnknown(), TenantID: types.StringValue("tenant:/one"), OrganizationCode: types.StringValue("code:/one"), OrganizationName: types.StringValue("First"), Description: types.StringValue("description")}
}

func TestTenantOrganizationLifecycle(t *testing.T) {
	present, deletes, creates := false, 0, 0
	name := "First"
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/get-organization":
			if r.Method != "GET" || r.URL.Query().Get("tenantId") != "tenant:/one" || r.URL.Query().Get("organizationCode") != "code:/one" {
				t.Errorf("wrong lookup: %s %s", r.Method, r.URL.RawQuery)
			}
			if !present {
				fmt.Fprint(w, `{"statusCode":404}`)
				return
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"tenantId":"tenant:/one","organizationCode":"code:/one","organizationName":%q,"description":"description","hasChildren":false}}`, name)
		case "/api/v3/create-organization", "/api/v3/update-organization", "/api/v3/delete-organization":
			var body map[string]any
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || body["tenantId"] != "tenant:/one" || body["organizationCode"] != "code:/one" {
				t.Errorf("wrong mutation %s: %v", r.URL.Path, body)
			}
			switch r.URL.Path {
			case "/api/v3/create-organization":
				creates++
				if body["organizationName"] != "First" {
					t.Errorf("name %v", body)
				}
				metadata, ok := body["metadata"].(map[string]any)
				if !ok || len(metadata) != 0 {
					t.Errorf("metadata %v", body)
				}
				present = true
			case "/api/v3/update-organization":
				name = body["organizationName"].(string)
			case "/api/v3/delete-organization":
				deletes++
				present = false
				fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
				return
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"tenantId":"tenant:/one","organizationCode":"code:/one","organizationName":%q}}`, name)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	svc := &TenantOrganizationResource{client: c}
	ctx := context.Background()
	out := resource.CreateResponse{State: objectState(t, svc, &TenantOrganizationModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, tenantOrgModel())}, &out)
	if out.Diagnostics.HasError() || creates != 1 {
		t.Fatalf("create: %v, %d", out.Diagnostics, creates)
	}
	var m TenantOrganizationModel
	if d := out.State.Get(ctx, &m); d.HasError() || m.ID.ValueString() != tenantOrganizationID("tenant:/one", "code:/one") {
		t.Fatalf("state %+v %v", m, d)
	}
	name = "drift"
	rd := resource.ReadResponse{State: out.State}
	svc.Read(ctx, resource.ReadRequest{State: out.State}, &rd)
	if d := rd.State.Get(ctx, &m); d.HasError() || rd.Diagnostics.HasError() || m.OrganizationName.ValueString() != "drift" {
		t.Fatalf("drift %+v %v", m, rd.Diagnostics)
	}
	m.OrganizationName = types.StringValue("Updated")
	up := resource.UpdateResponse{State: rd.State}
	svc.Update(ctx, resource.UpdateRequest{Plan: objectPlan(t, svc, m), State: rd.State}, &up)
	if up.Diagnostics.HasError() || name != "Updated" {
		t.Fatalf("update %v %s", up.Diagnostics, name)
	}
	del := resource.DeleteResponse{State: up.State}
	svc.Delete(ctx, resource.DeleteRequest{State: up.State}, &del)
	if del.Diagnostics.HasError() || deletes != 1 {
		t.Fatalf("delete %v %d", del.Diagnostics, deletes)
	}
	missing := resource.ReadResponse{State: up.State}
	svc.Read(ctx, resource.ReadRequest{State: up.State}, &missing)
	if missing.Diagnostics.HasError() || !missing.State.Raw.IsNull() {
		t.Fatalf("missing %v", missing.Diagnostics)
	}
}
