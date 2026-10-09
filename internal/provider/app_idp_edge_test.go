package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestExtIdpUpdateWithUnknownComputedIdentityUsesVerifiedState(t *testing.T) {
	name := "hermesacc-1234567890abcdef"
	calls := 0
	svc := &ExtIdpResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/get-ext-idp":
			fmt.Fprintf(w, `{"statusCode":200,"data":{"id":"idp-1","name":%q,"type":"oidc"}}`, name)
		case "/api/v3/update-ext-idp":
			calls++
			var body struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.ID != "idp-1" || body.Name != name {
				t.Errorf("unexpected update body %+v", body)
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"id":"idp-1","name":%q,"type":"oidc"}}`, name)
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	})}
	old := extIdpModel("", name+"-drift")
	st := extIdpState(t, svc, old)
	plan := tfsdk.Plan{Schema: st.Schema}
	if d := plan.Set(context.Background(), ExtIdpModel{ID: types.StringUnknown(), ExtIdpId: types.StringUnknown(), Name: types.StringValue(name), Type: types.StringValue("oidc"), TenantId: types.StringValue("")}); d.HasError() {
		t.Fatal(d)
	}
	out := resource.UpdateResponse{State: st}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: st}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	got := extIdpReadModel(t, out.State)
	if calls != 1 || got.ID.ValueString() != "idp-1" || got.ExtIdpId.ValueString() != "idp-1" || got.ID.IsUnknown() || got.ExtIdpId.IsUnknown() {
		t.Fatalf("update returned unknown identity or skipped mutation: %+v calls=%d", got, calls)
	}
}

func TestExtIdpCreateWithOmittedTenantReturnsKnownState(t *testing.T) {
	name := "hermesacc-1234567890abcdef"
	svc := &ExtIdpResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/create-ext-idp":
			fmt.Fprintf(w, `{"statusCode":200,"data":{"id":"idp-1","name":%q,"type":"oidc"}}`, name)
		case "/api/v3/get-ext-idp":
			if r.URL.Query().Get("id") != "idp-1" || r.URL.Query().Get("tenantId") != "" {
				t.Errorf("unexpected GET %s", r.URL)
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"id":"idp-1","name":%q,"type":"oidc","connections":[]}}`, name)
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	})}
	model := ExtIdpModel{ID: types.StringUnknown(), ExtIdpId: types.StringUnknown(), Name: types.StringValue(name), Type: types.StringValue("oidc"), TenantId: types.StringUnknown()}
	st := extIdpState(t, svc, model)
	out := resource.CreateResponse{State: st}
	svc.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan{Schema: st.Schema, Raw: st.Raw}}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	got := extIdpReadModel(t, out.State)
	if got.TenantId.IsUnknown() || got.TenantId.IsNull() || got.TenantId.ValueString() != "" || got.ID.ValueString() != "idp-1" || got.ExtIdpId.ValueString() != "idp-1" {
		t.Fatalf("create returned unresolved tenant or identity: %+v", got)
	}
}

func TestExtIdpCreateRejectsWrongTenantResponse(t *testing.T) {
	for _, tenant := range []string{"tenant-B", ""} {
		t.Run(tenant, func(t *testing.T) {
			svc := &ExtIdpResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v3/create-ext-idp" {
					t.Errorf("unexpected %s", r.URL)
				}
				fmt.Fprintf(w, `{"statusCode":200,"data":{"id":"idp-1","name":"Original","type":"oidc","tenantId":%q}}`, tenant)
			})}
			st := extIdpState(t, svc, extIdpModel("tenant-A", "Original"))
			out := resource.CreateResponse{State: st}
			svc.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan{Schema: st.Schema, Raw: st.Raw}}, &out)
			if !out.Diagnostics.HasError() || !strings.Contains(out.Diagnostics.Errors()[0].Detail(), "idp-1") {
				t.Errorf("wrong scope adopted without recovery ID: %v", out.Diagnostics)
			}
		})
	}
}

func TestApplicationUpdateReadbackAdoptsOmittedSetting(t *testing.T) {
	svc, st, plan := applicationFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/update-application":
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		case "/api/v3/get-application":
			fmt.Fprint(w, `{"statusCode":200,"data":{"appId":"app-1","appName":"New","appType":"web","appLogo":"remote-logo","appIdentifier":"remote-id","defaultProtocol":"oidc","ssoEnabled":false,"appDescription":"old","initLoginUri":"https://old/login","redirectUris":["https://old/cb"],"logoutRedirectUris":["https://old/logout"]}}`)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	old := applicationModel("Old")
	old.AppLogo = types.StringValue("stale-logo")
	old.AppIdentifier = types.StringValue("stale-id")
	old.SsoEnabled = types.BoolValue(true)
	old.DefaultProtocol = types.StringValue("oidc")
	next := applicationModel("New")
	next.AppLogo = types.StringNull()
	next.AppIdentifier = types.StringNull()
	next.SsoEnabled = types.BoolNull()
	next.DefaultProtocol = types.StringNull()
	setApplicationState(t, &st, old)
	setApplicationPlan(t, &plan, next)
	out := resource.UpdateResponse{State: st}
	svc.Update(context.Background(), resource.UpdateRequest{State: st, Plan: plan}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	got := getApplicationState(t, out.State)
	if got.AppLogo.ValueString() != "remote-logo" || got.AppIdentifier.ValueString() != "remote-id" || got.SsoEnabled.IsNull() || got.SsoEnabled.ValueBool() {
		t.Errorf("did not refresh omitted settings: %#v", got)
	}
}
