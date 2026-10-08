package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

// The mock enforces child-before-parent creation/deletion and exact namespace scope.
type mockPermissionChild struct {
	sync.Mutex
	family                                                                       string
	namespace, child, description                                                string
	paths                                                                        []string
	namespaceDeletes, childDeletes                                               int
	foreignChild, extraRole, extraResource, extraPolicy, badInventory, failDrift bool
}

func (m *mockPermissionChild) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	var body map[string]any
	if r.Method == http.MethodPost && r.URL.Path != "/api/v3/get-management-token" {
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad body", 500)
			return
		}
	}
	success := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": data}) }
	absent := func() { fmt.Fprint(w, `{"statusCode":404}`) }
	invalid := func() { http.Error(w, "invalid lifecycle", 500) }
	code := namespaceTestCode
	child := code + "-child"
	marker := "hermesacc ownership " + child
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
	case "/api/v3/get-permission-namespace":
		if r.URL.Query().Get("code") != code || m.namespace == "" {
			absent()
			return
		}
		success(map[string]string{"code": code, "name": code, "description": "hermesacc ownership " + code})
	case "/api/v3/create-permission-namespace":
		if m.namespace != "" || body["code"] != code || body["name"] != code || body["description"] != "hermesacc ownership "+code {
			invalid()
			return
		}
		m.namespace = code
		success(map[string]string{"code": code, "name": code, "description": "hermesacc ownership " + code})
	case "/api/v3/delete-permission-namespace":
		if body["code"] != code || m.namespace == "" || m.child != "" || m.extraRole || m.extraResource || m.extraPolicy {
			invalid()
			return
		}
		m.namespace = ""
		m.namespaceDeletes++
		success(map[string]any{"success": true})
	case "/api/v3/list-permission-namespace-roles", "/api/v3/list-resources", "/api/v3/list-data-resources", "/api/v3/list-data-policies":
		if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "1" {
			invalid()
			return
		}
		switch r.URL.Path {
		case "/api/v3/list-permission-namespace-roles":
			if r.URL.Query().Get("code") != code {
				invalid()
				return
			}
		case "/api/v3/list-resources":
			if r.URL.Query().Get("namespace") != code {
				invalid()
				return
			}
		case "/api/v3/list-data-resources":
			if r.URL.Query().Get("namespaceCodes") != `["`+code+`"]` {
				invalid()
				return
			}
		}
		if m.badInventory {
			success(map[string]any{})
			return
		}
		count := 0
		if (r.URL.Path == "/api/v3/list-permission-namespace-roles" && (m.extraRole || m.family == "role" && m.child != "")) || (r.URL.Path == "/api/v3/list-resources" && (m.extraResource || m.family == "resource" && m.child != "")) || (r.URL.Path == "/api/v3/list-data-policies" && m.extraPolicy) {
			count = 1
		}
		list := []any{}
		if count == 1 && !m.extraRole && !m.extraResource && (r.URL.Path == "/api/v3/list-permission-namespace-roles" || r.URL.Path == "/api/v3/list-resources") {
			list = append(list, map[string]string{"code": child, "namespace": code})
		}
		payload := map[string]any{"totalCount": count, "list": list}
		if r.URL.Path == "/api/v3/list-resources" {
			payload["statusCode"] = 200
		}
		success(payload)
	case "/api/v3/get-role", "/api/v3/get-resource":
		if r.URL.Query().Get("code") != child || r.URL.Query().Get("namespace") != code || m.child == "" {
			absent()
			return
		}
		desc := m.description
		if m.foreignChild {
			desc = "foreign"
		}
		if m.family == "role" {
			success(map[string]any{"code": child, "namespace": code, "name": child, "description": desc})
			return
		}
		success(map[string]any{"code": child, "namespace": code, "type": "BUTTON", "description": desc, "actions": []any{}})
	case "/api/v3/create-role", "/api/v3/create-resource":
		if m.namespace != code || m.child != "" || body["code"] != child || body["namespace"] != code || body["description"] != marker {
			invalid()
			return
		}
		if m.family == "role" && body["name"] != child {
			invalid()
			return
		}
		if m.family == "resource" && (body["type"] != "BUTTON" || body["actions"] != nil && len(body["actions"].([]any)) != 0) {
			invalid()
			return
		}
		m.child = child
		m.description = marker
		data := map[string]any{"code": child, "namespace": code, "description": marker, "name": child}
		if m.family == "resource" {
			data["type"] = "BUTTON"
		}
		success(data)
	case "/api/v3/update-role", "/api/v3/update-resource":
		if m.failDrift {
			http.Error(w, "secret-marker", 500)
			return
		}
		if m.child != child || body["code"] != child || body["namespace"] != code {
			invalid()
			return
		}
		if m.family == "role" && (body["name"] != child || body["newCode"] != child) {
			invalid()
			return
		}
		if m.family == "resource" && (body["type"] != "BUTTON" || body["actions"] != nil && len(body["actions"].([]any)) != 0) {
			invalid()
			return
		}
		m.description = body["description"].(string)
		success(map[string]any{"code": child, "namespace": code, "description": m.description})
	case "/api/v3/delete-roles-batch", "/api/v3/delete-resource":
		if m.child != child || body["namespace"] != code || m.foreignChild {
			invalid()
			return
		}
		if m.family == "role" {
			codes, ok := body["codeList"].([]any)
			if !ok || len(codes) != 1 || codes[0] != child {
				invalid()
				return
			}
		} else if body["code"] != child {
			invalid()
			return
		}
		m.child = ""
		m.childDeletes++
		success(map[string]any{"success": true})
	default:
		invalid()
	}
}
func mockChildClient(t *testing.T, m *mockPermissionChild) (*authingapi.Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key-marker", AccessKeySecret: "credential-marker", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}
func TestMockPermissionChildTerraformTrace(t *testing.T) {
	for _, family := range []string{"role", "resource"} {
		t.Run(family, func(t *testing.T) {
			m := &mockPermissionChild{family: family}
			_, server := mockChildClient(t, m)
			defer server.Close()
			err := runPermissionChildTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, namespaceTestCode, family)
			if err != nil {
				m.Lock()
				paths := append([]string(nil), m.paths...)
				desc := m.description
				m.Unlock()
				t.Fatalf("%v paths=%v description=%q", err, paths, desc)
			}
			m.Lock()
			defer m.Unlock()
			if m.namespace != "" || m.child != "" || m.childDeletes != 1 || m.namespaceDeletes != 1 {
				t.Fatalf("lifecycle incomplete: %+v", m)
			}
			paths := strings.Join(m.paths, ",")
			createChild := "POST /api/v3/create-" + family
			deleteChild := "POST /api/v3/delete-resource"
			if family == "role" {
				deleteChild = "POST /api/v3/delete-roles-batch"
			}
			createNS := "POST /api/v3/create-permission-namespace"
			deleteNS := "POST /api/v3/delete-permission-namespace"
			if strings.Index(paths, createNS) < 0 || strings.Index(paths, createChild) < strings.Index(paths, createNS) || strings.Index(paths, deleteChild) < 0 || strings.Index(paths, deleteNS) < strings.Index(paths, deleteChild) {
				t.Fatal("namespace dependency ordering was not enforced")
			}
			if strings.Count(paths, "POST /api/v3/update-"+family) < 2 {
				t.Fatal("description drift was not reconciled")
			}
			for _, endpoint := range []string{"create-" + family, "get-" + family, "update-" + family, "delete-permission-namespace", "list-data-policies"} {
				if !strings.Contains(paths, "/api/v3/"+endpoint) {
					t.Errorf("missing endpoint %s", endpoint)
				}
			}
		})
	}
}
func TestPermissionChildCleanupRefusesForeignAndNonemptyInventories(t *testing.T) {
	for _, tc := range []struct {
		name                                        string
		foreign, role, resource, policy, incomplete bool
	}{
		{name: "foreign", foreign: true}, {name: "extra-role", role: true}, {name: "extra-resource", resource: true}, {name: "extra-policy", policy: true}, {name: "incomplete", incomplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockPermissionChild{family: "role", namespace: namespaceTestCode, child: namespaceTestCode + "-child", description: "hermesacc ownership " + namespaceTestCode + "-child", foreignChild: tc.foreign, extraRole: tc.role, extraResource: tc.resource, extraPolicy: tc.policy, badInventory: tc.incomplete}
			client, server := mockChildClient(t, m)
			defer server.Close()
			if tc.foreign {
				if cleanupPermissionChild(client, namespaceTestCode, "role") == nil {
					t.Fatal("deleted foreign child")
				}
			} else {
				if verifyChildNamespaceInventory(client, namespaceTestCode, "role", true) == nil {
					t.Fatal("accepted unsafe inventory")
				}
			}
			if m.namespaceDeletes != 0 || m.childDeletes != 0 {
				t.Fatal("deleted without ownership or empty inventory")
			}
		})
	}
}
func TestPermissionChildTraceRejectsExistingAndScrubsErrors(t *testing.T) {
	m := &mockPermissionChild{family: "role", namespace: namespaceTestCode}
	_, server := mockChildClient(t, m)
	defer server.Close()
	env := map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}
	if runPermissionChildTrace(t.TempDir(), env, namespaceTestCode, "role") == nil {
		t.Fatal("adopted existing namespace")
	}
	if runPermissionChildTrace(t.TempDir(), env, "default", "role") == nil {
		t.Fatal("accepted default namespace")
	}
	if runPermissionChildTrace(t.TempDir(), env, namespaceTestCode, "other") == nil {
		t.Fatal("accepted unknown family")
	}
	if m.namespaceDeletes != 0 {
		t.Fatal("deleted existing namespace")
	}
	m.namespace = ""
	m.child = namespaceTestCode + "-child"
	m.description = "foreign"
	if runPermissionChildTrace(t.TempDir(), env, namespaceTestCode, "role") == nil {
		t.Fatal("adopted existing child in an absent namespace")
	}
	if m.childDeletes != 0 || m.namespaceDeletes != 0 {
		t.Fatal("preflight deleted an existing child")
	}
	m.child = ""
	m.failDrift = true
	err := runPermissionChildTrace(t.TempDir(), env, namespaceTestCode, "role")
	if err == nil {
		t.Fatal("accepted failed drift")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "secret-marker", "mock-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("error leaked secret")
		}
	}
	if m.namespace != "" || m.child != "" || m.namespaceDeletes != 1 || m.childDeletes != 1 {
		t.Fatal("failure did not clean up both owned identities")
	}
}
func TestDestructiveLiveRoleTrace(t *testing.T)     { runLivePermissionChildTrace(t, "role") }
func TestDestructiveLiveResourceTrace(t *testing.T) { runLivePermissionChildTrace(t, "resource") }
func runLivePermissionChildTrace(t *testing.T, family string) {
	t.Helper()
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	code, err := newNamespaceCode()
	if err != nil {
		t.Fatal("namespace code generation failed")
	}
	if err = runPermissionChildTrace(t.TempDir(), env, code, family); err != nil {
		t.Fatal(err)
	}
}
