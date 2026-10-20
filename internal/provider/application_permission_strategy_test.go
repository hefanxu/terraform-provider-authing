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

func TestApplicationPermissionStrategyCreate(t *testing.T) {
	var calls []string
	remote := "ALLOW_ALL"
	svc, state, plan := applicationFixtureWithStrategy(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/v3/create-application":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if _, ok := body["permissionStrategy"]; ok {
				t.Errorf("strategy leaked into create request: %v", body)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"appId":"app-1","appName":"Portal","appType":"web"}}`)
		case "/api/v3/update-application-permission-strategy":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body) != 2 || body["appId"] != "app-1" || body["permissionStrategy"] != "DENY_ALL" {
				t.Errorf("strategy payload: %v", body)
			}
			remote = "DENY_ALL"
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		case "/api/v3/get-application-permission-strategy":
			if r.Method != http.MethodGet || r.URL.Query().Get("appId") != "app-1" {
				t.Errorf("strategy lookup: %v", r.URL)
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"permissionStrategy":%q}}`, remote)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	})
	m := applicationModel("Portal")
	m.PermissionStrategy = types.StringValue("DENY_ALL")
	setApplicationPlan(t, &plan, m)
	resp := resource.CreateResponse{State: state}
	svc.Create(context.Background(), resource.CreateRequest{Plan: plan}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := getApplicationState(t, resp.State).PermissionStrategy.ValueString(); got != "DENY_ALL" {
		t.Errorf("strategy in state: %q", got)
	}
	want := "POST /api/v3/create-application,POST /api/v3/update-application-permission-strategy,GET /api/v3/get-application-permission-strategy"
	if strings.Join(calls, ",") != want {
		t.Errorf("calls: %v", calls)
	}
}

func TestApplicationPermissionStrategyUpdateAndDrift(t *testing.T) {
	remote := "ALLOW_ALL"
	var calls []string
	svc, state, plan := applicationFixtureWithStrategy(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/v3/update-application":
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		case "/api/v3/update-application-permission-strategy":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(body, map[string]any{"appId": "app-1", "permissionStrategy": "DENY_ALL"}) {
				t.Errorf("payload: %v", body)
			}
			remote = "DENY_ALL"
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		case "/api/v3/get-application-permission-strategy":
			if r.URL.Query().Get("appId") != "app-1" {
				t.Errorf("lookup ID: %s", r.URL)
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"permissionStrategy":%q}}`, remote)
		case "/api/v3/get-application":
			fmt.Fprint(w, `{"statusCode":200,"data":{"appId":"app-1","appName":"Portal","appType":"web"}}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	old := applicationModel("Portal")
	old.PermissionStrategy = types.StringValue("ALLOW_ALL")
	setApplicationState(t, &state, old)
	next := old
	next.PermissionStrategy = types.StringValue("DENY_ALL")
	setApplicationPlan(t, &plan, next)
	updated := resource.UpdateResponse{State: state}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: state}, &updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	if got := getApplicationState(t, updated.State).PermissionStrategy.ValueString(); got != "DENY_ALL" {
		t.Errorf("updated state %q", got)
	}
	if !reflect.DeepEqual(calls, []string{"/api/v3/update-application-permission-strategy", "/api/v3/get-application-permission-strategy"}) {
		t.Errorf("calls: %v", calls)
	}
	calls = nil
	remote = "ALLOW_ALL"
	read := resource.ReadResponse{State: updated.State}
	svc.Read(context.Background(), resource.ReadRequest{State: updated.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	got := getApplicationState(t, read.State)
	if got.PermissionStrategy.ValueString() != "ALLOW_ALL" || got.AppName.ValueString() != "Portal" {
		t.Errorf("drift: %#v", got)
	}
	if !reflect.DeepEqual(calls, []string{"/api/v3/get-application", "/api/v3/get-application-permission-strategy"}) {
		t.Errorf("read calls: %v", calls)
	}
}

func TestApplicationPermissionStrategyImportAndOmittedCreate(t *testing.T) {
	writes := 0
	svc, state, plan := applicationFixtureWithStrategy(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/create-application":
			fmt.Fprint(w, `{"statusCode":200,"data":{"appId":"app-1","appName":"Portal"}}`)
		case "/api/v3/get-application":
			fmt.Fprint(w, `{"statusCode":200,"data":{"appId":"app-1","appName":"Portal"}}`)
		case "/api/v3/get-application-permission-strategy":
			fmt.Fprint(w, `{"statusCode":200,"data":{"permissionStrategy":"ALLOW_ALL"}}`)
		case "/api/v3/update-application-permission-strategy":
			writes++
			t.Error("unexpected strategy write")
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	m := applicationModel("Portal")
	m.PermissionStrategy = types.StringNull()
	setApplicationPlan(t, &plan, m)
	created := resource.CreateResponse{State: state}
	svc.Create(context.Background(), resource.CreateRequest{Plan: plan}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	if got := getApplicationState(t, created.State).PermissionStrategy.ValueString(); got != "ALLOW_ALL" {
		t.Errorf("omitted create: %q", got)
	}
	imported := resource.ImportStateResponse{State: state}
	svc.ImportState(context.Background(), resource.ImportStateRequest{ID: "app-1"}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	if !getApplicationState(t, imported.State).PermissionStrategy.IsNull() {
		t.Fatal("import should start null")
	}
	read := resource.ReadResponse{State: imported.State}
	svc.Read(context.Background(), resource.ReadRequest{State: imported.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	if got := getApplicationState(t, read.State).PermissionStrategy.ValueString(); got != "ALLOW_ALL" {
		t.Errorf("import read: %q", got)
	}
	if writes != 0 {
		t.Errorf("unexpected writes %d", writes)
	}
}

func TestApplicationPermissionStrategyReadFailureKeepsState(t *testing.T) {
	for _, bad := range []string{`{"statusCode":403,"message":"forbidden"}`, `{"statusCode":404,"message":"not found"}`, `{"statusCode":200,"data":{}}`, `{"statusCode":200,"data":{"permissionStrategy":"OTHER"}}`, `broken`} {
		t.Run(bad, func(t *testing.T) {
			svc, state, _ := applicationFixtureWithStrategy(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v3/get-application":
					fmt.Fprint(w, `{"statusCode":200,"data":{"appId":"app-1","appName":"Remote"}}`)
				case "/api/v3/get-application-permission-strategy":
					fmt.Fprint(w, bad)
				default:
					t.Errorf("unexpected %s", r.URL.Path)
				}
			})
			old := applicationModel("Old")
			old.PermissionStrategy = types.StringValue("DENY_ALL")
			setApplicationState(t, &state, old)
			resp := resource.ReadResponse{State: state}
			svc.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
			if !resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
				t.Fatalf("strategy failure removed resource: %v", resp.Diagnostics)
			}
			got := getApplicationState(t, resp.State)
			if got.AppName.ValueString() != "Old" || got.PermissionStrategy.ValueString() != "DENY_ALL" {
				t.Errorf("read mutated state: %#v", got)
			}
		})
	}
}

func TestApplicationPermissionStrategyCreateFailureShowsRecoveryID(t *testing.T) {
	for _, tc := range []struct{ name, write, read string }{
		{"false success", `{"statusCode":200,"data":{"success":false}}`, `{"statusCode":200,"data":{"permissionStrategy":"DENY_ALL"}}`},
		{"write error", `{"statusCode":403,"message":"forbidden"}`, `{"statusCode":200,"data":{"permissionStrategy":"DENY_ALL"}}`},
		{"read error", `{"statusCode":200,"data":{"success":true}}`, `{"statusCode":500,"message":"offline"}`},
		{"mismatch", `{"statusCode":200,"data":{"success":true}}`, `{"statusCode":200,"data":{"permissionStrategy":"ALLOW_ALL"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, state, plan := applicationFixtureWithStrategy(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v3/create-application":
					fmt.Fprint(w, `{"statusCode":200,"data":{"appId":"app-1","appName":"Portal"}}`)
				case "/api/v3/update-application-permission-strategy":
					fmt.Fprint(w, tc.write)
				case "/api/v3/get-application-permission-strategy":
					fmt.Fprint(w, tc.read)
				default:
					t.Errorf("unexpected %s", r.URL.Path)
				}
			})
			m := applicationModel("Portal")
			m.PermissionStrategy = types.StringValue("DENY_ALL")
			setApplicationPlan(t, &plan, m)
			resp := resource.CreateResponse{State: state}
			svc.Create(context.Background(), resource.CreateRequest{Plan: plan}, &resp)
			if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "app-1") || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "Import") {
				t.Errorf("missing recovery ID: %v", resp.Diagnostics)
			}
		})
	}
}

func TestApplicationPermissionStrategyUpdateFailurePreservesState(t *testing.T) {
	for _, tc := range []struct{ name, write, read string }{
		{"false success", `{"statusCode":200,"data":{"success":false}}`, `{"statusCode":200,"data":{"permissionStrategy":"DENY_ALL"}}`},
		{"write error", `{"statusCode":500,"message":"unavailable"}`, `{"statusCode":200,"data":{"permissionStrategy":"DENY_ALL"}}`},
		{"read error", `{"statusCode":200,"data":{"success":true}}`, `{"statusCode":403,"message":"forbidden"}`},
		{"mismatch", `{"statusCode":200,"data":{"success":true}}`, `{"statusCode":200,"data":{"permissionStrategy":"ALLOW_ALL"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, state, plan := applicationFixtureWithStrategy(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v3/update-application-permission-strategy":
					fmt.Fprint(w, tc.write)
				case "/api/v3/get-application-permission-strategy":
					fmt.Fprint(w, tc.read)
				default:
					t.Errorf("unexpected %s", r.URL.Path)
				}
			})
			old := applicationModel("Portal")
			old.PermissionStrategy = types.StringValue("ALLOW_ALL")
			setApplicationState(t, &state, old)
			next := old
			next.PermissionStrategy = types.StringValue("DENY_ALL")
			setApplicationPlan(t, &plan, next)
			resp := resource.UpdateResponse{State: state}
			svc.Update(context.Background(), resource.UpdateRequest{State: state, Plan: plan}, &resp)
			if !resp.Diagnostics.HasError() || getApplicationState(t, resp.State).PermissionStrategy.ValueString() != "ALLOW_ALL" {
				t.Errorf("update failure lost state: %v", resp.Diagnostics)
			}
		})
	}
}
