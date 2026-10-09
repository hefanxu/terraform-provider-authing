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

const flowTestName = "hermesacc-1234567890abcdef"

type mockFlow struct {
	sync.Mutex
	id, name                  string
	enabled                   bool
	creates, updates, deletes int
	failReconcile             bool
	failCreate                bool
	foreignSource             bool
	blockCleanup              bool
	cleanupBlocked            bool
	paths                     []string
}

func (m *mockFlow) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/v3/get-management-token" {
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
		return
	}
	if r.URL.Path == "/api/v3/get-auth-flow-function" {
		if m.cleanupBlocked {
			http.Error(w, "get-secret-marker", 500)
			return
		}
		if r.Method != http.MethodGet || r.URL.Query().Get("funcId") != m.id || m.id == "" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		source := flowInertSource
		if m.foreignSource {
			source = "foreign source"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"funcId": m.id, "funcName": m.name, "funcDescription": flowMarker(flowTestName), "scene": flowScene, "sourceCode": source, "enabled": m.enabled, "isAsynchronous": false, "timeout": 3, "terminateOnTimeout": false}})
		return
	}
	var body map[string]any
	if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil {
		http.Error(w, "invalid", 500)
		return
	}
	switch r.URL.Path {
	case "/api/v3/create-auth-flow-function":
		if m.failCreate {
			http.Error(w, "create-secret-marker", 500)
			return
		}
		if m.id != "" || body["funcName"] != flowTestName || body["scene"] != flowScene || body["sourceCode"] != flowInertSource || body["funcDescription"] != flowMarker(flowTestName) || body["enabled"] != false || body["isAsynchronous"] != false {
			http.Error(w, "invalid create", 500)
			return
		}
		m.id, m.name = "mock-flow-123", flowTestName
		m.creates++
		fmt.Fprint(w, `{"statusCode":200,"data":{"funcId":"mock-flow-123"}}`)
	case "/api/v3/update-auth-flow-function":
		if m.failReconcile && body["funcName"] == flowTestName {
			m.cleanupBlocked = m.blockCleanup
			http.Error(w, "response-secret-marker", 500)
			return
		}
		if m.id == "" || body["funcId"] != m.id || body["funcName"] != flowTestName && body["funcName"] != flowTestName+"-drift" || body["enabled"] != nil && body["enabled"] != false {
			http.Error(w, "invalid update", 500)
			return
		}
		m.name = body["funcName"].(string)
		m.updates++
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]string{"funcId": m.id}})
	case "/api/v3/delete-auth-flow-function":
		if m.id == "" || body["funcId"] != m.id || m.enabled {
			http.Error(w, "unsafe deletion", 500)
			return
		}
		m.id = ""
		m.deletes++
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	default:
		http.Error(w, "unexpected endpoint", 404)
	}
}
func flowCredentials(s *httptest.Server) map[string]string {
	return map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": s.URL}
}
func TestMockAuthFlowFunctionTrace(t *testing.T) {
	m := &mockFlow{}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	if err := runAuthFlowTrace(t.TempDir(), flowCredentials(s), flowTestName); err != nil {
		t.Fatalf("%v; paths=%v", err, m.paths)
	}
	m.Lock()
	defer m.Unlock()
	if m.id != "" || m.creates != 1 || m.updates < 2 || m.deletes != 1 {
		t.Fatalf("lifecycle counts create=%d update=%d delete=%d id=%q", m.creates, m.updates, m.deletes, m.id)
	}
	for _, p := range m.paths {
		switch p {
		case "POST /api/v3/get-management-token", "GET /api/v3/get-auth-flow-function", "POST /api/v3/create-auth-flow-function", "POST /api/v3/update-auth-flow-function", "POST /api/v3/delete-auth-flow-function":
		default:
			t.Fatalf("unexpected request %s", p)
		}
	}
}
func TestMockAuthFlowFailedReconcileCleansUp(t *testing.T) {
	m := &mockFlow{failReconcile: true}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	err := runAuthFlowTrace(t.TempDir(), flowCredentials(s), flowTestName)
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), "phase=apply-reconcile") || !strings.Contains(err.Error(), "code="+flowTestName) || !strings.Contains(err.Error(), "cleanup=confirmed") {
		t.Fatalf("lost original phase or verified cleanup: %v", err)
	}
	for _, secret := range []string{"key-marker", "credential-marker", "response-secret-marker", "mock-token", flowInertSource} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("sensitive material leaked")
		}
	}
	m.Lock()
	defer m.Unlock()
	if m.id != "" || m.deletes != 1 {
		t.Fatal("failed to clean exact owned ID")
	}
}
func TestAuthFlowCleanupRefusesChangedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*mockFlow)
	}{
		{"foreign name", func(m *mockFlow) { m.name = "foreign" }},
		{"enabled", func(m *mockFlow) { m.enabled = true }},
		{"foreign source", func(m *mockFlow) { m.foreignSource = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockFlow{id: "mock-flow-123", name: flowTestName}
			tc.mutate(m)
			s := httptest.NewServer(http.HandlerFunc(m.serve))
			defer s.Close()
			c, e := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: s.URL})
			if e != nil {
				t.Fatal(e)
			}
			if cleanupAuthFlow(c, "mock-flow-123", flowTestName) == nil || m.deletes != 0 {
				t.Fatal("unsafe cleanup authorized")
			}
		})
	}
}
func TestAuthFlowFailedCreateWithoutStateRefusesGuessedCleanup(t *testing.T) {
	m := &mockFlow{failCreate: true}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	err := runAuthFlowTrace(t.TempDir(), flowCredentials(s), flowTestName)
	if err == nil || !strings.Contains(err.Error(), "phase=apply-create") || !strings.Contains(err.Error(), "code="+flowTestName) || !strings.Contains(err.Error(), "cleanup=incomplete") || strings.Contains(err.Error(), "create-secret-marker") {
		t.Fatalf("failed create did not report safe cleanup boundary: %v", err)
	}
	m.Lock()
	defer m.Unlock()
	if m.creates != 0 || m.deletes != 0 {
		t.Fatal("no-state failure touched function")
	}
}
func TestAuthFlowCleanupUncertainRetainsFirstPhase(t *testing.T) {
	m := &mockFlow{failReconcile: true, blockCleanup: true}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	err := runAuthFlowTrace(t.TempDir(), flowCredentials(s), flowTestName)
	if err == nil || !strings.Contains(err.Error(), "phase=apply-reconcile") || !strings.Contains(err.Error(), "code="+flowTestName) || !strings.Contains(err.Error(), "cleanup=unknown") || strings.Contains(err.Error(), "get-secret-marker") {
		t.Fatalf("uncertain cleanup lost the first failure or leaked body: %v", err)
	}
	if m.deletes != 0 {
		t.Fatal("deleted without exact GET")
	}
}
func TestAuthFlowTraceRejectsInvalidName(t *testing.T) {
	if runAuthFlowTrace(t.TempDir(), nil, "production") == nil {
		t.Fatal("accepted non-generated name")
	}
}
