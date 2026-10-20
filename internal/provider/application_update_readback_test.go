package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestApplicationUpdateReadbackRejectsUnchangedRemote(t *testing.T) {
	gets := 0
	svc, st, plan := applicationFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/update-application":
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		case "/api/v3/get-application":
			gets++
			if r.URL.Query().Get("appId") != "app-1" {
				t.Errorf("wrong readback %s", r.URL)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"appId":"app-1","appName":"Portal","appType":"web","appIdentifier":"old-id","appLogo":"old-logo","defaultProtocol":"oidc","ssoEnabled":true}}`)
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
	})
	old := applicationModel("Portal")
	old.AppIdentifier = types.StringValue("old-id")
	old.AppLogo = types.StringValue("old-logo")
	old.DefaultProtocol = types.StringValue("oidc")
	old.SsoEnabled = types.BoolValue(true)
	next := old
	next.AppLogo = types.StringValue("new-logo")
	next.SsoEnabled = types.BoolValue(false)
	setApplicationState(t, &st, old)
	setApplicationPlan(t, &plan, next)
	out := resource.UpdateResponse{State: st}
	svc.Update(context.Background(), resource.UpdateRequest{State: st, Plan: plan}, &out)
	if !out.Diagnostics.HasError() || gets != 1 || !out.State.Raw.Equal(st.Raw) {
		t.Fatalf("acknowledged but unchanged app accepted: reads=%d diagnostics=%v", gets, out.Diagnostics)
	}
	if !strings.Contains(out.Diagnostics.Errors()[0].Detail(), "app-1") {
		t.Errorf("diagnostic omitted identity: %v", out.Diagnostics)
	}
}

func TestApplicationUpdateReadbackRefreshesRemote(t *testing.T) {
	gets := 0
	svc, st, plan := applicationFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/update-application":
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		case "/api/v3/get-application":
			gets++
			fmt.Fprint(w, `{"statusCode":200,"data":{"appId":"app-1","appName":"Portal","appType":"web","appDescription":"old","initLoginUri":"https://old/login","redirectUris":["https://old/cb"],"logoutRedirectUris":["https://old/logout"],"appIdentifier":"new-id","appLogo":"new-logo","defaultProtocol":"oidc","ssoEnabled":false}}`)
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
	})
	old := applicationModel("Portal")
	old.AppIdentifier = types.StringValue("old-id")
	old.AppLogo = types.StringValue("old-logo")
	old.DefaultProtocol = types.StringValue("oidc")
	old.SsoEnabled = types.BoolValue(true)
	next := old
	next.AppIdentifier = types.StringValue("new-id")
	next.AppLogo = types.StringValue("new-logo")
	next.SsoEnabled = types.BoolValue(false)
	setApplicationState(t, &st, old)
	setApplicationPlan(t, &plan, next)
	out := resource.UpdateResponse{State: st}
	svc.Update(context.Background(), resource.UpdateRequest{State: st, Plan: plan}, &out)
	if out.Diagnostics.HasError() || gets != 1 {
		t.Fatalf("readback failed: reads=%d diagnostics=%v", gets, out.Diagnostics)
	}
	got := getApplicationState(t, out.State)
	if got.AppLogo.ValueString() != "new-logo" || got.AppIdentifier.ValueString() != "new-id" || got.SsoEnabled.ValueBool() {
		t.Fatalf("not refreshed: %#v", got)
	}
}

func TestApplicationUpdateReadbackFailurePreservesState(t *testing.T) {
	for _, response := range []string{`{"statusCode":500,"message":"offline"}`, `{"statusCode":404,"message":"missing"}`, `{"statusCode":200,"data":{}}`, `broken`} {
		t.Run(response, func(t *testing.T) {
			svc, st, plan := applicationFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/update-application" {
					fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
				} else if r.URL.Path == "/api/v3/get-application" {
					fmt.Fprint(w, response)
				} else {
					t.Errorf("unexpected %s", r.URL)
				}
			})
			old := applicationModel("Old")
			next := applicationModel("New")
			setApplicationState(t, &st, old)
			setApplicationPlan(t, &plan, next)
			out := resource.UpdateResponse{State: st}
			svc.Update(context.Background(), resource.UpdateRequest{State: st, Plan: plan}, &out)
			if !out.Diagnostics.HasError() || !out.State.Raw.Equal(st.Raw) {
				t.Fatalf("readback failure adopted plan: %v", out.Diagnostics)
			}
		})
	}
}
