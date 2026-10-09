package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

// Every request is checked, including the only POSTs (authentication and list
// operations). No tenant/user/org write endpoint is accepted by this fixture.
func recoveryClient(t *testing.T, alter func(string, int, map[string]any) (int, any)) (*authingapi.Client, *[]string) {
	t.Helper()
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		reply := func(status int, data any) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(data)
		}
		if r.URL.Path == "/api/v3/get-management-token" {
			if r.Method != "POST" {
				t.Error("wrong token method")
			}
			reply(200, map[string]any{"statusCode": 200, "data": map[string]any{"access_token": "token-marker", "expires_in": 3600}})
			return
		}
		var data any
		page := 1
		switch r.URL.Path {
		case "/api/v3/list-tenants", "/api/v3/list-organizations":
			if r.Method != "GET" || len(r.URL.Query()) != 2 && r.URL.Path == "/api/v3/list-tenants" || len(r.URL.Query()) != 3 && r.URL.Path == "/api/v3/list-organizations" || r.URL.Query().Get("limit") != "50" {
				t.Error("filtered or invalid list request")
			}
			page, _ = strconv.Atoi(r.URL.Query().Get("page"))
			if r.URL.Path == "/api/v3/list-organizations" && r.URL.Query().Get("tenantId") != "raw-id-marker" {
				t.Error("unscoped org request")
			}
		case "/api/v3/get-tenant":
			if r.Method != "GET" || r.URL.Query().Get("tenantId") != "raw-id-marker" || len(r.URL.Query()) != 1 {
				t.Error("wrong GET identity")
			}
			data = map[string]any{"tenantId": "raw-id-marker", "name": incidentTenantName, "appIds": []string{}, "description": "pii-marker"}
		case "/api/v3/list-tenant-users", "/api/v3/list-tenant-admin":
			var body map[string]json.RawMessage
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || string(body["tenantId"]) != `"raw-id-marker"` {
				t.Error("unscoped POST")
			}
			if r.URL.Path == "/api/v3/list-tenant-admin" {
				var p string
				_ = json.Unmarshal(body["page"], &p)
				page, _ = strconv.Atoi(p)
				if len(body) != 3 || string(body["limit"]) != `"50"` {
					t.Error("admin page/limit not strings")
				}
			} else {
				var options struct{ Pagination struct{ Page, Limit int } }
				if len(body) != 2 || json.Unmarshal(body["options"], &options) != nil || options.Pagination.Limit != 50 {
					t.Error("bad member pagination")
				}
				page = options.Pagination.Page
			}
		default:
			t.Error("mutation or unknown request")
			reply(500, map[string]any{"statusCode": 500})
			return
		}
		if page < 1 || page > 2 {
			t.Error("unexpected page")
		}
		if data == nil {
			list := []any{}
			for i := (page - 1) * 50; i < min(page*50, 51); i++ {
				switch r.URL.Path {
				case "/api/v3/list-tenants":
					name, id := "pii-marker", fmt.Sprint("tenant-", i)
					if i == 0 {
						name = incidentTenantName
						id = "raw-id-marker"
					}
					list = append(list, map[string]any{"tenantId": id, "name": name})
				case "/api/v3/list-organizations":
					list = append(list, map[string]any{"organizationCode": fmt.Sprint("org-", i), "tenantId": "raw-id-marker"})
				default:
					list = append(list, map[string]any{"memberId": fmt.Sprint("member-", i), "tenantId": "raw-id-marker", "email": "pii-marker"})
				}
			}
			data = map[string]any{"totalCount": 51, "list": list}
		}
		envelope := map[string]any{"statusCode": 200, "data": data, "message": "body-marker"}
		status := 200
		var body any = envelope
		if alter != nil {
			status, body = alter(r.URL.Path, page, envelope)
		}
		reply(status, body)
	}))
	t.Cleanup(server.Close)
	client, err := authingapi.NewClient(authingapi.Options{Host: server.URL, AccessKeyID: "key-marker", AccessKeySecret: "secret-marker"})
	if err != nil {
		t.Fatal(err)
	}
	return client, &calls
}

func TestTenantRecoveryCompleteReadOnlyCandidate(t *testing.T) {
	client, calls := recoveryClient(t, nil)
	result := probeTenantRecovery(client, incidentTenantName)
	if len(*calls) != 10 || result.List.Category != "candidate" || result.List.Count != 51 || result.GET.Category != "candidate" || result.Apps.Count != 0 || result.Users.Count != 51 || result.Admins.Count != 51 || result.Orgs.Count != 51 {
		t.Fatalf("incomplete probe: %+v calls=%v", result, *calls)
	}
	for _, stage := range []tenantProbeStage{result.List, result.Users, result.Admins, result.Orgs} {
		if stage.Pages != 2 {
			t.Fatal("did not exhaust pagination")
		}
	}
	output := formatTenantRecovery(result)
	for _, private := range []string{"raw-id-marker", "pii-marker", "body-marker", "token-marker", "key-marker", "secret-marker"} {
		if strings.Contains(output, private) {
			t.Fatal("private data leaked")
		}
	}
	if !strings.Contains(output, "authority=none") || !strings.Contains(output, incidentTenantName) {
		t.Fatal("candidate wrongly authorized or name missing")
	}
}

func TestTenantStatePinnedDiagnosticPreservesFirstPhase(t *testing.T) {
	m := &mockTenant{admins: true}
	_, s := mockTenantClient(t, m)
	defer s.Close()
	err := runTenantTrace(t.TempDir(), postCredentials(s), tenantTestName)
	if err == nil || !strings.Contains(err.Error(), "phase=verify-created") || !strings.Contains(err.Error(), "cleanup=incomplete") || !strings.Contains(err.Error(), "stage=admins category=nonempty") {
		t.Fatalf("lost first stage: %v", err)
	}
	for _, value := range []string{"tenant-unique-id", "mock-token", "credential-marker", "key-marker"} {
		if strings.Contains(err.Error(), value) {
			t.Fatal("private data in tracer")
		}
	}
	if m.deletes != 0 {
		t.Fatal("unsafe delete")
	}
}

func TestTenantRecoveryGuardExactIncident(t *testing.T) {
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}
	if !tenantRecoveryGuard(true, env, "hermesacc-97f07719b2e15f0c") {
		t.Fatal("valid guard refused")
	}
	for _, name := range []string{"", tenantTestName, "hermesacc-97f07719b2e15f0c-updated", "hermesacc-97f07719b2e15f0c\n"} {
		if tenantRecoveryGuard(true, env, name) {
			t.Fatal("unapproved incident accepted")
		}
	}
	if tenantRecoveryGuard(false, env, "hermesacc-97f07719b2e15f0c") {
		t.Fatal("missing opt-in accepted")
	}
	for _, key := range []string{"AUTHING_ACCEPTANCE_CONFIRM", "AUTHING_ACCESS_KEY_ID", "AUTHING_ACCESS_KEY_SECRET"} {
		copy := map[string]string{}
		for k, v := range env {
			copy[k] = v
		}
		copy[key] = ""
		if tenantRecoveryGuard(true, copy, "hermesacc-97f07719b2e15f0c") {
			t.Fatal("missing gate accepted")
		}
	}
	env["AUTHING_ACCEPTANCE_CONFIRM"] = "READ_ONLY_SANDBOX "
	if tenantRecoveryGuard(true, env, "hermesacc-97f07719b2e15f0c") {
		t.Fatal("inexact confirmation accepted")
	}
}
