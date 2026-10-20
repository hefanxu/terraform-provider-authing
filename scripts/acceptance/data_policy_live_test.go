package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"terraform-provider-authing/internal/authingapi"
	"testing"
)

type mockPolicy struct {
	sync.Mutex
	ns, resource, name, description                       string
	policyID                                              string
	paths                                                 []string
	policyDeletes, resourceDeletes, namespaceDeletes      int
	incompleteStatements, targets, foreign, brokenTargets bool
}

func (m *mockPolicy) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	var body map[string]json.RawMessage
	if r.Method == http.MethodPost && r.URL.Path != "/api/v3/get-management-token" {
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad request", 500)
			return
		}
	}
	s := func(key string) string { var v string; _ = json.Unmarshal(body[key], &v); return v }
	ok := func(v any) { _ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": v}) }
	absent := func() { fmt.Fprint(w, `{"statusCode":404}`) }
	invalid := func() { http.Error(w, "secret-marker", 500) }
	code := namespaceTestCode
	res := dataResourceCode(code)
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
	case "/api/v3/get-permission-namespace":
		if r.URL.Query().Get("code") != code || m.ns == "" {
			absent()
			return
		}
		ok(map[string]string{"code": code, "name": code, "description": "hermesacc ownership " + code})
	case "/api/v3/create-permission-namespace":
		if m.ns != "" || s("code") != code || s("description") != "hermesacc ownership "+code {
			invalid()
			return
		}
		m.ns = code
		ok(map[string]string{"code": code, "name": code, "description": "hermesacc ownership " + code})
	case "/api/v3/delete-permission-namespace":
		if m.ns == "" || m.resource != "" || m.policyID != "" || s("code") != code {
			invalid()
			return
		}
		m.ns = ""
		m.namespaceDeletes++
		ok(map[string]any{"success": true})
	case "/api/v3/get-data-resource":
		if r.URL.Query().Get("namespaceCode") != code || r.URL.Query().Get("resourceCode") != res || m.resource == "" {
			absent()
			return
		}
		ok(map[string]any{"namespaceCode": code, "resourceCode": res, "resourceName": res, "description": dataResourceMarker(code), "type": "ARRAY", "struct": []any{}, "actions": []string{"read"}, "extendFieldList": []any{}})
	case "/api/v3/create-data-resource":
		if m.ns == "" || m.resource != "" || s("resourceCode") != res || string(body["actions"]) != `["read"]` || string(body["struct"]) != "[]" {
			invalid()
			return
		}
		m.resource = res
		ok(map[string]any{})
	case "/api/v3/delete-data-resource":
		if m.resource == "" || m.policyID != "" || s("resourceCode") != res {
			invalid()
			return
		}
		m.resource = ""
		m.resourceDeletes++
		ok(map[string]any{"success": true})
	case "/api/v3/create-data-policy":
		if m.resource == "" || m.policyID != "" || s("policyName") != policyName(code) || s("description") != policyMarker(code) || !m.validStatements(body["statementList"], code) {
			invalid()
			return
		}
		m.policyID = "sandbox-policy-id"
		m.name = policyName(code)
		m.description = policyMarker(code)
		ok(map[string]string{"policyId": m.policyID})
	case "/api/v3/get-data-policy":
		if r.URL.Query().Get("policyId") != m.policyID || m.policyID == "" {
			absent()
			return
		}
		v := map[string]any{"policyId": m.policyID, "policyName": m.name, "description": m.description}
		if !m.incompleteStatements {
			v["statementList"] = []any{map[string]any{"effect": "DENY", "permissions": []string{policyPermission(code)}}}
		}
		ok(v)
	case "/api/v3/update-data-policy":
		if s("policyId") != m.policyID || s("description") != policyMarker(code) || !m.validStatements(body["statementList"], code) {
			invalid()
			return
		}
		m.name = s("policyName")
		ok(map[string]any{})
	case "/api/v3/delete-data-policy":
		if m.policyID == "" || s("policyId") != m.policyID || m.targets {
			invalid()
			return
		}
		m.policyID = ""
		m.policyDeletes++
		ok(map[string]any{"success": true})
	case "/api/v3/list-data-policy-targets":
		if r.URL.Query().Get("policyId") != m.policyID || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "50" {
			invalid()
			return
		}
		if m.brokenTargets {
			ok(map[string]any{"list": []any{}})
			return
		}
		entries := []any{}
		if m.targets {
			entries = append(entries, map[string]string{"targetIdentifier": "foreign", "targetType": "USER"})
		}
		ok(map[string]any{"totalCount": len(entries), "list": entries})
	case "/api/v3/list-permission-namespace-roles", "/api/v3/list-resources", "/api/v3/list-data-resources", "/api/v3/list-data-policies":
		if r.URL.Path == "/api/v3/list-data-policies" && r.URL.Query().Get("query") != "" {
			if r.URL.Query().Get("query") != policyName(code) || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "50" {
				invalid()
				return
			}
			entries := []any{}
			if m.policyID != "" {
				entries = append(entries, map[string]string{"policyId": m.policyID, "policyName": m.name})
			}
			ok(map[string]any{"totalCount": len(entries), "list": entries})
			return
		}
		if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "1" {
			invalid()
			return
		}
		entries := []any{}
		if r.URL.Path == "/api/v3/list-data-resources" && m.resource != "" {
			entries = append(entries, map[string]string{"namespaceCode": code, "resourceCode": res})
		}
		if r.URL.Path == "/api/v3/list-data-policies" && m.policyID != "" {
			entries = append(entries, map[string]string{"policyId": m.policyID})
		}
		data := map[string]any{"totalCount": len(entries), "list": entries}
		if r.URL.Path == "/api/v3/list-resources" {
			data["statusCode"] = 200
		}
		ok(data)
	default:
		invalid()
	}
}
func (m *mockPolicy) validStatements(raw json.RawMessage, code string) bool {
	var v []struct {
		Effect      string   `json:"effect"`
		Permissions []string `json:"permissions"`
	}
	return json.Unmarshal(raw, &v) == nil && len(v) == 1 && v[0].Effect == "DENY" && len(v[0].Permissions) == 1 && v[0].Permissions[0] == policyPermission(code)
}
func TestMockDataPolicyTerraformTrace(t *testing.T) {
	m := &mockPolicy{}
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	defer server.Close()
	err := runDataPolicyTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, namespaceTestCode)
	if err != nil {
		t.Fatalf("%v paths=%v", err, m.paths)
	}
	if m.ns != "" || m.resource != "" || m.policyID != "" || m.policyDeletes != 1 || m.resourceDeletes != 1 || m.namespaceDeletes != 1 {
		t.Fatalf("lifecycle incomplete: %+v", m)
	}
	paths := strings.Join(m.paths, ",")
	if strings.Count(paths, "POST /api/v3/update-data-policy") < 2 || strings.Index(paths, "POST /api/v3/delete-data-resource") < strings.Index(paths, "POST /api/v3/delete-data-policy") {
		t.Fatal("update/drift or dependency missing")
	}
}
func TestPolicyDeletionFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*mockPolicy)
	}{
		{"missing-statements", func(m *mockPolicy) { m.incompleteStatements = true }},
		{"nonempty-targets", func(m *mockPolicy) { m.targets = true }},
		{"incomplete-targets", func(m *mockPolicy) { m.brokenTargets = true }},
		{"foreign-policy", func(m *mockPolicy) { m.description = "foreign" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockPolicy{ns: namespaceTestCode, resource: dataResourceCode(namespaceTestCode), policyID: "sandbox-policy-id", name: policyName(namespaceTestCode), description: policyMarker(namespaceTestCode)}
			tc.change(m)
			server := httptest.NewServer(http.HandlerFunc(m.serve))
			defer server.Close()
			c, e := policyTestClient(server.URL)
			if e != nil {
				t.Fatal("client setup failed")
			}
			if removeOwnedPolicy(c, namespaceTestCode, "sandbox-policy-id") == nil || m.policyDeletes != 0 {
				t.Fatal("unsafe deletion accepted")
			}
		})
	}
}
func policyTestClient(host string) (*authingapi.Client, error) {
	return authingapi.NewClient(authingapi.Options{AccessKeyID: "key-marker", AccessKeySecret: "credential-marker", Host: host})
}
func TestPolicyPreflightRejectsExistingName(t *testing.T) {
	m := &mockPolicy{policyID: "foreign-id", name: policyName(namespaceTestCode), description: "foreign"}
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	defer server.Close()
	err := runDataPolicyTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, namespaceTestCode)
	if err == nil || !strings.Contains(err.Error(), "phase=preflight-policy") || m.ns != "" || m.resource != "" || m.policyDeletes != 0 {
		t.Fatal("existing policy name adopted or mutated")
	}
}

func TestMockPolicyTraceStopsOnUnavailableStatements(t *testing.T) {
	m := &mockPolicy{incompleteStatements: true}
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	defer server.Close()
	err := runDataPolicyTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, namespaceTestCode)
	if err == nil || !strings.Contains(err.Error(), "phase=verify-created") || !strings.Contains(err.Error(), "cleanup=incomplete") {
		t.Fatal("missing statement readback did not fail closed with original phase")
	}
	if m.policyDeletes != 0 || m.resourceDeletes != 0 || m.namespaceDeletes != 0 {
		t.Fatal("deleted object with unverified policy statements")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "mock-token", "secret-marker"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("secret in diagnostic")
		}
	}
}

func TestDestructiveLiveDataPolicyTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	// The published GET schema omits statementList. Do not create a policy
	// unless the sandbox owner has separately confirmed complete raw readback;
	// otherwise safe cleanup cannot be established after a failed apply.
	if os.Getenv("AUTHING_DATA_POLICY_STATEMENTS_READBACK_CONFIRMED") != "COMPLETE" {
		t.Skip("data-policy GET statementList readback not confirmed; refusing live create")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if e := destructiveGuard(*destructiveLive, env); e != nil {
		t.Fatal(e)
	}
	code, e := newNamespaceCode()
	if e != nil {
		t.Fatal("code generation failed")
	}
	if e = runDataPolicyTrace(t.TempDir(), env, code); e != nil {
		t.Fatal(e)
	}
}
