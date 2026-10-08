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

const idpTestName = "hermesacc-1234567890abcdef"

type mockExtIdp struct {
	sync.Mutex
	id, name                                                                            string
	paths                                                                               []string
	deletes                                                                             int
	failCreate, failDrift, foreignName, foreignID, hasConnections, hijackAfterReconcile bool
}

func (m *mockExtIdp) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	var body struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Type     string `json:"type"`
		TenantID string `json:"tenantId"`
	}
	if r.Method == http.MethodPost && r.URL.Path != "/api/v3/get-management-token" {
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "invalid JSON", 500)
			return
		}
	}
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
	case "/api/v3/create-ext-idp":
		if r.Method != http.MethodPost || body.Name != idpTestName || body.Type != "oidc" || body.TenantID != "" || m.id != "" {
			http.Error(w, "invalid create", 500)
			return
		}
		if m.failCreate {
			// A remote operation can fail after creating an object, without any local state.
			m.id, m.name = "foreign-idp", body.Name
			http.Error(w, "secret-marker", 500)
			return
		}
		m.id, m.name = "mock-idp-123", body.Name
		m.respond(w)
	case "/api/v3/get-ext-idp":
		if r.Method != http.MethodGet || r.URL.Query().Get("id") != m.id || m.id == "" || r.URL.Query().Get("tenantId") != "" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		m.respond(w)
	case "/api/v3/update-ext-idp":
		if m.failDrift {
			http.Error(w, "secret-marker", 500)
			return
		}
		if r.Method != http.MethodPost || body.ID != m.id || m.id == "" || body.TenantID != "" || (body.Name != idpTestName && body.Name != idpTestName+"-drift") {
			http.Error(w, "invalid update", 500)
			return
		}
		m.name = body.Name
		m.respond(w)
		if m.hijackAfterReconcile && body.Name == idpTestName {
			m.foreignName = true
		}
	case "/api/v3/delete-ext-idp":
		if r.Method != http.MethodPost || body.ID != m.id || m.id == "" || body.TenantID != "" || m.foreignName || m.name != idpTestName && m.name != idpTestName+"-drift" {
			http.Error(w, "unowned deletion", 500)
			return
		}
		m.id = ""
		m.deletes++
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	default:
		http.Error(w, "unexpected path", 404)
	}
}
func (m *mockExtIdp) respond(w http.ResponseWriter) {
	name := m.name
	if m.foreignName {
		name = "foreign name"
	}
	id := m.id
	if m.foreignID {
		id = "foreign-id"
	}
	connections := []any{}
	if m.hasConnections {
		connections = append(connections, map[string]any{"id": "foreign-conn"})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"id": id, "name": name, "type": "oidc", "tenantId": "", "connections": connections}})
}
func idpClient(t *testing.T, m *mockExtIdp) (*authingapi.Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key-marker", AccessKeySecret: "credential-marker", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}
func idpCredentials(server *httptest.Server) map[string]string {
	return map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}
}
func TestMockExtIdpTerraformTrace(t *testing.T) {
	m := &mockExtIdp{}
	_, server := idpClient(t, m)
	defer server.Close()
	if err := runExtIdpTrace(t.TempDir(), idpCredentials(server), idpTestName); err != nil {
		t.Fatalf("%v; paths=%v", err, m.paths)
	}
	m.Lock()
	defer m.Unlock()
	if m.id != "" || m.deletes != 1 {
		t.Fatalf("IdP remained or deletion count wrong: %+v", m)
	}
	counts := map[string]int{}
	for _, path := range m.paths {
		counts[path]++
	}
	for _, path := range []string{"POST /api/v3/create-ext-idp", "GET /api/v3/get-ext-idp", "POST /api/v3/update-ext-idp", "POST /api/v3/delete-ext-idp"} {
		if counts[path] == 0 {
			t.Errorf("missing %s", path)
		}
	}
	if counts["POST /api/v3/update-ext-idp"] < 2 {
		t.Fatal("drift was not reconciled")
	}
}
func TestMockExtIdpFailedCreateWithoutStateNeverDeletes(t *testing.T) {
	m := &mockExtIdp{failCreate: true}
	_, server := idpClient(t, m)
	defer server.Close()
	err := runExtIdpTrace(t.TempDir(), idpCredentials(server), idpTestName)
	if err == nil {
		t.Fatal("expected failed create")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "secret-marker", "mock-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("diagnostic leaked secret")
		}
	}
	m.Lock()
	defer m.Unlock()
	if m.id != "foreign-idp" || m.deletes != 0 {
		t.Fatal("failed create deleted foreign object")
	}
	for _, path := range m.paths {
		if path == "POST /api/v3/delete-ext-idp" {
			t.Fatal("delete attempted without state ID")
		}
	}
}
func TestMockExtIdpCleanupRejectsForeignName(t *testing.T) {
	m := &mockExtIdp{id: "mock-idp-123", name: idpTestName, foreignName: true}
	client, server := idpClient(t, m)
	defer server.Close()
	if cleanupExtIdp(client, m.id, idpTestName) == nil {
		t.Fatal("accepted foreign name")
	}
	m.Lock()
	defer m.Unlock()
	if m.deletes != 0 {
		t.Fatal("deleted foreign IdP")
	}
}
func TestMockExtIdpCleanupRejectsForeignIdentityAndConnections(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		foreignID, hasConnections bool
	}{
		{"mismatched-get-id", true, false},
		{"existing-connection", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockExtIdp{id: "mock-idp-123", name: idpTestName, foreignID: tc.foreignID, hasConnections: tc.hasConnections}
			client, server := idpClient(t, m)
			defer server.Close()
			if cleanupExtIdp(client, m.id, idpTestName) == nil {
				t.Fatal("accepted unsafe deletion")
			}
			m.Lock()
			defer m.Unlock()
			if m.deletes != 0 || m.id == "" {
				t.Fatal("deleted unverified IdP")
			}
			for _, path := range m.paths {
				if path == "POST /api/v3/delete-ext-idp" {
					t.Fatal("attempted unsafe deletion")
				}
			}
		})
	}
}

func TestMockExtIdpTraceDoesNotDestroyHijackedName(t *testing.T) {
	m := &mockExtIdp{hijackAfterReconcile: true}
	_, server := idpClient(t, m)
	defer server.Close()
	if runExtIdpTrace(t.TempDir(), idpCredentials(server), idpTestName) == nil {
		t.Fatal("accepted hijacked IdP")
	}
	m.Lock()
	defer m.Unlock()
	if m.id != "mock-idp-123" || m.deletes != 0 {
		t.Fatal("destroyed IdP after ownership loss")
	}
	for _, path := range m.paths {
		if path == "POST /api/v3/delete-ext-idp" {
			t.Fatal("delete attempted after ownership loss")
		}
	}
}

func TestMockExtIdpFailedDriftCleansOwnedID(t *testing.T) {
	m := &mockExtIdp{failDrift: true}
	_, server := idpClient(t, m)
	defer server.Close()
	err := runExtIdpTrace(t.TempDir(), idpCredentials(server), idpTestName)
	if err == nil {
		t.Fatal("expected drift failure")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "secret-marker", "mock-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("diagnostic leaked secret")
		}
	}
	m.Lock()
	defer m.Unlock()
	if m.id != "" || m.deletes != 1 {
		t.Fatal("failed to clean owned IdP")
	}
}
func TestExtIdpTraceRejectsInvalidNameWithoutNetwork(t *testing.T) {
	m := &mockExtIdp{}
	_, server := idpClient(t, m)
	defer server.Close()
	for _, name := range []string{"foreign", "hermesacc-123", "hermesacc-1234567890abcdef;"} {
		if runExtIdpTrace(t.TempDir(), idpCredentials(server), name) == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	if len(m.paths) != 0 {
		t.Fatalf("network requests for invalid names: %v", m.paths)
	}
}
