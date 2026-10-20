package acceptance

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"terraform-provider-authing/internal/authingapi"
)

// tenantEnvelope discards response text: sandbox failures must not leak API bodies.
func tenantEnvelope(c *authingapi.Client, path, method string, arg any) (int, json.RawMessage, error) {
	raw, err := c.SendHttpRequest(path, method, arg)
	if err != nil {
		return 0, nil, errors.New("tenant API request failed")
	}
	var e struct {
		Status *int            `json:"statusCode"`
		Data   json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Status == nil {
		return 0, nil, errors.New("invalid tenant API envelope")
	}
	return *e.Status, e.Data, nil
}

type tenantIdentity struct {
	ID     string    `json:"tenantId"`
	Name   string    `json:"name"`
	AppIDs *[]string `json:"appIds"`
}

func tenantGET(c *authingapi.Client, id string) (int, tenantIdentity, error) {
	status, raw, err := tenantEnvelope(c, "/api/v3/get-tenant", http.MethodGet, map[string]string{"tenantId": id})
	var v tenantIdentity
	if err != nil || status == 404 {
		return status, v, err
	}
	if status != 200 || json.Unmarshal(raw, &v) != nil || v.ID != id || v.Name == "" || v.AppIDs == nil {
		return status, v, errors.New("tenant GET identity incomplete")
	}
	return status, v, nil
}

// Check an unfiltered, complete tenant inventory before creating a name that
// might already exist; keywords filtering cannot establish uniqueness.
func tenantNameAbsent(c *authingapi.Client, name string) error {
	const limit = 100
	total := -1
	seen := map[string]bool{}
	for page := 1; ; page++ {
		status, raw, err := tenantEnvelope(c, "/api/v3/list-tenants", http.MethodGet, map[string]string{"page": fmt.Sprint(page), "limit": fmt.Sprint(limit)})
		var data struct {
			Total *int `json:"totalCount"`
			List  *[]struct {
				ID   string `json:"tenantId"`
				Name string `json:"name"`
			} `json:"list"`
		}
		if err != nil || status != 200 || json.Unmarshal(raw, &data) != nil || data.Total == nil || data.List == nil || *data.Total < 0 {
			return errors.New("tenant preflight inventory incomplete")
		}
		if total < 0 {
			total = *data.Total
		}
		if total != *data.Total || len(*data.List) != min(limit, total-len(seen)) {
			return errors.New("tenant preflight pagination incomplete")
		}
		for _, v := range *data.List {
			if v.ID == "" || v.Name == "" || seen[v.ID] || v.Name == name {
				return errors.New("tenant preflight identity not unique")
			}
			seen[v.ID] = true
		}
		if len(seen) == total {
			return nil
		}
	}
}

// A zero total without an explicit empty list is not proof of an empty scope.
func emptyTenantCollection(c *authingapi.Client, path, method string, query any) error {
	stage := "organizations"
	if path == "/api/v3/list-tenant-users" {
		stage = "users"
	}
	if path == "/api/v3/list-tenant-admin" {
		stage = "admins"
	}
	s, raw := tenantProbeRequest(c, stage, path, method, query)
	s.Pages = 1
	if s.Category != "complete" {
		return tenantDiagnosticError{s}
	}
	s, list, total := tenantProbePage(raw, s)
	if s.Category != "complete" {
		return tenantDiagnosticError{s}
	}
	s.Count = len(list)
	if total == 0 && len(list) == 0 {
		return nil
	}
	s.Category = "nonempty"
	if total != len(list) && len(list) == 0 || total == 0 {
		s.Category = "incomplete-inventory"
	}
	return tenantDiagnosticError{s}
}
func verifyEmptyOwnedTenant(c *authingapi.Client, id, expectedName string) error {
	if !sandboxCode.MatchString(expectedName) && !strings.HasSuffix(expectedName, "-updated") {
		return errors.New("tenant name not generated")
	}
	base := strings.TrimSuffix(expectedName, "-updated")
	if !sandboxCode.MatchString(base) || id == "" {
		return errors.New("tenant identity not generated")
	}
	get, apps := tenantProbeIdentity(c, id, base, base+"-updated", base+"-updated-drift")
	if get.Category != "candidate" {
		return tenantDiagnosticError{get}
	}
	if apps.Category != "empty" {
		return tenantDiagnosticError{apps}
	}
	// Separate scoped inventories: membership includes ordinary and admin users;
	// checking admin independently protects against an inconsistent membership view.
	for _, entry := range []struct {
		path, method string
		query        any
	}{
		{"/api/v3/list-tenant-users", http.MethodPost, map[string]any{"tenantId": id, "options": map[string]any{"pagination": map[string]int{"page": 1, "limit": 1}}}},
		{"/api/v3/list-tenant-admin", http.MethodPost, map[string]string{"tenantId": id, "page": "1", "limit": "1"}},
		{"/api/v3/list-organizations", http.MethodGet, map[string]any{"tenantId": id, "page": 1, "limit": 1}},
	} {
		if err := emptyTenantCollection(c, entry.path, entry.method, entry.query); err != nil {
			return err
		}
	}
	return nil
}
func cleanupTenant(c *authingapi.Client, id, name string) error {
	if id == "" || !sandboxCode.MatchString(strings.TrimSuffix(name, "-updated")) {
		return errors.New("no state-pinned tenant identity for cleanup")
	}
	status, _, err := tenantGET(c, id)
	if err != nil {
		return err
	}
	if status == 404 {
		return nil
	}
	if err := verifyEmptyOwnedTenant(c, id, name); err != nil {
		return err
	}
	status, raw, err := tenantEnvelope(c, "/api/v3/delete-tenant", http.MethodPost, map[string]string{"tenantId": id})
	var data struct {
		Success *bool `json:"success"`
	}
	if err != nil || status != 200 || json.Unmarshal(raw, &data) != nil || data.Success == nil || !*data.Success {
		return errors.New("tenant deletion unconfirmed")
	}
	status, _, err = tenantGET(c, id)
	if err != nil || status != 404 {
		return errors.New("tenant absence unconfirmed")
	}
	return nil
}
func tenantStateID(root, terraform string, env []string, name string) (string, error) {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.New("tenant state unavailable")
	}
	var state struct {
		Values struct {
			Root struct {
				Resources []struct {
					Address string `json:"address"`
					Values  struct {
						ID     string   `json:"id"`
						Name   string   `json:"name"`
						AppIDs []string `json:"app_ids"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil || len(state.Values.Root.Resources) != 1 {
		return "", errors.New("tenant state incomplete")
	}
	r := state.Values.Root.Resources[0]
	if r.Address != "authing_tenant.sandbox" || r.Values.ID == "" || r.Values.Name != name || len(r.Values.AppIDs) != 0 {
		return "", errors.New("tenant state identity mismatch")
	}
	return r.Values.ID, nil
}
func runTenantTrace(root string, credentials map[string]string, name string) (result error) {
	if !sandboxCode.MatchString(name) {
		return errors.New("tenant trace requires generated name")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	goBinary, goErr := exec.LookPath("go")
	if goErr != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
	}
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("tenant phase=terraform-cli code=%s", name)
	}
	if _, err := os.Stat(goBinary); err != nil {
		return fmt.Errorf("tenant phase=go-toolchain code=%s", name)
	}
	c, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("tenant phase=client code=%s", name)
	}
	if tenantNameAbsent(c, name) != nil {
		return fmt.Errorf("tenant phase=preflight code=%s", name)
	}
	dir, example := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(dir, 0700) != nil || os.MkdirAll(example, 0700) != nil {
		return fmt.Errorf("tenant phase=workspace code=%s", name)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(dir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("tenant phase=provider-build code=%s (output suppressed)", name)
	}
	rc := filepath.Join(root, "terraform.rc")
	hcl := fmt.Sprintf(`terraform {
 required_providers {
  authing = { source = %q }
 }
}
provider "authing" {}
resource "authing_tenant" "sandbox" {
 name = %q
 app_ids = []
}
`, source, name)
	tf := filepath.Join(example, "main.tf")
	if os.WriteFile(rc, []byte(fmt.Sprintf("provider_installation {\n dev_overrides { %q = %q }\n direct {}\n}\n", source, dir)), 0600) != nil || os.WriteFile(tf, []byte(hcl), 0600) != nil {
		return fmt.Errorf("tenant phase=config code=%s", name)
	}
	env := traceEnvironment(root, rc, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	id := ""
	expected := name
	started := false
	defer func() {
		if !started {
			return
		}
		// A failed apply can still leave a valid, state-pinned created ID.
		// Do not search by name when Terraform never persisted that ID.
		if id == "" {
			if pinned, err := tenantStateID(root, terraform, env, name); err == nil {
				id = pinned
			}
		}
		// Never discover by name to delete after an apply without a state ID.
		if id == "" {
			if result != nil {
				result = fmt.Errorf("%v cleanup=unknown (no state-pinned ID; manual inspection required)", result)
			}
			return
		}
		// Future incident evidence is pinned to Terraform state, not discovery.
		// A non-reversible fingerprint is safe for logs; it does not grant
		// deletion authority to this run's read-only name-derived candidates.
		if result != nil {
			result = fmt.Errorf("%v state_id_sha256=%x", result, sha256.Sum256([]byte(id)))
		}
		if err := cleanupTenant(c, id, expected); err != nil {
			if result != nil {
				result = fmt.Errorf("%v cleanup=incomplete (manual inspection required)", result)
			} else {
				result = fmt.Errorf("tenant phase=cleanup-incomplete code=%s", name)
			}
		} else if result != nil {
			result = fmt.Errorf("%v cleanup=confirmed", result)
		}
	}()
	started = true
	result = executeTenantTrace(traceCase{name: "tenant", code: name, phases: []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"pin-created-id", func() error { var e error; id, e = tenantStateID(root, terraform, env, name); return e }},
		{"verify-created", func() error { return verifyEmptyOwnedTenant(c, id, expected) }},
		{"plan-converged", plan(0)},
		{"configure-name-update", func() error {
			updated := strings.Replace(hcl, fmt.Sprintf("name = %q", name), fmt.Sprintf("name = %q", name+"-updated"), 1)
			if updated == hcl {
				return errors.New("unchanged HCL")
			}
			if err := os.WriteFile(tf, []byte(updated), 0600); err != nil {
				return err
			}
			expected = name + "-updated"
			return nil
		}},
		{"plan-name-update", plan(2)},
		{"apply-name-update", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-name-update", func() error {
			stateID, e := tenantStateID(root, terraform, env, expected)
			if e != nil || stateID != id {
				return errors.New("tenant state changed identity")
			}
			s, v, e := tenantGET(c, id)
			if e != nil || s != 200 || v.Name != expected {
				return errors.New("tenant name update missing")
			}
			return verifyEmptyOwnedTenant(c, id, expected)
		}},
		{"plan-updated", plan(0)},
		{"remote-name-drift", func() error {
			if e := verifyEmptyOwnedTenant(c, id, expected); e != nil {
				return e
			}
			s, raw, e := tenantEnvelope(c, "/api/v3/update-tenant", http.MethodPost, map[string]any{"tenantId": id, "name": expected + "-drift", "appIds": []string{}})
			var v struct {
				Success *bool `json:"success"`
			}
			if e != nil || s != 200 || json.Unmarshal(raw, &v) != nil || v.Success == nil || !*v.Success {
				return errors.New("tenant drift update failed")
			}
			s, got, e := tenantGET(c, id)
			if e != nil || s != 200 || got.Name != expected+"-drift" {
				return errors.New("tenant drift missing")
			}
			return nil
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-before-destroy", func() error {
			s, v, e := tenantGET(c, id)
			if e != nil || s != 200 || v.Name != expected {
				return errors.New("tenant reconciliation missing")
			}
			return verifyEmptyOwnedTenant(c, id, expected)
		}},
		{"destroy", func() error {
			if e := verifyEmptyOwnedTenant(c, id, expected); e != nil {
				return e
			}
			return run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")()
		}},
		{"verify-absent", func() error {
			s, _, e := tenantGET(c, id)
			if e != nil || s != 404 {
				return errors.New("tenant still exists")
			}
			return nil
		}},
	}})
	return result
}
