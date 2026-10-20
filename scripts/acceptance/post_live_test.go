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

const postTestCode = "hermesacc-1234567890abcdef"

type mockPost struct {
	sync.Mutex
	code, name, description   string
	paths                     []string
	creates, updates, deletes int
	failDrift, foreign        bool
}

func (m *mockPost) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
	case "/api/v3/get-post":
		if r.Method != http.MethodGet || m.code == "" || r.URL.Query().Get("code") != m.code {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		m.respond(w)
	case "/api/v3/create-post", "/api/v3/update-post", "/api/v3/remove-post":
		var v struct {
			Code        string `json:"code"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&v) != nil || v.Code != postTestCode {
			http.Error(w, "invalid request", 500)
			return
		}
		switch r.URL.Path {
		case "/api/v3/create-post":
			if m.code != "" || v.Name != postTestCode || v.Description != "hermesacc ownership "+postTestCode {
				http.Error(w, "invalid create", 500)
				return
			}
			m.code, m.name, m.description = v.Code, v.Name, v.Description
			m.creates++
			m.respond(w)
		case "/api/v3/update-post":
			if m.failDrift && v.Name == postTestCode+"-updated-drift" {
				http.Error(w, "secret-marker", 500)
				return
			}
			if m.code != v.Code || v.Description != m.description || v.Name != postTestCode+"-updated" && v.Name != postTestCode+"-updated-drift" {
				http.Error(w, "invalid update", 500)
				return
			}
			m.name = v.Name
			m.updates++
			m.respond(w)
		case "/api/v3/remove-post":
			if m.code != v.Code || m.foreign || v.Name != "" || v.Description != "" {
				http.Error(w, "unsafe delete", 500)
				return
			}
			m.code = ""
			m.deletes++
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		}
	default:
		http.Error(w, "unexpected endpoint", 404)
	}
}
func (m *mockPost) respond(w http.ResponseWriter) {
	name := m.name
	if m.foreign {
		name = "foreign"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]string{"code": m.code, "name": name, "description": m.description}})
}
func postClient(t *testing.T, m *mockPost) (*authingapi.Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	c, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key-marker", AccessKeySecret: "credential-marker", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return c, server
}
func postCredentials(server *httptest.Server) map[string]string {
	return map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}
}
func TestMockPostTerraformTrace(t *testing.T) {
	m := &mockPost{}
	_, server := postClient(t, m)
	defer server.Close()
	if err := runPostTrace(t.TempDir(), postCredentials(server), postTestCode); err != nil {
		t.Fatalf("%v; paths=%v", err, m.paths)
	}
	m.Lock()
	defer m.Unlock()
	if m.code != "" || m.creates != 1 || m.updates < 3 || m.deletes != 1 {
		t.Fatalf("incomplete post lifecycle: creates=%d updates=%d deletes=%d code=%q", m.creates, m.updates, m.deletes, m.code)
	}
	for _, path := range []string{"GET /api/v3/get-post", "POST /api/v3/create-post", "POST /api/v3/update-post", "POST /api/v3/remove-post"} {
		if !strings.Contains(strings.Join(m.paths, ","), path) {
			t.Errorf("missing %s", path)
		}
	}
}
func TestPostCleanupRejectsForeignIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, code, postName, description string
		foreign                           bool
	}{
		{"wrong-name", postTestCode, "foreign", "hermesacc ownership " + postTestCode, false},
		{"wrong-marker", postTestCode, postTestCode, "other", false},
		{"changed-readback", postTestCode, postTestCode, "hermesacc ownership " + postTestCode, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockPost{code: tc.code, name: tc.postName, description: tc.description, foreign: tc.foreign}
			c, server := postClient(t, m)
			defer server.Close()
			if err := cleanupPost(c, postTestCode, postTestCode+"-updated", "hermesacc ownership "+postTestCode); err == nil {
				t.Fatal("foreign identity accepted")
			}
			if m.deletes != 0 {
				t.Fatal("foreign post deleted")
			}
		})
	}
}
func TestPostCleanupRejectsMismatchedGETCode(t *testing.T) {
	m := &mockPost{code: postTestCode, name: postTestCode, description: "hermesacc ownership " + postTestCode}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-post" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"code":"different","name":"`+postTestCode+`","description":"hermesacc ownership `+postTestCode+`"}}`)
			return
		}
		m.serve(w, r)
	}))
	defer server.Close()
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if cleanupPost(client, postTestCode, postTestCode, "hermesacc ownership "+postTestCode) == nil || m.deletes != 0 {
		t.Fatal("mismatched GET identity authorized deletion")
	}
}

func TestPostTraceRefusesExistingAndMalformedCode(t *testing.T) {
	m := &mockPost{code: postTestCode, name: postTestCode, description: "hermesacc ownership " + postTestCode}
	_, server := postClient(t, m)
	defer server.Close()
	if runPostTrace(t.TempDir(), postCredentials(server), postTestCode) == nil {
		t.Fatal("existing post accepted")
	}
	for _, code := range []string{"default", "hermesacc-123", "hermesacc-1234567890abcdef;"} {
		if runPostTrace(t.TempDir(), postCredentials(server), code) == nil {
			t.Fatalf("invalid code accepted: %q", code)
		}
	}
	if m.deletes != 0 || m.creates != 0 {
		t.Fatal("preflight changed existing post")
	}
}
func TestPostFailedMutationCleansOwnedIdentityWithoutLeaking(t *testing.T) {
	m := &mockPost{failDrift: true}
	_, server := postClient(t, m)
	defer server.Close()
	err := runPostTrace(t.TempDir(), postCredentials(server), postTestCode)
	if err == nil {
		t.Fatal("expected failure")
	}
	for _, s := range []string{"key-marker", "credential-marker", "secret-marker", "mock-token"} {
		if strings.Contains(err.Error(), s) {
			t.Fatal("credential or API body leaked")
		}
	}
	if m.code != "" || m.deletes != 1 {
		t.Fatal("owned post not cleaned up")
	}
}
func TestPostTraceRefusesForeignCleanup(t *testing.T) {
	m := &mockPost{failDrift: true, foreign: true}
	_, server := postClient(t, m)
	defer server.Close()
	err := runPostTrace(t.TempDir(), postCredentials(server), postTestCode)
	if err == nil || !strings.Contains(err.Error(), "cleanup-incomplete") || m.deletes != 0 {
		t.Fatalf("foreign cleanup was not refused: error=%v deletes=%d", err, m.deletes)
	}
}

func TestDestructiveLivePostTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	code, err := newGroupCode()
	if err != nil {
		t.Fatal("post code generation failed")
	}
	if err := runPostTrace(t.TempDir(), env, code); err != nil {
		t.Fatal(err)
	}
}
