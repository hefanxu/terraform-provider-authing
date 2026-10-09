package acceptance

import (
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

const organizationTestCode = "hermesacc-1234567890abcdef"

// The mock runs the real Terraform CLI/plugin protocol, with no live credentials.
type mockOrganization struct {
	sync.Mutex
	code, name, description                           string
	paths                                             []string
	deletes                                           int
	children, members, incomplete, foreign, failDrift bool
}

func (m *mockOrganization) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	code := r.URL.Query().Get("organizationCode")
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
	case "/api/v3/get-organization":
		if r.Method != http.MethodGet || m.code == "" || code != m.code {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		m.respond(w)
	case "/api/v3/create-organization":
		var v struct {
			Code        string `json:"organizationCode"`
			Name        string `json:"organizationName"`
			Description string `json:"description"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&v) != nil || m.code != "" || v.Code != organizationTestCode || v.Name != v.Code || v.Description != "hermesacc ownership "+v.Code {
			http.Error(w, "invalid create", 500)
			return
		}
		m.code, m.name, m.description = v.Code, v.Name, v.Description
		m.respond(w)
	case "/api/v3/update-organization":
		if m.failDrift {
			http.Error(w, "secret-marker", 500)
			return
		}
		var v struct {
			Code        string `json:"organizationCode"`
			Name        string `json:"organizationName"`
			Description string `json:"description"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&v) != nil || v.Code != m.code || v.Description != m.description {
			http.Error(w, "invalid update", 500)
			return
		}
		m.name = v.Name
		m.respond(w)
	case "/api/v3/list-children-departments", "/api/v3/get-all-departments":
		if r.Method != http.MethodGet || code != m.code || r.URL.Query().Get("departmentId") != "root" {
			http.Error(w, "wrong department scope", 500)
			return
		}
		m.inventory(w, m.children)
	case "/api/v3/list-department-members":
		if r.Method != http.MethodGet || code != m.code || r.URL.Query().Get("departmentId") != "root" || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "1" || r.URL.Query().Get("includeChildrenDepartments") != "true" {
			http.Error(w, "wrong member scope", 500)
			return
		}
		m.inventory(w, m.members)
	case "/api/v3/delete-organization":
		var v struct {
			Code string `json:"organizationCode"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&v) != nil || v.Code != m.code || m.children || m.members || m.foreign || m.incomplete {
			http.Error(w, "unsafe delete", 500)
			return
		}
		m.deletes++
		m.code = ""
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	default:
		http.Error(w, "unexpected", 404)
	}
}
func (m *mockOrganization) respond(w http.ResponseWriter) {
	name := m.name
	if m.foreign {
		name = "foreign"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"organizationCode": m.code, "organizationName": name, "description": m.description, "hasChildren": m.children, "membersCount": 0}})
}
func (m *mockOrganization) inventory(w http.ResponseWriter, occupied bool) {
	if m.incomplete {
		fmt.Fprint(w, `{"statusCode":200,"data":{}}`)
		return
	}
	count := 0
	list := []any{}
	if occupied {
		count = 1
		list = append(list, map[string]string{"departmentId": "foreign"})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": count, "list": list}})
}
func organizationClient(t *testing.T, m *mockOrganization) (*authingapi.Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key-marker", AccessKeySecret: "credential-marker", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}

// Require positive proof of ownership AND complete empty child/member inventories.
// A missing count, a missing list, a business error, or a nonempty collection
// cannot authorize deletion of the entire organization tree.
func verifyOwnedOrganization(client *authingapi.Client, code, name, marker string) error {
	if !sandboxCode.MatchString(code) || (name != code && name != code+"-updated") || marker != "hermesacc ownership "+code {
		return errors.New("invalid generated organization identity")
	}
	body, err := client.SendHttpRequest("/api/v3/get-organization", http.MethodGet, map[string]any{"organizationCode": code})
	var got struct {
		StatusCode *int `json:"statusCode"`
		Data       *struct {
			Code         string `json:"organizationCode"`
			Name         string `json:"organizationName"`
			Description  string `json:"description"`
			HasChildren  *bool  `json:"hasChildren"`
			MembersCount *int   `json:"membersCount"`
		} `json:"data"`
	}
	if err != nil || json.Unmarshal(body, &got) != nil || got.StatusCode == nil || *got.StatusCode != 200 || got.Data == nil || got.Data.Code != code || got.Data.Description != marker || got.Data.Name != name && got.Data.Name != name+"-drift" && !(name == code+"-updated" && got.Data.Name == code) || got.Data.HasChildren == nil || *got.Data.HasChildren || got.Data.MembersCount == nil || *got.Data.MembersCount != 0 {
		return errors.New("organization ownership or root occupancy not verified")
	}
	for _, entry := range []struct {
		path  string
		query map[string]any
	}{
		{"/api/v3/list-children-departments", map[string]any{"organizationCode": code, "departmentId": "root"}},
		{"/api/v3/get-all-departments", map[string]any{"organizationCode": code, "departmentId": "root"}},
		{"/api/v3/list-department-members", map[string]any{"organizationCode": code, "departmentId": "root", "page": 1, "limit": 1, "includeChildrenDepartments": true}},
	} {
		body, err := client.SendHttpRequest(entry.path, http.MethodGet, entry.query)
		var out struct {
			StatusCode *int `json:"statusCode"`
			Data       *struct {
				TotalCount *int            `json:"totalCount"`
				List       json.RawMessage `json:"list"`
			} `json:"data"`
		}
		if err != nil || json.Unmarshal(body, &out) != nil || out.StatusCode == nil || *out.StatusCode != 200 || out.Data == nil || out.Data.TotalCount == nil || *out.Data.TotalCount != 0 || string(out.Data.List) != "[]" {
			return errors.New("organization descendants/members present or inventory incomplete")
		}
	}
	return nil
}
func cleanupOrganization(client *authingapi.Client, code, name, marker string) error {
	if !sandboxCode.MatchString(code) {
		return errors.New("invalid organization code")
	}
	got := client.GetOrganization(&dto.GetOrganizationDto{OrganizationCode: code})
	if got != nil && got.StatusCode == 404 {
		return nil
	}
	if err := verifyOwnedOrganization(client, code, name, marker); err != nil {
		return err
	}
	deleted := client.DeleteOrganization(&dto.DeleteOrganizationReqDto{OrganizationCode: code})
	if deleted == nil || deleted.StatusCode != 200 || !deleted.Data.Success {
		return errors.New("organization deletion not confirmed")
	}
	got = client.GetOrganization(&dto.GetOrganizationDto{OrganizationCode: code})
	if got == nil || got.StatusCode != 404 {
		return errors.New("organization absence not confirmed")
	}
	return nil
}
func runOrganizationTrace(root string, credentials map[string]string, code string) (result error) {
	if !sandboxCode.MatchString(code) {
		return errors.New("organization tracer requires a generated hermesacc code")
	}
	name, marker := code, "hermesacc ownership "+code
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("organization phase=terraform-cli code=%s (output suppressed)", code)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("organization phase=go-toolchain code=%s (output suppressed)", code)
		}
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("organization phase=client code=%s (output suppressed)", code)
	}
	existing := client.GetOrganization(&dto.GetOrganizationDto{OrganizationCode: code})
	if existing == nil || existing.StatusCode != 404 {
		return fmt.Errorf("organization phase=preflight code=%s (output suppressed)", code)
	}
	started := false
	defer func() {
		if started {
			if err := cleanupOrganization(client, code, name, marker); err != nil {
				result = fmt.Errorf("organization phase=cleanup-incomplete code=%s (output suppressed; owner must manually inspect)", code)
			}
		}
	}()
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("organization phase=workspace code=%s (output suppressed)", code)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("organization phase=provider-build code=%s (output suppressed)", code)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	hcl := fmt.Sprintf(`terraform {
 required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_organization" "sandbox" {
 organization_code = %q
 organization_name = %q
 description = %q
}
`, source, code, name, marker)
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("organization phase=config code=%s (output suppressed)", code)
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	started = true
	return (traceCase{name: "organization", code: code, phases: []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-converged", plan(0)},
		{"verify-created", func() error { return verifyOwnedOrganization(client, code, name, marker) }},
		{"configure-name-update", func() error {
			updated := strings.Replace(hcl, fmt.Sprintf("organization_name = %q", name), fmt.Sprintf("organization_name = %q", name+"-updated"), 1)
			if updated == hcl {
				return errors.New("organization configuration did not change")
			}
			if err := os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(updated), 0600); err != nil {
				return err
			}
			name += "-updated"
			return nil
		}},
		{"plan-name-update", plan(2)},
		{"apply-name-update", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-name-update", func() error {
			got := client.GetOrganization(&dto.GetOrganizationDto{OrganizationCode: code})
			if got == nil || got.StatusCode != 200 || got.Data.OrganizationCode != code || got.Data.OrganizationName != name || got.Data.Description != marker {
				return errors.New("Terraform name update not visible")
			}
			return verifyOwnedOrganization(client, code, name, marker)
		}},
		{"plan-updated", plan(0)},
		{"remote-drift", func() error {
			res := client.UpdateOrganization(&dto.UpdateOrganizationReqDto{OrganizationCode: code, OrganizationName: name + "-drift", Description: marker})
			if res == nil || res.StatusCode != 200 || res.Data.OrganizationCode != code || res.Data.OrganizationName != name+"-drift" {
				return errors.New("organization drift mutation failed")
			}
			got := client.GetOrganization(&dto.GetOrganizationDto{OrganizationCode: code})
			if got == nil || got.StatusCode != 200 || got.Data.OrganizationCode != code || got.Data.OrganizationName != name+"-drift" || got.Data.Description != marker {
				return errors.New("organization drift not visible")
			}
			return verifyOwnedOrganization(client, code, name, marker)
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-safe-before-destroy", func() error {
			got := client.GetOrganization(&dto.GetOrganizationDto{OrganizationCode: code})
			if got == nil || got.StatusCode != 200 || got.Data.OrganizationCode != code || got.Data.OrganizationName != name || got.Data.Description != marker {
				return errors.New("organization name not reconciled")
			}
			return verifyOwnedOrganization(client, code, name, marker)
		}},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			got := client.GetOrganization(&dto.GetOrganizationDto{OrganizationCode: code})
			if got == nil || got.StatusCode != 404 {
				return errors.New("organization still present")
			}
			return nil
		}},
	}}).execute()
}
func TestMockOrganizationTerraformTrace(t *testing.T) {
	m := &mockOrganization{}
	_, server := organizationClient(t, m)
	defer server.Close()
	if err := runOrganizationTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, organizationTestCode); err != nil {
		t.Fatalf("%v; paths=%v", err, m.paths)
	}
	m.Lock()
	defer m.Unlock()
	if m.code != "" || m.deletes != 1 {
		t.Fatal("organization remains or deletion count incorrect")
	}
	counts := map[string]int{}
	for _, p := range m.paths {
		counts[p]++
	}
	for _, p := range []string{"POST /api/v3/create-organization", "POST /api/v3/update-organization", "GET /api/v3/get-organization", "POST /api/v3/delete-organization"} {
		if counts[p] == 0 {
			t.Errorf("missing %s", p)
		}
	}
	if counts["POST /api/v3/create-organization"] != 1 || counts["POST /api/v3/delete-organization"] != 1 {
		t.Error("name update replaced the organization instead of updating in place")
	}
	if counts["POST /api/v3/update-organization"] < 3 {
		t.Error("Terraform name update or drift reconciliation was not exercised")
	}
}
func TestOrganizationCleanupRejectsUnsafeInventory(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		children, members, incomplete, foreign bool
	}{
		{name: "children", children: true}, {name: "members", members: true}, {name: "incomplete", incomplete: true}, {name: "foreign", foreign: true},
		{name: "missing-marker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockOrganization{code: organizationTestCode, name: organizationTestCode, description: "hermesacc ownership " + organizationTestCode, children: tc.children, members: tc.members, incomplete: tc.incomplete, foreign: tc.foreign}
			if tc.name == "missing-marker" {
				m.description = ""
			}
			client, server := organizationClient(t, m)
			defer server.Close()
			if cleanupOrganization(client, organizationTestCode, organizationTestCode, "hermesacc ownership "+organizationTestCode) == nil || m.deletes != 0 {
				t.Fatal("unsafe deletion accepted")
			}
		})
	}
}
func TestOrganizationTraceRefusesExistingAndInvalidCode(t *testing.T) {
	m := &mockOrganization{code: organizationTestCode, name: organizationTestCode, description: "hermesacc ownership " + organizationTestCode}
	_, server := organizationClient(t, m)
	defer server.Close()
	env := map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}
	if runOrganizationTrace(t.TempDir(), env, organizationTestCode) == nil {
		t.Fatal("existing organization accepted")
	}
	for _, code := range []string{"default", "hermesacc-123", "hermesacc-1234567890abcdef;"} {
		if runOrganizationTrace(t.TempDir(), env, code) == nil {
			t.Fatalf("invalid code accepted: %q", code)
		}
	}
	if m.deletes != 0 {
		t.Fatal("existing organization deleted")
	}
}
func TestOrganizationTraceFailedMutationCleansUpWithoutLeaking(t *testing.T) {
	m := &mockOrganization{failDrift: true}
	_, server := organizationClient(t, m)
	defer server.Close()
	err := runOrganizationTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, organizationTestCode)
	if err == nil {
		t.Fatal("failed mutation accepted")
	}
	for _, s := range []string{"key-marker", "credential-marker", "secret-marker", "mock-token"} {
		if strings.Contains(err.Error(), s) {
			t.Fatal("diagnostic leaked credential")
		}
	}
	if m.deletes != 1 || m.code != "" {
		t.Fatal("failure cleanup did not remove owned organization")
	}
}
func TestDestructiveLiveOrganizationTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	code, err := newNamespaceCode()
	if err != nil {
		t.Fatal("organization code generation failed")
	}
	if err := runOrganizationTrace(t.TempDir(), env, code); err != nil {
		t.Fatal(err)
	}
}
