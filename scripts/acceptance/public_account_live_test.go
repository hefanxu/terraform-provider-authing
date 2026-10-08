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
)

const mockPublicUsername = "hermesacc-1234567890abcdef"

type mockPublicAccount struct {
	sync.Mutex
	id, username, name                              string
	calls                                           []string
	deletes, deleteAttempts                         int
	failDrift, failCreateAfterWrite, failDeleteOnce bool
}

func (a *mockPublicAccount) serve(w http.ResponseWriter, r *http.Request) {
	a.Lock()
	defer a.Unlock()
	a.calls = append(a.calls, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
	case "/api/v3/create-public-account":
		var body map[string]any
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || a.id != "" || body["username"] != mockPublicUsername || body["name"] != mockPublicUsername || body["password"] != nil {
			http.Error(w, "invalid create", 500)
			return
		}
		a.id, a.username, a.name = "mock-public-id", mockPublicUsername, mockPublicUsername
		if a.failCreateAfterWrite {
			http.Error(w, "secret-marker", 500)
			return
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"userId":"mock-public-id"}}`)
	case "/api/v3/get-public-account":
		if r.Method != "GET" || r.URL.Query().Get("userIdType") != "user_id" || r.URL.Query().Get("userId") != a.id || a.id == "" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		a.respond(w)
	case "/api/v3/update-public-account":
		if a.failDrift {
			http.Error(w, "secret-marker", 500)
			return
		}
		var body map[string]any
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || body["userId"] != a.id || body["name"] == nil || body["password"] != nil {
			http.Error(w, "invalid update", 500)
			return
		}
		a.name = body["name"].(string)
		fmt.Fprint(w, `{"statusCode":200,"data":{"userId":"mock-public-id"}}`)
	case "/api/v3/delete-public-accounts-batch":
		a.deleteAttempts++
		if a.failDeleteOnce && a.deleteAttempts == 1 {
			http.Error(w, "secret-marker", 500)
			return
		}
		var body struct {
			UserIDs []string `json:"userIds"`
		}
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || len(body.UserIDs) != 1 || body.UserIDs[0] != a.id || a.id == "" {
			http.Error(w, "unowned deletion", 500)
			return
		}
		a.id = ""
		a.deletes++
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	default:
		http.Error(w, "unexpected path", 500)
	}
}
func (a *mockPublicAccount) respond(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"userId": a.id, "username": a.username, "name": a.name}})
}
func mockPublicCredentials(url string) map[string]string {
	return map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": url}
}
func TestMockPublicAccountTerraformLifecycle(t *testing.T) {
	a := &mockPublicAccount{}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	if err := runPublicAccountTrace(t.TempDir(), mockPublicCredentials(server.URL), mockPublicUsername); err != nil {
		t.Fatal(err)
	}
	a.Lock()
	defer a.Unlock()
	if a.id != "" || a.deletes != 1 || a.name != mockPublicUsername {
		t.Fatal("lifecycle failed to reconcile and delete exact ID")
	}
	var creates, updates, gets int
	for _, p := range a.calls {
		switch p {
		case "POST /api/v3/create-public-account":
			creates++
		case "POST /api/v3/update-public-account":
			updates++
		case "GET /api/v3/get-public-account":
			gets++
		}
	}
	if creates != 1 || updates != 2 || gets < 5 {
		t.Fatalf("incomplete protocol lifecycle: creates=%d updates=%d gets=%d", creates, updates, gets)
	}
}
func TestMockPublicAccountFailedDriftCleansOwnedID(t *testing.T) {
	a := &mockPublicAccount{failDrift: true}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	err := runPublicAccountTrace(t.TempDir(), mockPublicCredentials(server.URL), mockPublicUsername)
	if err == nil {
		t.Fatal("drift failure accepted")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "mock-token", "secret-marker"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("diagnostic leaked secret")
		}
	}
	a.Lock()
	defer a.Unlock()
	if a.id != "" || a.deletes != 1 {
		t.Fatal("failed cleanup of owned ID")
	}
}
func TestMockPublicAccountUnknownCreateIDIsNeverDeleted(t *testing.T) {
	a := &mockPublicAccount{failCreateAfterWrite: true}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	err := runPublicAccountTrace(t.TempDir(), mockPublicCredentials(server.URL), mockPublicUsername)
	if err == nil || !strings.Contains(err.Error(), "cleanup-unknown-id") {
		t.Fatal("failed create must disclose unknown cleanup identity")
	}
	a.Lock()
	defer a.Unlock()
	if a.id == "" || a.deletes != 0 {
		t.Fatal("deleted ID not proved self-created")
	}
}
func TestMockPublicAccountRefusesForeignIdentity(t *testing.T) {
	a := &mockPublicAccount{id: "mock-public-id", username: "other", name: mockPublicUsername}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	client := mockUserClient(t, server.URL)
	if cleanupPublicAccount(client, "mock-public-id", mockPublicUsername) == nil {
		t.Fatal("accepted foreign identity")
	}
	a.Lock()
	defer a.Unlock()
	if a.deletes != 0 {
		t.Fatal("deleted foreign identity")
	}
}
func TestMockPublicAccountCleanupRetriesTransientDelete(t *testing.T) {
	a := &mockPublicAccount{id: "mock-public-id", username: mockPublicUsername, name: mockPublicUsername, failDeleteOnce: true}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	if err := cleanupPublicAccount(mockUserClient(t, server.URL), "mock-public-id", mockPublicUsername); err != nil {
		t.Fatal(err)
	}
	a.Lock()
	defer a.Unlock()
	if a.id != "" || a.deletes != 1 || a.deleteAttempts != 2 {
		t.Fatal("bounded cleanup did not confirm absence")
	}
}
func TestDestructiveLivePublicAccountTrace(t *testing.T) {
	if !*destructiveLive || os.Getenv("AUTHING_ACCEPTANCE_CONFIRM") != "DESTRUCTIVE_SANDBOX" {
		t.Skip("requires -authing-destructive-sandbox and exact DESTRUCTIVE_SANDBOX confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	username, err := newGroupCode()
	if err != nil {
		t.Fatal("random username generation failed")
	}
	if err := runPublicAccountTrace(t.TempDir(), env, username); err != nil {
		t.Fatal(err)
	}
}
