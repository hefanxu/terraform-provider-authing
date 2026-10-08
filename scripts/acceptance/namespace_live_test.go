package acceptance

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"terraform-provider-authing/internal/authingapi"
)

const namespaceTestCode = "hermesacc-1234567890abcdef"

// The mock exercises Terraform's real provider protocol, not just DTO calls.
type mockNamespace struct {
	sync.Mutex
	code, name, description                                                  string
	paths                                                                    []string
	deletes                                                                  int
	failDrift, failDelete, foreign, children, wrongCode, incompleteInventory bool
}

func (n *mockNamespace) serve(w http.ResponseWriter, r *http.Request) {
	n.Lock()
	defer n.Unlock()
	n.paths = append(n.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	code := r.URL.Query().Get("code")
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
	case "/api/v3/get-permission-namespace":
		if r.Method != http.MethodGet || code != n.code || n.code == "" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		n.respond(w)
	case "/api/v3/create-permission-namespace":
		var v struct {
			Code        string `json:"code"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&v) != nil || n.code != "" || v.Code != namespaceTestCode {
			http.Error(w, "invalid create", 500)
			return
		}
		n.code, n.name, n.description = v.Code, v.Name, v.Description
		n.respond(w)
	case "/api/v3/update-permission-namespace":
		if n.failDrift {
			http.Error(w, "secret-marker", 500)
			return
		}
		var v struct {
			Code        string `json:"code"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&v) != nil || v.Code != n.code {
			http.Error(w, "invalid update", 500)
			return
		}
		n.name, n.description = v.Name, v.Description
		n.respond(w)
	case "/api/v3/list-permission-namespace-roles":
		if code != n.code || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "1" {
			http.Error(w, "wrong roles scope", 500)
			return
		}
		n.list(w, false)
	case "/api/v3/list-resources":
		if r.URL.Query().Get("namespace") != n.code || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "1" {
			http.Error(w, "wrong resources scope", 500)
			return
		}
		n.list(w, true)
	case "/api/v3/list-data-resources":
		if r.URL.Query().Get("namespaceCodes") != `["`+n.code+`"]` || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "1" {
			http.Error(w, "wrong data resources scope", 500)
			return
		}
		n.list(w, false)
	case "/api/v3/delete-permission-namespace":
		var v struct {
			Code string `json:"code"`
		}
		if n.failDelete || r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&v) != nil || v.Code != n.code || n.foreign || n.children {
			http.Error(w, "unowned deletion", 500)
			return
		}
		n.deletes++
		n.code = ""
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	default:
		http.Error(w, "unexpected", 404)
	}
}
func (n *mockNamespace) respond(w http.ResponseWriter) {
	name := n.name
	if n.foreign {
		name = "foreign namespace"
	}
	code := n.code
	if n.wrongCode {
		code = "default"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]string{"code": code, "name": name, "description": n.description}})
}
func (n *mockNamespace) list(w http.ResponseWriter, legacy bool) {
	if n.incompleteInventory {
		fmt.Fprint(w, `{"statusCode":200,"data":{}}`)
		return
	}
	count := 0
	list := []any{}
	if n.children {
		count = 1
		list = append(list, map[string]string{"code": "test-child"})
	}
	if legacy {
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"statusCode": 200, "totalCount": count, "list": list}})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": count, "list": list}})
}
func namespaceClient(t *testing.T, n *mockNamespace) (*authingapi.Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(n.serve))
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key-marker", AccessKeySecret: "credential-marker", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}
func TestMockNamespaceTerraformTrace(t *testing.T) {
	n := &mockNamespace{}
	_, server := namespaceClient(t, n)
	defer server.Close()
	if err := runNamespaceTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, namespaceTestCode); err != nil {
		t.Fatalf("%v; mock paths=%v; code=%q name=%q description=%q", err, n.paths, n.code, n.name, n.description)
	}
	n.Lock()
	defer n.Unlock()
	if n.code != "" || n.deletes != 1 {
		t.Fatalf("namespace remains or deletion count wrong: %q %d", n.code, n.deletes)
	}
	counts := map[string]int{}
	for _, p := range n.paths {
		counts[p]++
	}
	for _, p := range []string{"POST /api/v3/create-permission-namespace", "POST /api/v3/update-permission-namespace", "GET /api/v3/get-permission-namespace", "POST /api/v3/delete-permission-namespace"} {
		if counts[p] == 0 {
			t.Errorf("missing %s: %v", p, counts)
		}
	}
	if counts["POST /api/v3/update-permission-namespace"] < 2 {
		t.Error("drift was not reconciled")
	}
}
func TestNamespaceCleanupRejectsUnownedAndChildren(t *testing.T) {
	for _, tc := range []struct {
		name                                              string
		foreign, children, wrongCode, incompleteInventory bool
		missingMarker                                     bool
	}{
		{"foreign", true, false, false, false, false},
		{"children", false, true, false, false, false},
		{"wrong-get-code", false, false, true, false, false},
		{"incomplete-inventory", false, false, false, true, false},
		{"missing-marker", false, false, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := "hermesacc ownership " + namespaceTestCode
			if tc.missingMarker {
				marker = ""
			}
			n := &mockNamespace{code: namespaceTestCode, name: namespaceTestCode, description: marker, foreign: tc.foreign, children: tc.children, wrongCode: tc.wrongCode, incompleteInventory: tc.incompleteInventory}
			client, server := namespaceClient(t, n)
			defer server.Close()
			if err := cleanupNamespace(client, namespaceTestCode, namespaceTestCode, "hermesacc ownership "+namespaceTestCode); err == nil {
				t.Fatal("unsafe deletion accepted")
			}
			if n.deletes != 0 || n.code == "" {
				t.Fatal("deleted namespace without ownership and empty child inventory")
			}
		})
	}
}
func TestNamespaceTraceRefusesExistingAndInvalidCode(t *testing.T) {
	n := &mockNamespace{code: namespaceTestCode, name: namespaceTestCode, description: "hermesacc ownership " + namespaceTestCode}
	_, server := namespaceClient(t, n)
	defer server.Close()
	env := map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}
	if runNamespaceTrace(t.TempDir(), env, namespaceTestCode) == nil {
		t.Fatal("existing namespace accepted")
	}
	for _, code := range []string{"default", "hermesacc-123", "hermesacc-1234567890abcdef;"} {
		if runNamespaceTrace(t.TempDir(), env, code) == nil {
			t.Fatalf("unsafe code accepted: %q", code)
		}
	}
	if n.deletes != 0 {
		t.Fatal("existing namespace deleted")
	}
}
func TestNamespaceTraceFailedMutationCleansUpWithoutLeaking(t *testing.T) {
	n := &mockNamespace{failDrift: true}
	_, server := namespaceClient(t, n)
	defer server.Close()
	err := runNamespaceTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, namespaceTestCode)
	if err == nil {
		t.Fatal("failed mutation accepted")
	}
	for _, s := range []string{"key-marker", "credential-marker", "secret-marker", "mock-token"} {
		if strings.Contains(err.Error(), s) {
			t.Fatal("diagnostic leaked secret")
		}
	}
	if n.deletes != 1 || n.code != "" {
		t.Fatal("failure cleanup did not remove owned namespace")
	}
}

func TestNamespaceTracePreservesOriginalPhaseWhenCleanupFails(t *testing.T) {
	n := &mockNamespace{failDrift: true, failDelete: true}
	_, server := namespaceClient(t, n)
	defer server.Close()
	err := runNamespaceTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, namespaceTestCode)
	if err == nil || !strings.Contains(err.Error(), "phase=remote-drift") || !strings.Contains(err.Error(), "cleanup=incomplete") || !strings.Contains(err.Error(), namespaceTestCode) {
		t.Fatalf("missing original phase and cleanup status: %v", err)
	}
	for _, secret := range []string{"key-marker", "credential-marker", "mock-token", "unowned deletion"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("diagnostic leaked response or credential")
		}
	}
	if n.deletes != 0 || n.code != namespaceTestCode {
		t.Fatal("failed cleanup changed namespace")
	}
}

func newNamespaceCode() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "hermesacc-" + hex.EncodeToString(b[:]), nil
}

// Namespace deletion may cascade; require exact GET identity and empty inventories.
func verifyOwnedNamespace(client *authingapi.Client, code, name, marker string) error {
	if !sandboxCode.MatchString(code) || code == "default" || name != code || marker != "hermesacc ownership "+code {
		return errors.New("invalid generated namespace identity")
	}
	got := client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code})
	if got == nil || got.StatusCode != 200 || got.Data.Code != code || got.Data.Description != marker || (got.Data.Name != name && got.Data.Name != name+"-drift") {
		return errors.New("namespace ownership not verified")
	}
	for _, entry := range []struct {
		path   string
		query  map[string]any
		nested bool
	}{
		{"/api/v3/list-permission-namespace-roles", map[string]any{"code": code, "page": 1, "limit": 1}, false},
		{"/api/v3/list-resources", map[string]any{"namespace": code, "page": 1, "limit": 1}, true},
		{"/api/v3/list-data-resources", map[string]any{"namespaceCodes": []string{code}, "page": 1, "limit": 1}, false},
	} {
		body, err := client.SendHttpRequest(entry.path, http.MethodGet, entry.query)
		if err != nil {
			return errors.New("namespace child inventory failed")
		}
		var response struct {
			StatusCode *int            `json:"statusCode"`
			Data       json.RawMessage `json:"data"`
		}
		if json.Unmarshal(body, &response) != nil || response.StatusCode == nil || *response.StatusCode != 200 || len(response.Data) == 0 {
			return errors.New("namespace child inventory invalid")
		}
		var data struct {
			StatusCode *int            `json:"statusCode"`
			TotalCount *int            `json:"totalCount"`
			List       json.RawMessage `json:"list"`
		}
		if json.Unmarshal(response.Data, &data) != nil || (entry.nested && (data.StatusCode == nil || *data.StatusCode != 200)) || data.TotalCount == nil || *data.TotalCount != 0 || string(data.List) != "[]" {
			return errors.New("namespace children present or inventory incomplete")
		}
	}
	return nil
}

func cleanupNamespace(client *authingapi.Client, code, name, marker string) error {
	if !sandboxCode.MatchString(code) || code == "default" {
		return errors.New("invalid namespace code")
	}
	for attempt := 0; attempt < 3; attempt++ {
		got := client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code})
		if got != nil && got.StatusCode == 404 {
			return nil
		}
		if err := verifyOwnedNamespace(client, code, name, marker); err != nil {
			return err
		}
		deleted := client.DeletePermissionNamespace(&dto.DeletePermissionNamespaceDto{Code: code})
		if deleted != nil && deleted.StatusCode != 200 && deleted.StatusCode != 404 {
			return errors.New("namespace delete rejected")
		}
	}
	got := client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code})
	if got != nil && got.StatusCode == 404 {
		return nil
	}
	return errors.New("namespace absence not confirmed")
}

func runNamespaceTrace(root string, credentials map[string]string, code string) (result error) {
	if !sandboxCode.MatchString(code) || code == "default" {
		return errors.New("namespace tracer requires a generated hermesacc code")
	}
	name, marker := code, "hermesacc ownership "+code
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("namespace phase=terraform-cli code=%s (output suppressed)", code)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("namespace phase=go-toolchain code=%s (output suppressed)", code)
		}
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("namespace phase=client code=%s (output suppressed)", code)
	}
	existing := client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code})
	if existing == nil || existing.StatusCode != 404 {
		return fmt.Errorf("namespace phase=preflight code=%s (output suppressed)", code)
	}
	started := false
	defer func() {
		if started {
			if err := cleanupNamespace(client, code, name, marker); err != nil {
				if result != nil {
					// result contains only a controlled phase and validated code.
					result = fmt.Errorf("%s cleanup=incomplete", result)
				} else {
					result = fmt.Errorf("namespace phase=cleanup code=%s cleanup=incomplete (output suppressed)", code)
				}
			}
		}
	}()
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("namespace phase=workspace code=%s (output suppressed)", code)
	}
	binary := filepath.Join(providerDir, "terraform-provider-authing")
	build := exec.Command(goBinary, "build", "-o", binary, ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("namespace phase=provider-build code=%s (output suppressed)", code)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	hcl := fmt.Sprintf(`terraform {
  required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_namespace" "sandbox" {
  code = %q
  name = %q
  description = %q
}
`, source, code, name, marker)
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("namespace phase=config code=%s (output suppressed)", code)
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	started = true
	return (traceCase{name: "namespace", code: code, phases: []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-converged", plan(0)},
		{"verify-created", func() error { return verifyOwnedNamespace(client, code, name, marker) }},
		{"remote-drift", func() error {
			res := client.UpdatePermissionNamespace(&dto.UpdatePermissionNamespaceDto{Code: code, Name: name + "-drift", Description: marker})
			if res == nil || res.StatusCode != 200 || res.Data.Code != code || res.Data.Name != name+"-drift" {
				return errors.New("namespace drift mutation failed")
			}
			got := client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code})
			if got == nil || got.StatusCode != 200 || got.Data.Code != code || got.Data.Name != name+"-drift" || got.Data.Description != marker {
				return errors.New("namespace drift not visible")
			}
			return nil
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-owned-before-destroy", func() error { return verifyOwnedNamespace(client, code, name, marker) }},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			got := client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code})
			if got == nil || got.StatusCode != 404 {
				return errors.New("namespace still present")
			}
			return nil
		}},
	}}).execute()
}

func TestDestructiveLiveNamespaceTrace(t *testing.T) {
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
	if err := runNamespaceTrace(t.TempDir(), env, code); err != nil {
		t.Fatal(err)
	}
}
