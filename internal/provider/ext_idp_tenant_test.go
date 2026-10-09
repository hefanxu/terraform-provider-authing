package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func extIdpModel(tenant, name string) ExtIdpModel {
	return ExtIdpModel{ID: types.StringValue("idp-1"), ExtIdpId: types.StringValue("idp-1"), Name: types.StringValue(name), Type: types.StringValue("oidc"), TenantId: types.StringValue(tenant)}
}

func extIdpState(t *testing.T, svc *ExtIdpResource, model ExtIdpModel) tfsdk.State {
	t.Helper()
	return lifecycleState(t, svc, model)
}

func extIdpReadModel(t *testing.T, st tfsdk.State) ExtIdpModel {
	t.Helper()
	var m ExtIdpModel
	if d := st.Get(context.Background(), &m); d.HasError() {
		t.Fatal(d)
	}
	return m
}

func TestExtIdpTenantScopedLifecycle(t *testing.T) {
	var calls []string
	remoteName := "Original"
	absent := false
	svc := &ExtIdpResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/v3/create-ext-idp", "/api/v3/update-ext-idp", "/api/v3/delete-ext-idp":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["tenantId"] != "tenant-A" {
				t.Errorf("unscoped write %s: %v", r.URL.Path, body)
			}
			if r.URL.Path == "/api/v3/create-ext-idp" {
				fmt.Fprint(w, `{"statusCode":200,"data":{"id":"idp-1","name":"Original","type":"oidc","tenantId":"tenant-A"}}`)
			}
			if r.URL.Path == "/api/v3/update-ext-idp" {
				remoteName = "Renamed"
				fmt.Fprint(w, `{"statusCode":200,"data":{"id":"idp-1","name":"Renamed","type":"oidc","tenantId":"tenant-A"}}`)
			}
			if r.URL.Path == "/api/v3/delete-ext-idp" {
				absent = true
				fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
			}
		case "/api/v3/get-ext-idp":
			if r.Method != http.MethodGet || r.URL.Query().Get("tenantId") != "tenant-A" || r.URL.Query().Get("id") != "idp-1" {
				t.Errorf("unscoped read: %s", r.URL)
			}
			if absent {
				fmt.Fprint(w, `{"statusCode":404}`)
				return
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"id":"idp-1","name":%q,"type":"oidc","tenantId":"tenant-A","connections":[]}}`, remoteName)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})}
	st := extIdpState(t, svc, extIdpModel("tenant-A", "Original"))
	plan := tfsdk.Plan{Schema: st.Schema, Raw: st.Raw}
	created := resource.CreateResponse{State: st}
	svc.Create(context.Background(), resource.CreateRequest{Plan: plan}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	read := resource.ReadResponse{State: created.State}
	svc.Read(context.Background(), resource.ReadRequest{State: created.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	if m := extIdpReadModel(t, read.State); m.TenantId.ValueString() != "tenant-A" || m.Type.ValueString() != "oidc" {
		t.Fatalf("bad state: %#v", m)
	}
	next := extIdpModel("tenant-A", "Renamed")
	plan = tfsdk.Plan{Schema: st.Schema}
	if d := plan.Set(context.Background(), next); d.HasError() {
		t.Fatal(d)
	}
	updated := resource.UpdateResponse{State: read.State}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: read.State}, &updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	deleted := resource.DeleteResponse{State: updated.State}
	svc.Delete(context.Background(), resource.DeleteRequest{State: updated.State}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	if strings.Join(calls, ",") != "/api/v3/create-ext-idp,/api/v3/get-ext-idp,/api/v3/get-ext-idp,/api/v3/get-ext-idp,/api/v3/update-ext-idp,/api/v3/get-ext-idp,/api/v3/get-ext-idp,/api/v3/delete-ext-idp,/api/v3/get-ext-idp" {
		t.Errorf("unexpected calls %v", calls)
	}
}

func TestExtIdpTenantMismatchNeverMutates(t *testing.T) {
	for _, tc := range []struct{ name, tenant, kind string }{
		{"wrong tenant", "tenant-B", "oidc"}, {"missing tenant", "", "oidc"}, {"wrong type", "tenant-A", "saml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			svc := &ExtIdpResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v3/get-ext-idp" {
					t.Errorf("unsafe write %s", r.URL)
					return
				}
				calls++
				if r.URL.Query().Get("tenantId") != "tenant-A" {
					t.Errorf("unscoped lookup %s", r.URL)
				}
				fmt.Fprintf(w, `{"statusCode":200,"data":{"id":"idp-1","name":"Remote","type":%q,"tenantId":%q}}`, tc.kind, tc.tenant)
			})}
			old := extIdpState(t, svc, extIdpModel("tenant-A", "Original"))
			read := resource.ReadResponse{State: old}
			svc.Read(context.Background(), resource.ReadRequest{State: old}, &read)
			if !read.Diagnostics.HasError() || !read.State.Raw.Equal(old.Raw) {
				t.Fatalf("read adopted mismatch: %v", read.Diagnostics)
			}
			plan := tfsdk.Plan{Schema: old.Schema}
			if d := plan.Set(context.Background(), extIdpModel("tenant-A", "Changed")); d.HasError() {
				t.Fatal(d)
			}
			updated := resource.UpdateResponse{State: old}
			svc.Update(context.Background(), resource.UpdateRequest{State: old, Plan: plan}, &updated)
			if !updated.Diagnostics.HasError() || !updated.State.Raw.Equal(old.Raw) {
				t.Fatalf("update adopted mismatch: %v", updated.Diagnostics)
			}
			deleted := resource.DeleteResponse{State: old}
			svc.Delete(context.Background(), resource.DeleteRequest{State: old}, &deleted)
			if !deleted.Diagnostics.HasError() {
				t.Fatal("delete did not reject mismatch")
			}
			if calls != 3 {
				t.Errorf("expected three scoped preflights, got %d", calls)
			}
		})
	}
}

func TestExtIdpTenantChangeCannotUpdate(t *testing.T) {
	svc := &ExtIdpResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected call %s", r.URL) })}
	old := extIdpState(t, svc, extIdpModel("tenant-A", "Original"))
	plan := tfsdk.Plan{Schema: old.Schema}
	if d := plan.Set(context.Background(), extIdpModel("tenant-B", "Changed")); d.HasError() {
		t.Fatal(d)
	}
	out := resource.UpdateResponse{State: old}
	svc.Update(context.Background(), resource.UpdateRequest{State: old, Plan: plan}, &out)
	if !out.Diagnostics.HasError() || !out.State.Raw.Equal(old.Raw) {
		t.Fatalf("tenant move accepted: %v", out.Diagnostics)
	}
	var sr resource.SchemaResponse
	svc.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	attr := sr.Schema.Attributes["tenant_id"].(schema.StringAttribute)
	if len(attr.PlanModifiers) < 2 {
		t.Fatal("tenant_id must preserve prior scope when omitted and require replacement when changed")
	}
}

func TestExtIdpUpdateRejectsUnchangedRemote(t *testing.T) {
	svc := &ExtIdpResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/get-ext-idp":
			fmt.Fprint(w, `{"statusCode":200,"data":{"id":"idp-1","tenantId":"tenant-A","type":"oidc","name":"Original"}}`)
		case "/api/v3/update-ext-idp":
			fmt.Fprint(w, `{"statusCode":200,"data":{"id":"idp-1"}}`)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})}
	st := extIdpState(t, svc, extIdpModel("tenant-A", "Original"))
	plan := tfsdk.Plan{Schema: st.Schema}
	if d := plan.Set(context.Background(), extIdpModel("tenant-A", "Changed")); d.HasError() {
		t.Fatal(d)
	}
	out := resource.UpdateResponse{State: st}
	svc.Update(context.Background(), resource.UpdateRequest{State: st, Plan: plan}, &out)
	if !out.Diagnostics.HasError() || !out.State.Raw.Equal(st.Raw) {
		t.Fatalf("unchanged remote accepted: %v", out.Diagnostics)
	}
}

func TestExtIdpTenantImport(t *testing.T) {
	svc := &ExtIdpResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/get-ext-idp" || r.URL.Query().Get("id") != "idp-1" || r.URL.Query().Get("tenantId") != "tenant-A" {
			t.Errorf("wrong import lookup %s", r.URL)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"id":"idp-1","name":"Remote","type":"oidc","tenantId":"tenant-A"}}`)
	})}
	for _, id := range []string{"tenant-A:idp-1", "tenant-A:", ":idp-1"} {
		t.Run(id, func(t *testing.T) {
			var sr resource.SchemaResponse
			svc.Schema(context.Background(), resource.SchemaRequest{}, &sr)
			out := resource.ImportStateResponse{State: tfsdk.State{Schema: sr.Schema}}
			svc.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &out)
			if id != "tenant-A:idp-1" {
				if !out.Diagnostics.HasError() {
					t.Fatal("invalid composite accepted")
				}
				return
			}
			if out.Diagnostics.HasError() {
				t.Fatal(out.Diagnostics)
			}
			m := extIdpReadModel(t, out.State)
			if m.ExtIdpId.ValueString() != "idp-1" || m.TenantId.ValueString() != "tenant-A" {
				t.Fatalf("bad import identity: %#v", m)
			}
			read := resource.ReadResponse{State: out.State}
			svc.Read(context.Background(), resource.ReadRequest{State: out.State}, &read)
			if read.Diagnostics.HasError() {
				t.Fatal(read.Diagnostics)
			}
			m = extIdpReadModel(t, read.State)
			if m.Type.ValueString() != "oidc" || m.Name.ValueString() != "Remote" {
				t.Fatalf("not hydrated: %#v", m)
			}
		})
	}
}
