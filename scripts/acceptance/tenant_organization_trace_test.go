package acceptance

import (
	"encoding/base64"
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

func tenantOrgID(tenant, code string) string {
	b, _ := json.Marshal([2]string{tenant, code})
	return "v1." + base64.RawURLEncoding.EncodeToString(b)
}

type tenantOrgIdentity struct {
	Tenant       string `json:"tenantId"`
	Code         string `json:"organizationCode"`
	Name         string `json:"organizationName"`
	Description  string `json:"description"`
	HasChildren  *bool  `json:"hasChildren"`
	MembersCount *int   `json:"membersCount"`
}

func tenantOrgGET(c *authingapi.Client, tenant, code string) (int, tenantOrgIdentity, error) {
	var v tenantOrgIdentity
	status, raw, err := tenantEnvelope(c, "/api/v3/get-organization", http.MethodGet, map[string]string{"tenantId": tenant, "organizationCode": code})
	if err != nil || status == 404 {
		return status, v, err
	}
	if status != 200 || json.Unmarshal(raw, &v) != nil || v.Tenant != tenant || v.Code != code || v.Name == "" {
		return status, v, errors.New("tenant organization identity unknown")
	}
	return status, v, nil
}
func verifyTenantOrganization(c *authingapi.Client, tenant, code, name, marker string) error {
	if tenant == "" || !sandboxCode.MatchString(code) || marker != "hermesacc ownership "+code || name != code && name != code+"-updated" && name != code+"-updated-drift" {
		return errors.New("generated organization identity invalid")
	}
	status, v, err := tenantOrgGET(c, tenant, code)
	if err != nil || status != 200 || v.Name != name || v.Description != marker || v.HasChildren == nil || *v.HasChildren || v.MembersCount == nil || *v.MembersCount != 0 {
		return errors.New("organization identity or occupancy unverified")
	}
	for _, e := range []struct {
		path  string
		query map[string]any
	}{
		{"/api/v3/list-children-departments", map[string]any{"tenantId": tenant, "organizationCode": code, "departmentId": "root"}},
		{"/api/v3/get-all-departments", map[string]any{"tenantId": tenant, "organizationCode": code, "departmentId": "root"}},
		{"/api/v3/list-department-members", map[string]any{"tenantId": tenant, "organizationCode": code, "departmentId": "root", "page": 1, "limit": 1, "includeChildrenDepartments": true}},
	} {
		if emptyTenantCollection(c, e.path, http.MethodGet, e.query) != nil {
			return errors.New("organization descendants or members unknown")
		}
	}
	return nil
}
func cleanupTenantOrganization(c *authingapi.Client, tenant, code, name, marker string) error {
	if tenant == "" || !sandboxCode.MatchString(code) || marker != "hermesacc ownership "+code {
		return errors.New("unowned organization")
	}
	status, _, err := tenantOrgGET(c, tenant, code)
	if err != nil {
		return err
	}
	if status == 404 {
		return nil
	}
	if err := verifyTenantOrganization(c, tenant, code, name, marker); err != nil {
		return err
	}
	status, raw, err := tenantEnvelope(c, "/api/v3/delete-organization", http.MethodPost, map[string]string{"tenantId": tenant, "organizationCode": code})
	var v struct {
		Success *bool `json:"success"`
	}
	if err != nil || status != 200 || json.Unmarshal(raw, &v) != nil || v.Success == nil || !*v.Success {
		return errors.New("organization delete unconfirmed")
	}
	status, _, err = tenantOrgGET(c, tenant, code)
	if err != nil || status != 404 {
		return errors.New("organization absence unconfirmed")
	}
	return nil
}

// A child can be deleted only when its composite identity is pinned by Terraform state.
func tenantOrgState(root, terraform string, env []string, tenant, code string) error {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New("state unavailable")
	}
	var state struct {
		Values struct {
			Root struct {
				Resources []struct {
					Address string `json:"address"`
					Values  struct {
						ID          string `json:"id"`
						Tenant      string `json:"tenant_id"`
						Code        string `json:"organization_code"`
						Name        string `json:"organization_name"`
						Description string `json:"description"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil || len(state.Values.Root.Resources) != 2 {
		return errors.New("organization state incomplete")
	}
	found := false
	for _, r := range state.Values.Root.Resources {
		if r.Address == "authing_tenant_organization.sandbox" {
			if found || r.Values.ID != tenantOrgID(tenant, code) || r.Values.Tenant != tenant || r.Values.Code != code || r.Values.Description != "hermesacc ownership "+code || r.Values.Name != code && r.Values.Name != code+"-updated" {
				return errors.New("organization state identity mismatch")
			}
			found = true
		} else if r.Address != "authing_tenant.sandbox" || r.Values.ID != tenant {
			return errors.New("unexpected state identity")
		}
	}
	if !found {
		return errors.New("organization state missing")
	}
	return nil
}
func runTenantOrganizationTrace(root string, credentials map[string]string, name, code string) (result error) {
	if !sandboxCode.MatchString(name) || !sandboxCode.MatchString(code) || name == code {
		return errors.New("distinct generated identities required")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
	}
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("tenant-organization phase=terraform-cli code=%s", code)
	}
	if _, err := os.Stat(goBinary); err != nil {
		return fmt.Errorf("tenant-organization phase=go-toolchain code=%s", code)
	}
	c, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return errors.New("client unavailable")
	}
	if tenantNameAbsent(c, name) != nil {
		return fmt.Errorf("tenant-organization phase=preflight code=%s", code)
	}
	dir, example := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(dir, 0700) != nil || os.MkdirAll(example, 0700) != nil {
		return errors.New("workspace unavailable")
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(dir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return errors.New("provider build failed")
	}
	rc := filepath.Join(root, "terraform.rc")
	tf := filepath.Join(example, "main.tf")
	parent := fmt.Sprintf("terraform {\n required_providers {\n  authing = { source = %q }\n }\n}\nprovider \"authing\" {}\nresource \"authing_tenant\" \"sandbox\" {\n name = %q\n app_ids = []\n}\n", source, name)
	child := fmt.Sprintf("resource \"authing_tenant_organization\" \"sandbox\" {\n tenant_id = authing_tenant.sandbox.id\n organization_code = %q\n organization_name = %q\n description = %q\n}\n", code, code, "hermesacc ownership "+code)
	if os.WriteFile(rc, []byte(fmt.Sprintf("provider_installation {\n dev_overrides { %q = %q }\n direct {}\n}\n", source, dir)), 0600) != nil || os.WriteFile(tf, []byte(parent), 0600) != nil {
		return errors.New("config unavailable")
	}
	env := traceEnvironment(root, rc, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	apply := run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	tenant := ""
	pinned := false
	expected := code
	started := false
	defer func() {
		if !started {
			return
		}
		if tenant == "" {
			tenant, _ = tenantStateID(root, terraform, env, name)
		}
		if tenant == "" {
			if result != nil {
				result = fmt.Errorf("%v cleanup=unknown", result)
			}
			return
		}
		status, _, e := tenantOrgGET(c, tenant, code)
		if e != nil || status != 404 && !pinned {
			if result != nil {
				result = fmt.Errorf("%v cleanup=incomplete (organization identity)", result)
			}
			return
		}
		if status == 200 && cleanupTenantOrganization(c, tenant, code, expected, "hermesacc ownership "+code) != nil {
			if result != nil {
				result = fmt.Errorf("%v cleanup=incomplete (organization)", result)
			}
			return
		}
		if cleanupTenant(c, tenant, name) != nil {
			if result != nil {
				result = fmt.Errorf("%v cleanup=incomplete (tenant)", result)
			} else {
				result = fmt.Errorf("tenant-organization phase=cleanup-incomplete code=%s", code)
			}
		}
	}()
	result = (traceCase{name: "tenant-organization", code: code, phases: []tracePhase{
		{"apply-tenant", func() error { started = true; return apply() }},
		{"pin-tenant", func() error { var e error; tenant, e = tenantStateID(root, terraform, env, name); return e }},
		{"verify-tenant", func() error { return verifyEmptyOwnedTenant(c, tenant, name) }},
		{"preflight-org", func() error {
			status, _, e := tenantOrgGET(c, tenant, code)
			if e != nil || status != 404 {
				return errors.New("organization preflight unknown")
			}
			return nil
		}},
		{"configure-org", func() error { return os.WriteFile(tf, []byte(parent+child), 0600) }},
		{"apply-org", apply},
		{"pin-org", func() error {
			if e := tenantOrgState(root, terraform, env, tenant, code); e != nil {
				return e
			}
			pinned = true
			return nil
		}},
		{"verify-created", func() error { return verifyTenantOrganization(c, tenant, code, expected, "hermesacc ownership "+code) }},
		{"plan-converged", plan(0)},
		{"configure-name-update", func() error {
			return os.WriteFile(tf, []byte(parent+strings.Replace(child, fmt.Sprintf("organization_name = %q", code), fmt.Sprintf("organization_name = %q", code+"-updated"), 1)), 0600)
		}},
		{"plan-name-update", plan(2)},
		{"apply-name-update", apply},
		{"verify-name-update", func() error {
			expected = code + "-updated"
			if e := tenantOrgState(root, terraform, env, tenant, code); e != nil {
				return e
			}
			return verifyTenantOrganization(c, tenant, code, expected, "hermesacc ownership "+code)
		}},
		{"plan-updated", plan(0)},
		{"remote-drift", func() error {
			status, raw, e := tenantEnvelope(c, "/api/v3/update-organization", http.MethodPost, map[string]any{"tenantId": tenant, "organizationCode": code, "organizationName": code + "-updated-drift", "description": "hermesacc ownership " + code})
			if e != nil || status != 200 {
				return errors.New("drift write failed")
			}
			var v struct {
				Tenant string `json:"tenantId"`
				Code   string `json:"organizationCode"`
				Name   string `json:"organizationName"`
			}
			if json.Unmarshal(raw, &v) != nil || v.Tenant != tenant || v.Code != code || v.Name != code+"-updated-drift" {
				return errors.New("drift response identity mismatch")
			}
			return verifyTenantOrganization(c, tenant, code, code+"-updated-drift", "hermesacc ownership "+code)
		}},
		{"plan-drift", plan(2)}, {"apply-reconcile", apply}, {"plan-reconverged", plan(0)},
		{"verify-reconciled", func() error { return verifyTenantOrganization(c, tenant, code, expected, "hermesacc ownership "+code) }},
		{"destroy-org", func() error {
			if e := verifyTenantOrganization(c, tenant, code, expected, "hermesacc ownership "+code); e != nil {
				return e
			}
			return run(0, "destroy", "-target=authing_tenant_organization.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")()
		}},
		{"verify-org-absent", func() error {
			status, _, e := tenantOrgGET(c, tenant, code)
			if e != nil || status != 404 {
				return errors.New("organization remains")
			}
			return nil
		}},
		{"destroy-tenant", func() error {
			if e := verifyEmptyOwnedTenant(c, tenant, name); e != nil {
				return e
			}
			return run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")()
		}},
		{"verify-tenant-absent", func() error {
			status, _, e := tenantGET(c, tenant)
			if e != nil || status != 404 {
				return errors.New("tenant remains")
			}
			return nil
		}},
	}}).execute()
	return result
}
