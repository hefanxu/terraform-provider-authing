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

type mockDataResource struct {
	sync.Mutex
	namespace, resource, name, description                                                            string
	paths                                                                                             []string
	namespaceDeletes, resourceDeletes                                                                 int
	foreign, extensions, nonempty, wrongIdentity, extraResource, extraPolicy, badInventory, failDrift bool
}

func (m *mockDataResource) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	var body map[string]json.RawMessage
	if r.Method == http.MethodPost && r.URL.Path != "/api/v3/get-management-token" {
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "invalid body", 500)
			return
		}
	}
	str := func(key string) string { var s string; _ = json.Unmarshal(body[key], &s); return s }
	ok := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": data}) }
	absent := func() { fmt.Fprint(w, `{"statusCode":404}`) }
	invalid := func() { http.Error(w, "unexpected operation", 500) }
	ns, code := namespaceTestCode, dataResourceCode(namespaceTestCode)
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
	case "/api/v3/get-permission-namespace":
		if r.URL.Query().Get("code") != ns || m.namespace == "" {
			absent()
			return
		}
		ok(map[string]string{"code": ns, "name": ns, "description": "hermesacc ownership " + ns})
	case "/api/v3/create-permission-namespace":
		if m.namespace != "" || str("code") != ns || str("name") != ns || str("description") != "hermesacc ownership "+ns {
			invalid()
			return
		}
		m.namespace = ns
		ok(map[string]string{"code": ns, "name": ns, "description": "hermesacc ownership " + ns})
	case "/api/v3/delete-permission-namespace":
		if str("code") != ns || m.namespace == "" || m.resource != "" || m.extraResource || m.extraPolicy {
			invalid()
			return
		}
		m.namespace = ""
		m.namespaceDeletes++
		ok(map[string]any{"success": true})
	case "/api/v3/get-data-resource":
		if r.URL.Query().Get("namespaceCode") != ns || r.URL.Query().Get("resourceCode") != code || m.resource == "" {
			absent()
			return
		}
		resourceCode := code
		if m.wrongIdentity {
			resourceCode = "foreign"
		}
		description := m.description
		if m.foreign {
			description = "foreign"
		}
		structure := []string{}
		if m.nonempty {
			structure = []string{"row"}
		}
		fields := []any{}
		if m.extensions {
			fields = []any{map[string]any{"key": "unsafe", "type": "STRING"}}
		}
		ok(map[string]any{"namespaceCode": ns, "resourceCode": resourceCode, "resourceName": m.name, "description": description, "type": "ARRAY", "struct": structure, "actions": []string{}, "extendFieldList": fields})
	case "/api/v3/create-data-resource":
		if m.namespace != ns || m.resource != "" || str("namespaceCode") != ns || str("resourceCode") != code || str("resourceName") != code || str("type") != "ARRAY" || str("description") != dataResourceMarker(ns) || string(body["struct"]) != "[]" || string(body["actions"]) != "[]" {
			invalid()
			return
		}
		m.resource = code
		m.name = code
		m.description = dataResourceMarker(ns)
		ok(map[string]any{})
	case "/api/v3/update-data-resource":
		if m.failDrift {
			http.Error(w, "secret-marker", 500)
			return
		}
		if m.resource != code || str("namespaceCode") != ns || str("resourceCode") != code || str("description") != dataResourceMarker(ns) || string(body["struct"]) != "[]" || string(body["actions"]) != "[]" {
			invalid()
			return
		}
		m.name = str("resourceName")
		ok(map[string]any{})
	case "/api/v3/delete-data-resource":
		if m.resource != code || str("namespaceCode") != ns || str("resourceCode") != code || m.foreign || m.extensions || m.nonempty || m.wrongIdentity || m.extraPolicy {
			invalid()
			return
		}
		m.resource = ""
		m.resourceDeletes++
		ok(map[string]any{"success": true})
	case "/api/v3/list-permission-namespace-roles", "/api/v3/list-resources", "/api/v3/list-data-resources", "/api/v3/list-data-policies":
		if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "1" {
			invalid()
			return
		}
		switch r.URL.Path {
		case "/api/v3/list-permission-namespace-roles":
			if r.URL.Query().Get("code") != ns {
				invalid()
				return
			}
		case "/api/v3/list-resources":
			if r.URL.Query().Get("namespace") != ns {
				invalid()
				return
			}
		case "/api/v3/list-data-resources":
			if r.URL.Query().Get("namespaceCodes") != `["`+ns+`"]` {
				invalid()
				return
			}
		}
		if m.badInventory {
			ok(map[string]any{})
			return
		}
		list := []any{}
		if r.URL.Path == "/api/v3/list-data-resources" && m.resource != "" {
			list = append(list, map[string]string{"namespaceCode": ns, "resourceCode": code})
		}
		if r.URL.Path == "/api/v3/list-resources" && m.extraResource {
			list = append(list, map[string]string{"namespace": ns, "code": "foreign"})
		}
		if r.URL.Path == "/api/v3/list-data-policies" && m.extraPolicy {
			list = append(list, map[string]string{"id": "foreign"})
		}
		data := map[string]any{"totalCount": len(list), "list": list}
		if r.URL.Path == "/api/v3/list-resources" {
			data["statusCode"] = 200
		}
		ok(data)
	default:
		invalid()
	}
}
func mockDataClient(t *testing.T, m *mockDataResource) (*authingapi.Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key-marker", AccessKeySecret: "credential-marker", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}
func TestMockDataResourceTerraformTrace(t *testing.T) {
	m := &mockDataResource{}
	_, server := mockDataClient(t, m)
	defer server.Close()
	if err := runDataResourceTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, namespaceTestCode); err != nil {
		t.Fatalf("%v paths=%v", err, m.paths)
	}
	m.Lock()
	defer m.Unlock()
	if m.namespace != "" || m.resource != "" || m.resourceDeletes != 1 || m.namespaceDeletes != 1 {
		t.Fatalf("incomplete lifecycle: %+v", m)
	}
	paths := strings.Join(m.paths, ",")
	if strings.Index(paths, "POST /api/v3/create-data-resource") < strings.Index(paths, "POST /api/v3/create-permission-namespace") || strings.Index(paths, "POST /api/v3/delete-permission-namespace") < strings.Index(paths, "POST /api/v3/delete-data-resource") || strings.Count(paths, "POST /api/v3/update-data-resource") < 2 {
		t.Fatal("dependency ordering or drift reconciliation missing")
	}
}
func TestDataResourceCleanupRefusesUnsafeResource(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*mockDataResource)
	}{
		{"foreign", func(m *mockDataResource) { m.foreign = true }},
		{"extensions", func(m *mockDataResource) { m.extensions = true }},
		{"nonempty", func(m *mockDataResource) { m.nonempty = true }},
		{"identity", func(m *mockDataResource) { m.wrongIdentity = true }},
		{"extra-resource", func(m *mockDataResource) { m.extraResource = true }},
		{"policy", func(m *mockDataResource) { m.extraPolicy = true }},
		{"incomplete", func(m *mockDataResource) { m.badInventory = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockDataResource{namespace: namespaceTestCode, resource: dataResourceCode(namespaceTestCode), name: dataResourceCode(namespaceTestCode), description: dataResourceMarker(namespaceTestCode)}
			tc.change(m)
			client, server := mockDataClient(t, m)
			defer server.Close()
			if cleanupDataResource(client, namespaceTestCode) == nil {
				t.Fatal("accepted unsafe cleanup")
			}
			if m.resourceDeletes != 0 || m.namespaceDeletes != 0 {
				t.Fatal("deleted unsafe resource")
			}
		})
	}
}
func TestDataResourceTraceRejectsExistingAndScrubsErrors(t *testing.T) {
	m := &mockDataResource{namespace: namespaceTestCode}
	_, server := mockDataClient(t, m)
	defer server.Close()
	env := map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}
	if runDataResourceTrace(t.TempDir(), env, namespaceTestCode) == nil {
		t.Fatal("adopted existing namespace")
	}
	if runDataResourceTrace(t.TempDir(), env, "default") == nil {
		t.Fatal("accepted default namespace")
	}
	m.namespace = ""
	m.resource = dataResourceCode(namespaceTestCode)
	m.name = m.resource
	m.description = dataResourceMarker(namespaceTestCode)
	if runDataResourceTrace(t.TempDir(), env, namespaceTestCode) == nil {
		t.Fatal("adopted existing resource")
	}
	if m.resourceDeletes != 0 || m.namespaceDeletes != 0 {
		t.Fatal("preflight deleted existing identity")
	}
	m.resource = ""
	m.failDrift = true
	err := runDataResourceTrace(t.TempDir(), env, namespaceTestCode)
	if err == nil {
		t.Fatal("accepted failed mutation")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "secret-marker", "mock-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("diagnostic leaked secret")
		}
	}
	if m.namespace != "" || m.resource != "" || m.resourceDeletes != 1 || m.namespaceDeletes != 1 {
		t.Fatal("failure cleanup incomplete")
	}
}
func TestDestructiveLiveDataResourceTrace(t *testing.T) {
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
	if err = runDataResourceTrace(t.TempDir(), env, code); err != nil {
		t.Fatal(err)
	}
}
