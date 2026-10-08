package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

type mockApplication struct {
	sync.Mutex
	id, name, marker, identifier, strategy string
	paths                                  []string
	failStrategy                           bool
	failCreateValidation                   bool
	rejectUnconfiguredNested               bool
	failDriftUpdate                        bool
	deletes                                int
}

func (a *mockApplication) serve(w http.ResponseWriter, r *http.Request) {
	a.Lock()
	defer a.Unlock()
	a.paths = append(a.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	var body map[string]any
	if r.Method == "POST" && r.URL.Path != "/api/v3/get-management-token" {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
	case "/api/v3/list-applications":
		list := []any{}
		if a.id != "" {
			list = append(list, a.data())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"list": list, "totalCount": len(list)}})
	case "/api/v3/create-application":
		if a.failCreateValidation {
			fmt.Fprint(w, `{"statusCode":400,"apiCode":123456,"message":"invalid loginConfig secret-marker https://private.invalid/token app-id-marker"}`)
			return
		}
		if a.rejectUnconfiguredNested {
			for _, key := range []string{"oidcConfig", "samlConfig", "oauthConfig", "casConfig", "loginConfig", "registerConfig", "brandingConfig"} {
				if _, present := body[key]; present {
					fmt.Fprint(w, `{"statusCode":400,"message":"unconfigured nested application options"}`)
					return
				}
			}
		}
		if a.id != "" || !strings.HasPrefix(body["appName"].(string), "hermesacc-") || body["appType"] != "web" || body["ssoEnabled"] != false || body["appIdentifier"] != body["appName"] || body["appDescription"] != "hermesacc ownership "+body["appName"].(string) || body["redirectUris"].([]any)[0] != "https://example.invalid/callback" {
			http.Error(w, "invalid create", 500)
			return
		}
		a.id, a.name, a.identifier, a.marker, a.strategy = "mock-app-123", body["appName"].(string), body["appIdentifier"].(string), body["appDescription"].(string), "ALLOW_ALL"
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": a.data()})
	case "/api/v3/get-application":
		if r.URL.Query().Get("appId") != a.id || a.id == "" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": a.data()})
	case "/api/v3/get-application-permission-strategy":
		if r.URL.Query().Get("appId") != a.id || a.id == "" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]string{"permissionStrategy": a.strategy}})
	case "/api/v3/update-application-permission-strategy":
		if a.failStrategy {
			http.Error(w, "secret-marker", 500)
			return
		}
		if body["appId"] != a.id || body["permissionStrategy"] != "DENY_ALL" {
			http.Error(w, "invalid strategy", 500)
			return
		}
		a.strategy = "DENY_ALL"
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	case "/api/v3/update-application":
		if a.failDriftUpdate {
			fmt.Fprint(w, `{"statusCode":422,"message":"secret-marker private-uri"}`)
			return
		}
		if body["appId"] != a.id || body["appName"] == nil {
			http.Error(w, "invalid update", 500)
			return
		}
		a.name = body["appName"].(string)
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	case "/api/v3/delete-application":
		if body["appId"] != a.id || a.id == "" {
			http.Error(w, "unowned deletion", 500)
			return
		}
		a.id = ""
		a.deletes++
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	default:
		http.Error(w, "unexpected", 404)
	}
}
func (a *mockApplication) data() map[string]any {
	return map[string]any{"appId": a.id, "appName": a.name, "appIdentifier": a.identifier, "appDescription": a.marker, "appType": "web", "redirectUris": []string{"https://example.invalid/callback"}, "logoutRedirectUris": []string{}, "ssoEnabled": false}
}
func TestMockApplicationTrace(t *testing.T) {
	a := &mockApplication{}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	name := "hermesacc-1234567890abcdef"
	if err := runApplicationTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, name); err != nil {
		t.Fatal(err)
	}
	a.Lock()
	defer a.Unlock()
	if a.id != "" || a.deletes != 1 {
		t.Fatalf("application remained: %+v", a)
	}
	for _, path := range []string{"POST /api/v3/create-application", "POST /api/v3/update-application-permission-strategy", "POST /api/v3/update-application", "POST /api/v3/delete-application"} {
		if !strings.Contains(strings.Join(a.paths, ","), path) {
			t.Fatalf("missing %s: %v", path, a.paths)
		}
	}
}
func TestMockApplicationFailedStrategyCleanup(t *testing.T) {
	a := &mockApplication{failStrategy: true}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	name := "hermesacc-1234567890abcdef"
	err := runApplicationTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, name)
	if err == nil {
		t.Fatal("expected strategy error")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "secret-marker", "mock-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("leaked secret")
		}
	}
	a.Lock()
	defer a.Unlock()
	if a.id != "mock-app-123" || a.deletes != 0 {
		t.Fatalf("deleted application without state-backed ID: %+v", a)
	}
}
func TestMockApplicationCleanupRefusesForeignName(t *testing.T) {
	a := &mockApplication{id: "mock-app-123", name: "foreign", identifier: "hermesacc-1234567890abcdef", marker: "hermesacc ownership hermesacc-1234567890abcdef"}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if cleanupApplication(client, a.id, "hermesacc-1234567890abcdef", "hermesacc ownership hermesacc-1234567890abcdef") == nil {
		t.Fatal("accepted foreign name")
	}
	a.Lock()
	defer a.Unlock()
	if a.deletes != 0 {
		t.Fatal("deleted foreign app")
	}
}
func TestMockApplicationRefusesExistingName(t *testing.T) {
	name := "hermesacc-1234567890abcdef"
	a := &mockApplication{id: "existing-app", name: name, identifier: name, marker: "other owner"}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	if err := runApplicationTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, name); err == nil {
		t.Fatal("existing name accepted")
	}
	a.Lock()
	defer a.Unlock()
	if a.id != "existing-app" || a.deletes != 0 {
		t.Fatal("preflight touched existing app")
	}
}

func TestMockApplicationCleanupRefusesForeignMarker(t *testing.T) {
	name := "hermesacc-1234567890abcdef"
	a := &mockApplication{id: "existing-app", name: name, identifier: name, marker: "other owner"}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if cleanupApplication(client, a.id, name, "hermesacc ownership "+name) == nil {
		t.Fatal("accepted foreign marker")
	}
	a.Lock()
	defer a.Unlock()
	if a.deletes != 0 {
		t.Fatal("deleted foreign app")
	}
}

func TestApplicationLiveGuard(t *testing.T) {
	for _, tc := range []struct {
		flag    bool
		confirm string
		allowed bool
	}{{false, "DESTRUCTIVE_SANDBOX", false}, {true, "READ_ONLY_SANDBOX", false}, {true, "DESTRUCTIVE_SANDBOX ", false}, {true, "DESTRUCTIVE_SANDBOX", true}} {
		env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": tc.confirm, "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}
		if (destructiveGuard(tc.flag, env) == nil) != tc.allowed {
			t.Fatalf("guard failed for %+v", tc)
		}
	}
}
