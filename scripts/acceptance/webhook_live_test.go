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

const webhookTestName = "hermesacc-1234567890abcdef"

type mockWebhook struct {
	sync.Mutex
	id, name      string
	paths         []string
	deletes       int
	failReconcile bool
	invalidCreate map[string]any
	invalidUpdate map[string]any
}

func (m *mockWebhook) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	var body map[string]any
	if r.Method == http.MethodPost && r.URL.Path != "/api/v3/get-management-token" {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
	case "/api/v3/list-webhooks":
		list := []any{}
		if m.id != "" {
			list = append(list, m.data())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"list": list, "totalCount": len(list)}})
	case "/api/v3/create-webhook":
		if m.id != "" || body["name"] != webhookTestName || body["url"] != webhookInertURL || body["enabled"] != false || body["contentType"] != "application/json" || body["secret"] != nil || len(body["events"].([]any)) != 1 || body["events"].([]any)[0] != webhookEvent {
			m.invalidCreate = map[string]any{"enabled": body["enabled"], "contentType": body["contentType"], "events": body["events"]}
			http.Error(w, "invalid create", 500)
			return
		}
		m.id, m.name = "mock-webhook-123", webhookTestName
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": m.data()})
	case "/api/v3/get-webhook":
		if m.id == "" || r.URL.Query().Get("webhookId") != m.id {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": m.data()})
	case "/api/v3/update-webhook":
		if m.failReconcile && body["name"] == webhookTestName {
			http.Error(w, "failure-marker", 500)
			return
		}
		if m.id == "" || body["webhookId"] != m.id || body["url"] != webhookInertURL || body["enabled"] != false || body["secret"] != nil || body["name"] != webhookTestName && body["name"] != webhookTestName+"-drift" {
			m.invalidUpdate = map[string]any{"webhookId": body["webhookId"], "name": body["name"], "url": body["url"], "enabled": body["enabled"]}
			http.Error(w, "invalid update", 500)
			return
		}
		m.name = body["name"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": m.data()})
	case "/api/v3/delete-webhook":
		ids, ok := body["webhookIds"].([]any)
		if !ok || len(ids) != 1 || ids[0] != m.id || m.id == "" {
			http.Error(w, "unowned deletion", 500)
			return
		}
		m.id = ""
		m.deletes++
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	default:
		http.Error(w, "unexpected request", 404)
	}
}
func (m *mockWebhook) data() map[string]any {
	return map[string]any{"webhookId": m.id, "name": m.name, "url": webhookInertURL, "enabled": false, "contentType": "application/json"}
}
func TestMockWebhookTrace(t *testing.T) {
	m := &mockWebhook{}
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	defer server.Close()
	if err := runWebhookTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, webhookTestName); err != nil {
		m.Lock()
		detail, update, paths := m.invalidCreate, m.invalidUpdate, append([]string(nil), m.paths...)
		m.Unlock()
		t.Fatalf("%v; invalid create=%v update=%v paths=%v", err, detail, update, paths)
	}
	m.Lock()
	defer m.Unlock()
	if m.id != "" || m.deletes != 1 {
		t.Fatalf("webhook remained: %+v", m)
	}
	for _, path := range m.paths {
		switch path {
		case "POST /api/v3/get-management-token", "GET /api/v3/list-webhooks", "POST /api/v3/create-webhook", "GET /api/v3/get-webhook", "POST /api/v3/update-webhook", "POST /api/v3/delete-webhook":
		default:
			t.Fatalf("unexpected API request: %s", path)
		}
	}
	for _, path := range []string{"GET /api/v3/list-webhooks", "POST /api/v3/create-webhook", "GET /api/v3/get-webhook", "POST /api/v3/update-webhook", "POST /api/v3/delete-webhook"} {
		if !strings.Contains(strings.Join(m.paths, ","), path) {
			t.Fatalf("missing %s: %v", path, m.paths)
		}
	}
}
func TestMockWebhookFailedCreateWithoutStateNeverDeletesByName(t *testing.T) {
	m := &mockWebhook{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/create-webhook" {
			m.serve(httptest.NewRecorder(), r)
			w.WriteHeader(500)
			fmt.Fprint(w, "secret-marker")
			return
		}
		m.serve(w, r)
	}))
	defer server.Close()
	err := runWebhookTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, webhookTestName)
	if err == nil || !strings.Contains(err.Error(), "phase=apply-create") || !strings.Contains(err.Error(), "cleanup=incomplete") {
		t.Fatalf("failed create must keep first phase and unknown cleanup: %v", err)
	}
	if strings.Contains(err.Error(), "secret-marker") {
		t.Fatal("API response leaked")
	}
	m.Lock()
	defer m.Unlock()
	if m.id != "mock-webhook-123" || m.deletes != 0 {
		t.Fatal("failed create deleted a remote webhook without state-pinned ID")
	}
}

func TestMockWebhookFailedReconcileCleansUp(t *testing.T) {
	m := &mockWebhook{failReconcile: true}
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	defer server.Close()
	err := runWebhookTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "secret-marker", "AUTHING_HOST": server.URL}, webhookTestName)
	if err == nil {
		t.Fatal("expected reconciliation failure")
	}
	for _, want := range []string{"phase=apply-reconcile", "code=" + webhookTestName, "cleanup=confirmed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing controlled diagnostic %s: %v", want, err)
		}
	}
	for _, secret := range []string{"key-marker", "secret-marker", "failure-marker", "mock-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("leaked credential or API response")
		}
	}
	m.Lock()
	defer m.Unlock()
	if m.id != "" || m.deletes != 1 {
		t.Fatal("failed to clean owned webhook")
	}
}

func TestMockWebhookFailedReconcileAndCleanupRetainsFirstPhase(t *testing.T) {
	m := &mockWebhook{failReconcile: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/list-webhooks" {
			m.Lock()
			created := m.id != ""
			m.Unlock()
			if created {
				w.WriteHeader(500)
				fmt.Fprint(w, `{"message":"secret-marker"}`)
				return
			}
		}
		m.serve(w, r)
	}))
	defer server.Close()
	err := runWebhookTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, webhookTestName)
	if err == nil {
		t.Fatal("expected failure")
	}
	for _, s := range []string{"phase=apply-reconcile", "code=" + webhookTestName, "cleanup=incomplete"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("missing %s: %v", s, err)
		}
	}
	if strings.Contains(err.Error(), "secret-marker") {
		t.Fatal("leaked API body")
	}
	m.Lock()
	defer m.Unlock()
	if m.deletes != 0 {
		t.Fatal("cleanup must fail closed on list failure")
	}
}

func TestMockWebhookMalformedPreflightNeverWrites(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-management-token" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
			return
		}
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v3/list-webhooks" {
			t.Errorf("unexpected write/request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(405)
			return
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":0,"list":null}}`)
	}))
	defer server.Close()
	err := runWebhookTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, webhookTestName)
	if err == nil || !strings.Contains(err.Error(), "phase=preflight code="+webhookTestName) || requests != 1 {
		t.Errorf("preflight did not fail closed: requests=%d err=%v", requests, err)
	}
}

func TestMockWebhookRefusesExistingName(t *testing.T) {
	m := &mockWebhook{id: "existing-webhook", name: webhookTestName}
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	defer server.Close()
	err := runWebhookTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, webhookTestName)
	if err == nil {
		t.Fatal("accepted an existing webhook")
	}
	m.Lock()
	defer m.Unlock()
	if m.id != "existing-webhook" || m.deletes != 0 || strings.Contains(strings.Join(m.paths, ","), "POST /api/v3/create-webhook") {
		t.Fatal("preflight touched existing webhook")
	}
}

func TestMockWebhookCleanupRefusesForeignName(t *testing.T) {
	m := &mockWebhook{id: "mock-webhook-123", name: "foreign"}
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	defer server.Close()
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if cleanupWebhook(client, m.id, webhookTestName) == nil {
		t.Fatal("accepted foreign name")
	}
	m.Lock()
	defer m.Unlock()
	if m.deletes != 0 {
		t.Fatal("deleted foreign webhook")
	}
}
func TestMockWebhookCleanupRefusesForeignID(t *testing.T) {
	m := &mockWebhook{id: "mock-webhook-123", name: webhookTestName}
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	defer server.Close()
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanupWebhook(client, "foreign-id", webhookTestName); err != nil {
		t.Fatal("absent foreign ID should not be deleted")
	}
	m.Lock()
	defer m.Unlock()
	if m.deletes != 0 {
		t.Fatal("deleted foreign webhook")
	}
}
func TestMockWebhookCleanupRefusesEnabled(t *testing.T) {
	// An unexpected enabled webhook must never be deleted by the fallback.
	m := &mockWebhook{id: "mock-webhook-123", name: webhookTestName}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-webhook" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"webhookId":"mock-webhook-123","name":"`+webhookTestName+`","url":"https://example.invalid/","enabled":true}}`)
			return
		}
		m.serve(w, r)
	}))
	defer server.Close()
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if cleanupWebhook(client, m.id, webhookTestName) == nil {
		t.Fatal("accepted enabled webhook")
	}
	m.Lock()
	defer m.Unlock()
	if m.deletes != 0 {
		t.Fatal("deleted enabled webhook")
	}
}
