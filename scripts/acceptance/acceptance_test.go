package acceptance

import (
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var live = flag.Bool("authing-read-only-sandbox", false, "opt in to a read-only sandbox plan")

func TestGuard(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		enabled              bool
		confirm, key, secret string
		good                 bool
	}{
		{"no flag", false, "READ_ONLY_SANDBOX", "key", "secret", false},
		{"no confirmation", true, "", "key", "secret", false},
		{"wrong confirmation", true, "READ_ONLY_SANDBOX ", "key", "secret", false},
		{"missing key", true, "READ_ONLY_SANDBOX", "", "secret", false},
		{"missing secret", true, "READ_ONLY_SANDBOX", "key", "", false},
		{"all guards", true, "READ_ONLY_SANDBOX", "key", "secret", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": tc.confirm, "AUTHING_ACCESS_KEY_ID": tc.key, "AUTHING_ACCESS_KEY_SECRET": tc.secret}
			err := guard(tc.enabled, env)
			if (err == nil) != tc.good {
				t.Fatalf("guard success = %v, want %v", err == nil, tc.good)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("credential exposed by guard")
			}
		})
	}
}

func TestMockReadOnlyPlan(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/v3/get-management-token":
			if r.Method != http.MethodPost {
				t.Errorf("unexpected token method: %s", r.Method)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
		case "/api/v3/get-security-settings":
			if r.Method != http.MethodGet {
				t.Errorf("unexpected data method: %s", r.Method)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"registerDisabled":false,"loginRequireEmailVerified":true}}`)
		default:
			t.Errorf("unexpected API path: %s", r.URL.Path)
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	env := map[string]string{"AUTHING_ACCESS_KEY_ID": "mock-key", "AUTHING_ACCESS_KEY_SECRET": "mock-secret", "AUTHING_HOST": server.URL}
	if err := runPlan(root, env); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "POST /api/v3/get-management-token" || paths[1] != "GET /api/v3/get-security-settings" {
		t.Fatalf("unexpected calls: %v", paths)
	}
	if _, err := os.Stat(filepath.Join(root, "example", "terraform.tfstate")); !os.IsNotExist(err) {
		t.Fatalf("plan wrote state: %v", err)
	}
}

func TestMockFailureSuppressesCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-management-token" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
			return
		}
		http.Error(w, "secret-marker", http.StatusInternalServerError)
	}))
	defer server.Close()
	err := runPlan(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL})
	if err == nil {
		t.Fatal("expected failed plan")
	}
	for _, marker := range []string{"key-marker", "credential-marker", "secret-marker"} {
		if strings.Contains(err.Error(), marker) {
			t.Fatal("plan diagnostic leaked credential or API response")
		}
	}
}

func TestReadOnlyPlan(t *testing.T) {
	if !*live {
		t.Skip("requires -authing-read-only-sandbox and explicit confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := guard(*live, env); err != nil {
		t.Fatal(err)
	}
	if err := runPlan(t.TempDir(), env); err != nil {
		t.Fatal(err)
	}
}
