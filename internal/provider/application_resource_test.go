package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Authing/authing-golang-sdk/v3/management"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func applicationFixture(t *testing.T, handler func(http.ResponseWriter, *http.Request)) (*ApplicationResource, tfsdk.State, tfsdk.Plan) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v3/get-management-token" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"test-token","expires_in":3600}}`)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	client, err := management.NewManagementClient(&management.ManagementClientOptions{AccessKeyId: "application-test", AccessKeySecret: "test-secret", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	svc := &ApplicationResource{client: client}
	sr := resource.SchemaResponse{}
	svc.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	return svc, tfsdk.State{Schema: sr.Schema}, tfsdk.Plan{Schema: sr.Schema}
}

func applicationModel(name string) ApplicationModel {
	return ApplicationModel{ID: types.StringValue("app-1"), AppId: types.StringValue("app-1"), AppName: types.StringValue(name), AppType: types.StringValue("web"), RedirectUris: applicationList([]string{"https://old/cb"}), LogoutRedirectUris: applicationList([]string{"https://old/logout"}), InitLoginUrl: types.StringValue("https://old/login"), Description: types.StringValue("old")}
}
func applicationList(values []string) types.List {
	v, _ := types.ListValueFrom(context.Background(), types.StringType, values)
	return v
}
func setApplicationState(t *testing.T, state *tfsdk.State, model ApplicationModel) {
	t.Helper()
	if d := state.Set(context.Background(), model); d.HasError() {
		t.Fatal(d)
	}
}
func setApplicationPlan(t *testing.T, plan *tfsdk.Plan, model ApplicationModel) {
	t.Helper()
	if d := plan.Set(context.Background(), model); d.HasError() {
		t.Fatal(d)
	}
}
func getApplicationState(t *testing.T, state tfsdk.State) ApplicationModel {
	t.Helper()
	var m ApplicationModel
	if d := state.Get(context.Background(), &m); d.HasError() {
		t.Fatal(d)
	}
	return m
}

func TestApplicationUpdateSendsChangedFields(t *testing.T) {
	var body map[string]any
	svc, state, plan := applicationFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/update-application" || r.Method != "POST" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	})
	old := applicationModel("Old")
	setApplicationState(t, &state, old)
	next := applicationModel("New")
	next.RedirectUris = applicationList([]string{"https://new/cb"})
	next.LogoutRedirectUris = applicationList([]string{})
	next.InitLoginUrl = types.StringValue("https://new/login")
	next.Description = types.StringValue("new")
	setApplicationPlan(t, &plan, next)
	resp := resource.UpdateResponse{State: state}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	want := map[string]any{"appId": "app-1", "appName": "New", "redirectUris": []any{"https://new/cb"}, "logoutRedirectUris": []any{}, "initLoginUri": "https://new/login", "appDescription": "new"}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("update payload: got %#v want %#v", body, want)
	}
	if got := getApplicationState(t, resp.State); got.AppName.ValueString() != "New" {
		t.Errorf("state not updated: %#v", got)
	}
}

func TestApplicationUpdateFailurePreservesState(t *testing.T) {
	for _, tc := range []struct{ name, response string }{{"api error", `{"statusCode":500,"message":"failure"}`}, {"unsuccessful", `{"statusCode":200,"data":{"success":false}}`}, {"malformed", `not-json`}} {
		t.Run(tc.name, func(t *testing.T) {
			svc, state, plan := applicationFixture(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.response) })
			setApplicationState(t, &state, applicationModel("Old"))
			setApplicationPlan(t, &plan, applicationModel("New"))
			resp := resource.UpdateResponse{State: state}
			svc.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: state}, &resp)
			if !resp.Diagnostics.HasError() {
				t.Fatal("expected update diagnostic")
			}
			if got := getApplicationState(t, resp.State).AppName.ValueString(); got != "Old" {
				t.Fatalf("failure changed state to %q", got)
			}
		})
	}
}

func TestApplicationReadHydratesDrift(t *testing.T) {
	svc, state, _ := applicationFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/get-application" || r.URL.Query().Get("appId") != "app-1" {
			t.Errorf("unexpected get %s", r.URL.String())
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"appId":"app-1","appName":"Remote","appType":"spa","appDescription":"","redirectUris":["https://remote/cb"],"logoutRedirectUris":[],"initLoginUri":""}}`)
	})
	setApplicationState(t, &state, applicationModel("Old"))
	resp := resource.ReadResponse{State: state}
	svc.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	got := getApplicationState(t, resp.State)
	if got.ID.ValueString() != "app-1" || got.AppId.ValueString() != "app-1" || got.AppName.ValueString() != "Remote" || got.AppType.ValueString() != "spa" || got.Description.ValueString() != "" || got.InitLoginUrl.ValueString() != "" {
		t.Errorf("stale state: %#v", got)
	}
	var redirect, logout []string
	got.RedirectUris.ElementsAs(context.Background(), &redirect, false)
	got.LogoutRedirectUris.ElementsAs(context.Background(), &logout, false)
	if !reflect.DeepEqual(redirect, []string{"https://remote/cb"}) || !reflect.DeepEqual(logout, []string{}) {
		t.Errorf("stale URIs %v %v", redirect, logout)
	}
}

func TestApplicationReadOnlyRemovesMissing(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		missing        bool
	}{{"missing", `{"statusCode":404,"message":"not found"}`, true}, {"server failure", `{"statusCode":500,"message":"backend failure"}`, false}, {"empty success", `{"statusCode":200,"data":{}}`, false}, {"malformed", `not-json`, false}} {
		t.Run(tc.name, func(t *testing.T) {
			svc, state, _ := applicationFixture(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.response) })
			setApplicationState(t, &state, applicationModel("Old"))
			resp := resource.ReadResponse{State: state}
			svc.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
			if tc.missing {
				if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
					t.Fatalf("missing not removed: %v %#v", resp.Diagnostics, resp.State.Raw)
				}
			} else if !resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
				t.Fatalf("failure removed resource: %v %#v", resp.Diagnostics, resp.State.Raw)
			}
		})
	}
}

func TestApplicationDeleteReportsFailure(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		success        bool
	}{{"success", `{"statusCode":200,"data":{"success":true}}`, true}, {"already missing", `{"statusCode":404,"message":"not found"}`, true}, {"api failure", `{"statusCode":500,"message":"failure"}`, false}, {"false success", `{"statusCode":200,"data":{"success":false}}`, false}, {"malformed", `not-json`, false}} {
		t.Run(tc.name, func(t *testing.T) {
			svc, state, _ := applicationFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v3/delete-application" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				var req map[string]any
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				if req["appId"] != "app-1" {
					t.Errorf("wrong id: %v", req)
				}
				fmt.Fprint(w, tc.response)
			})
			setApplicationState(t, &state, applicationModel("Old"))
			resp := resource.DeleteResponse{State: state}
			svc.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() == tc.success {
				t.Fatalf("unexpected delete diagnostic: %v", resp.Diagnostics)
			}
		})
	}
}

func TestApplicationImportSetsBothIdentifiers(t *testing.T) {
	svc, state, _ := applicationFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected call %s", r.URL.Path) })
	resp := resource.ImportStateResponse{State: state}
	svc.ImportState(context.Background(), resource.ImportStateRequest{ID: "app-1"}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	got := getApplicationState(t, resp.State)
	if got.ID.ValueString() != "app-1" || got.AppId.ValueString() != "app-1" {
		t.Errorf("incomplete identity: %#v", got)
	}
}

func TestApplicationCreateReconcilesComputedFields(t *testing.T) {
	svc, state, plan := applicationFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/create-application" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req["appType"] != "spa" {
			t.Errorf("app type not sent: %v", req)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"appId":"app-1","appName":"Created","appType":"spa","appDescription":"server description","initLoginUri":"https://login","redirectUris":["https://callback"],"logoutRedirectUris":["https://logout"]}}`)
	})
	m := ApplicationModel{AppName: types.StringValue("Created"), AppType: types.StringValue("spa"), Description: types.StringUnknown(), InitLoginUrl: types.StringUnknown(), RedirectUris: applicationList([]string{"https://callback"}), LogoutRedirectUris: applicationList([]string{"https://logout"})}
	setApplicationPlan(t, &plan, m)
	resp := resource.CreateResponse{State: state}
	svc.Create(context.Background(), resource.CreateRequest{Plan: plan}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	got := getApplicationState(t, resp.State)
	if got.ID.ValueString() != "app-1" || got.AppId.ValueString() != "app-1" || got.Description.ValueString() != "server description" || got.InitLoginUrl.ValueString() != "https://login" || got.AppType.ValueString() != "spa" {
		t.Errorf("incomplete create state: %#v", got)
	}
}

func TestApplicationCreateKeepsConfiguredName(t *testing.T) {
	svc, state, plan := applicationFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"statusCode":200,"data":{"appId":"app-1","appName":"server-normalized"}}`)
	})
	setApplicationPlan(t, &plan, ApplicationModel{AppName: types.StringValue("User configured"), RedirectUris: types.ListNull(types.StringType), LogoutRedirectUris: types.ListNull(types.StringType)})
	resp := resource.CreateResponse{State: state}
	svc.Create(context.Background(), resource.CreateRequest{Plan: plan}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := getApplicationState(t, resp.State).AppName.ValueString(); got != "User configured" {
		t.Fatalf("configured name replaced by API: %q", got)
	}
}

func TestApplicationUpdateRejectsAppTypeChange(t *testing.T) {
	svc, state, plan := applicationFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected request %s", r.URL.Path) })
	old := applicationModel("Old")
	setApplicationState(t, &state, old)
	next := applicationModel("Old")
	next.AppType = types.StringValue("spa")
	setApplicationPlan(t, &plan, next)
	resp := resource.UpdateResponse{State: state}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: state}, &resp)
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Summary(), "app_type") {
		t.Fatalf("unsupported change not rejected: %v", resp.Diagnostics)
	}
}
