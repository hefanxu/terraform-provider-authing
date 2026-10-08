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

func TestDestructiveGuard(t *testing.T) {
	for _, tc := range []struct {
		flag    bool
		confirm string
		allowed bool
	}{
		{false, "DESTRUCTIVE_SANDBOX", false}, {true, "READ_ONLY_SANDBOX", false},
		{true, "DESTRUCTIVE_SANDBOX ", false}, {true, "DESTRUCTIVE_SANDBOX", true},
	} {
		env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": tc.confirm, "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}
		err := destructiveGuard(tc.flag, env)
		if (err == nil) != tc.allowed {
			t.Fatalf("guard(%v,%q) success=%v", tc.flag, tc.confirm, err == nil)
		}
	}
	if destructiveGuard(true, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "DESTRUCTIVE_SANDBOX"}) == nil {
		t.Fatal("missing credentials accepted")
	}
}

type mockGroup struct {
	sync.Mutex
	code, name, description, kind string
	paths                         []string
	failUpdate                    bool
	failDeleteOnce                bool
	deleteAttempts                int
	deletes                       int
}

func (g *mockGroup) serve(w http.ResponseWriter, r *http.Request) {
	g.Lock()
	defer g.Unlock()
	g.paths = append(g.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
	case "/api/v3/get-group":
		if r.URL.Query().Get("code") != g.code || g.code == "" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		g.group(w)
	case "/api/v3/create-group":
		var v struct {
			Code        string `json:"code"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Type        string `json:"type"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil || g.code != "" || !strings.HasPrefix(v.Code, "hermesacc-") {
			http.Error(w, "invalid create", 500)
			return
		}
		g.code, g.name, g.description, g.kind = v.Code, v.Name, v.Description, v.Type
		g.group(w)
	case "/api/v3/update-group":
		if g.failUpdate {
			http.Error(w, "secret-marker", 500)
			return
		}
		var v struct {
			Code        string `json:"code"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil || v.Code != g.code {
			http.Error(w, "invalid update", 500)
			return
		}
		g.name, g.description = v.Name, v.Description
		g.group(w)
	case "/api/v3/delete-groups-batch":
		g.deleteAttempts++
		if g.failDeleteOnce && g.deleteAttempts == 1 {
			http.Error(w, "secret-marker", 500)
			return
		}
		var v struct {
			Codes []string `json:"codeList"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil || len(v.Codes) != 1 || v.Codes[0] != g.code {
			http.Error(w, "unowned deletion", 500)
			return
		}
		g.deletes++
		g.code = ""
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	default:
		http.Error(w, "unexpected", 404)
	}
}
func (g *mockGroup) group(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]string{"code": g.code, "name": g.name, "description": g.description, "type": g.kind}})
}
func TestMockDestructiveGroupTrace(t *testing.T) {
	g := &mockGroup{}
	server := httptest.NewServer(http.HandlerFunc(g.serve))
	defer server.Close()
	code := "hermesacc-1234567890abcdef"
	err := runGroupTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, code)
	if err != nil {
		t.Fatal(err)
	}
	g.Lock()
	defer g.Unlock()
	if g.code != "" || g.deletes != 1 {
		t.Fatalf("group remained or delete count wrong: %q %d", g.code, g.deletes)
	}
	var creates, updates, gets int
	for _, p := range g.paths {
		switch p {
		case "POST /api/v3/create-group":
			creates++
		case "POST /api/v3/update-group":
			updates++
		case "GET /api/v3/get-group":
			gets++
		}
	}
	if creates != 1 || updates < 2 || gets < 4 {
		t.Fatalf("lifecycle incomplete: create=%d update=%d get=%d", creates, updates, gets)
	}
}
func TestMockDestructiveFailureCleanup(t *testing.T) {
	g := &mockGroup{failUpdate: true}
	server := httptest.NewServer(http.HandlerFunc(g.serve))
	defer server.Close()
	code := "hermesacc-1234567890abcdef"
	err := runGroupTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, code)
	if err == nil {
		t.Fatal("expected failed drift mutation")
	}
	for _, s := range []string{"credential-marker", "key-marker", "secret-marker", "mock-token"} {
		if strings.Contains(err.Error(), s) {
			t.Fatal("diagnostic leaked a secret")
		}
	}
	if !strings.Contains(err.Error(), code) {
		t.Fatal("missing cleanup identifier")
	}
	g.Lock()
	defer g.Unlock()
	if g.code != "" || g.deletes != 1 {
		t.Fatalf("cleanup failed: group=%q deletes=%d", g.code, g.deletes)
	}
}
func TestMockDestructiveRefusesExistingGroup(t *testing.T) {
	code := "hermesacc-1234567890abcdef"
	g := &mockGroup{code: code, name: "somebody else's group", kind: "static"}
	server := httptest.NewServer(http.HandlerFunc(g.serve))
	defer server.Close()
	err := runGroupTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, code)
	if err == nil {
		t.Fatal("existing resource accepted")
	}
	g.Lock()
	defer g.Unlock()
	if g.code != code || g.deletes != 0 {
		t.Fatal("deleted an unowned resource")
	}
}
func TestMockDestructiveCleanupRetriesTransientDelete(t *testing.T) {
	code := "hermesacc-1234567890abcdef"
	g := &mockGroup{code: code, name: code, description: "hermesacc ownership " + code, kind: "static", failDeleteOnce: true}
	server := httptest.NewServer(http.HandlerFunc(g.serve))
	defer server.Close()
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "mock", AccessKeySecret: "mock", Host: server.URL})
	if err != nil {
		t.Fatal("client setup failed")
	}
	if err := cleanupGroup(client, code, code, "hermesacc ownership "+code); err != nil {
		t.Fatal("bounded cleanup did not recover")
	}
	g.Lock()
	defer g.Unlock()
	if g.code != "" || g.deleteAttempts != 2 || g.deletes != 1 {
		t.Fatal("cleanup did not retry safely")
	}
}

func TestMockDestructiveCleanupRefusesLostOwnership(t *testing.T) {
	code := "hermesacc-1234567890abcdef"
	g := &mockGroup{code: code, name: "other owner", description: "hermesacc ownership " + code, kind: "static"}
	server := httptest.NewServer(http.HandlerFunc(g.serve))
	defer server.Close()
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "mock", AccessKeySecret: "mock", Host: server.URL})
	if err != nil {
		t.Fatal("client setup failed")
	}
	if cleanupGroup(client, code, code, "hermesacc ownership "+code) == nil {
		t.Fatal("unowned group accepted")
	}
	g.Lock()
	defer g.Unlock()
	if g.deletes != 0 || g.code != code {
		t.Fatal("deleted an unowned group")
	}
}

func TestMockDestructiveOwnershipCheckBeforeDestroy(t *testing.T) {
	code := "hermesacc-1234567890abcdef"
	g := &mockGroup{code: code, name: "other owner", description: "hermesacc ownership " + code, kind: "static"}
	server := httptest.NewServer(http.HandlerFunc(g.serve))
	defer server.Close()
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "mock", AccessKeySecret: "mock", Host: server.URL})
	if err != nil {
		t.Fatal("client setup failed")
	}
	if verifyOwnedGroup(client, code, code, "hermesacc ownership "+code) == nil {
		t.Fatal("ownership check accepted foreign group")
	}
}

func TestDestructiveLiveGroupTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	code, err := newGroupCode()
	if err != nil {
		t.Fatal("random resource code generation failed")
	}
	if err := runGroupTrace(t.TempDir(), env, code); err != nil {
		t.Fatal(err)
	}
}
