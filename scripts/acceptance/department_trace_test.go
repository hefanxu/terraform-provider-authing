package acceptance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"terraform-provider-authing/internal/authingapi"
)

func departmentGet(client *authingapi.Client, org, canonical, openID, name, marker string) error {
	if !sandboxCode.MatchString(org) || canonical == "" || !sandboxCode.MatchString(openID) || marker != "hermesacc ownership "+openID {
		return errors.New("invalid generated department identity")
	}
	body, err := client.SendHttpRequest("/api/v3/get-department", http.MethodGet, map[string]any{"organizationCode": org, "departmentId": canonical})
	var out struct {
		StatusCode *int `json:"statusCode"`
		Data       *struct {
			ID          string `json:"departmentId"`
			OpenID      string `json:"openDepartmentId"`
			Org         string `json:"organizationCode"`
			Name        string `json:"name"`
			Parent      string `json:"parentDepartmentId"`
			Description string `json:"description"`
			Children    *bool  `json:"hasChildren"`
			Members     *int   `json:"membersCount"`
		} `json:"data"`
	}
	if err != nil || json.Unmarshal(body, &out) != nil || out.StatusCode == nil || *out.StatusCode != 200 || out.Data == nil || out.Data.ID != canonical || out.Data.OpenID != openID || out.Data.Org != org || out.Data.Name != name || out.Data.Parent != "root" || out.Data.Description != marker {
		return errors.New("department exact identity not confirmed")
	}
	return nil
}
func departmentEmpty(client *authingapi.Client, org, canonical, openID, name, marker string) error {
	if err := departmentGet(client, org, canonical, openID, name, marker); err != nil {
		return err
	}
	body, err := client.SendHttpRequest("/api/v3/get-department", http.MethodGet, map[string]any{"organizationCode": org, "departmentId": canonical})
	var detail struct {
		StatusCode *int `json:"statusCode"`
		Data       *struct {
			HasChildren  *bool `json:"hasChildren"`
			MembersCount *int  `json:"membersCount"`
		} `json:"data"`
	}
	if err != nil || json.Unmarshal(body, &detail) != nil || detail.StatusCode == nil || *detail.StatusCode != 200 || detail.Data == nil || detail.Data.HasChildren == nil || *detail.Data.HasChildren || detail.Data.MembersCount == nil || *detail.Data.MembersCount != 0 {
		return errors.New("department occupancy not proven empty")
	}
	// GET alone cannot prove emptiness. Explicit, complete, scoped inventories are mandatory.
	for _, entry := range []struct {
		path  string
		query map[string]any
	}{
		{"/api/v3/list-children-departments", map[string]any{"organizationCode": org, "departmentId": canonical}},
		{"/api/v3/get-all-departments", map[string]any{"organizationCode": org, "departmentId": canonical}},
		{"/api/v3/list-department-members", map[string]any{"organizationCode": org, "departmentId": canonical, "page": 1, "limit": 1, "includeChildrenDepartments": true}},
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
			return errors.New("department children/members unknown or nonempty")
		}
	}
	return nil
}
func cleanupDepartment(host, org, id, marker string) error {
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key-marker", AccessKeySecret: "credential-marker", Host: host})
	if err != nil {
		return errors.New("client unavailable")
	}
	return cleanupDepartmentClient(client, org, id, id, id, marker)
}
func cleanupDepartmentClient(client *authingapi.Client, org, canonical, openID, name, marker string) error {
	if !sandboxCode.MatchString(org) || canonical == "" || !sandboxCode.MatchString(openID) {
		return errors.New("invalid identity")
	}
	got := client.GetDepartment(&dto.GetDepartmentDto{OrganizationCode: org, DepartmentId: canonical})
	if got != nil && got.StatusCode == 404 {
		return nil
	}
	if err := departmentEmpty(client, org, canonical, openID, name, marker); err != nil {
		return err
	}
	deleted := client.DeleteDepartment(&dto.DeleteDepartmentReqDto{OrganizationCode: org, DepartmentId: canonical})
	if deleted == nil || deleted.StatusCode != 200 || !deleted.Data.Success {
		return errors.New("department deletion not confirmed")
	}
	got = client.GetDepartment(&dto.GetDepartmentDto{OrganizationCode: org, DepartmentId: canonical})
	if got == nil || got.StatusCode != 404 {
		return errors.New("department absence not confirmed")
	}
	return nil
}
func departmentStateID(root, terraform string, env []string, org, openID string) (string, error) {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	body, err := cmd.Output()
	if err != nil {
		return "", errors.New("state inspection failed")
	}
	var state struct {
		Values struct {
			Root struct {
				Resources []struct {
					Address string `json:"address"`
					Values  struct {
						ID           string `json:"id"`
						DepartmentID string `json:"department_id"`
						Org          string `json:"organization_code"`
						Parent       string `json:"parent_department_id"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(body, &state) != nil {
		return "", errors.New("state malformed")
	}
	seenOrg, canonical := false, ""
	for _, r := range state.Values.Root.Resources {
		if r.Address == "authing_organization.sandbox" && r.Values.ID == org {
			seenOrg = true
		}
		if r.Address == "authing_department.sandbox" && r.Values.ID != "" && r.Values.DepartmentID == openID && r.Values.Org == org && r.Values.Parent == "root" {
			canonical = r.Values.ID
		}
	}
	if !seenOrg || canonical == "" {
		return "", errors.New("exact parent/department IDs missing in Terraform state")
	}
	return canonical, nil
}
func runDepartmentTrace(root string, credentials map[string]string, org string, departmentIDs ...string) (result error) {
	id := org
	if len(departmentIDs) > 0 {
		id = departmentIDs[0]
	}
	if !sandboxCode.MatchString(org) || !sandboxCode.MatchString(id) {
		return errors.New("invalid generated identity")
	}
	marker := "hermesacc ownership " + id
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("department phase=terraform-cli code=%s (output suppressed)", org)
	}
	goBinary := filepath.Join(repo, "../.tools/go/bin/go")
	if _, err := os.Stat(goBinary); err != nil {
		return fmt.Errorf("department phase=go-toolchain code=%s (output suppressed)", org)
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return errors.New("department client unavailable")
	}
	if x := client.GetOrganization(&dto.GetOrganizationDto{OrganizationCode: org}); x == nil || x.StatusCode != 404 {
		return fmt.Errorf("department phase=preflight-organization code=%s (output suppressed)", org)
	}
	if x := client.GetDepartment(&dto.GetDepartmentDto{OrganizationCode: org, DepartmentId: id}); x == nil || x.StatusCode != 404 {
		return fmt.Errorf("department phase=preflight-department code=%s (output suppressed)", org)
	}
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return errors.New("department workspace unavailable")
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("department phase=provider-build code=%s (output suppressed)", org)
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
resource "authing_department" "sandbox" {
 organization_code = authing_organization.sandbox.organization_code
 department_id = %q
 name = %q
 parent_department_id = "root"
 description = %q
}
`, source, org, org, "hermesacc ownership "+org, id, id, marker)
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return errors.New("department config unavailable")
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	orgCreated, deptPinned, canonical := false, false, ""
	expectedName := id
	defer func() {
		if !orgCreated {
			return
		}
		// Never delete by name after an ambiguous creation. An exact state ID is required.
		cleanup := "confirmed"
		if !deptPinned || cleanupDepartmentClient(client, org, canonical, id, expectedName, marker) != nil || cleanupOrganization(client, org, org, "hermesacc ownership "+org) != nil {
			cleanup = "incomplete"
		}
		if result != nil {
			result = fmt.Errorf("%v cleanup=%s", result, cleanup)
		} else if cleanup != "confirmed" {
			result = fmt.Errorf("department phase=cleanup-incomplete code=%s cleanup=incomplete (output suppressed)", org)
		}
	}()
	return (traceCase{name: "department", code: org, phases: []tracePhase{
		{"apply-create-organization", func() error {
			// Stage parent first: failure after child apply cannot authorize deleting an unknown child.
			parent := strings.Split(hcl, "resource \"authing_department\"")[0]
			if err := os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(parent), 0600); err != nil {
				return err
			}
			return terraformExit(root, env, terraform, 0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")
		}},
		{"verify-parent", func() error {
			orgCreated = true
			return verifyOwnedOrganization(client, org, org, "hermesacc ownership "+org)
		}},
		{"configure-department", func() error { return os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) }},
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"pin-identity", func() error {
			var err error
			if canonical, err = departmentStateID(root, terraform, env, org, id); err != nil {
				return err
			}
			deptPinned = true
			return nil
		}},
		{"verify-created", func() error { return departmentGet(client, org, canonical, id, id, marker) }},
		{"plan-converged", plan(0)},
		{"configure-name-update", func() error {
			return os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(strings.Replace(hcl, fmt.Sprintf("\n name = %q", id), fmt.Sprintf("\n name = %q", id+"-updated"), 1)), 0600)
		}},
		{"plan-name-update", plan(2)},
		{"apply-name-update", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-name-update", func() error {
			if err := departmentGet(client, org, canonical, id, id+"-updated", marker); err != nil {
				return err
			}
			expectedName = id + "-updated"
			return nil
		}},
		{"plan-updated", plan(0)},
		{"remote-drift", func() error {
			x := client.UpdateDepartment(&dto.UpdateDepartmentReqDto{OrganizationCode: org, DepartmentId: canonical, Name: id + "-drift", ParentDepartmentId: "root", Description: marker})
			if x == nil || x.StatusCode != 200 {
				return errors.New("drift mutation failed")
			}
			if err := departmentGet(client, org, canonical, id, id+"-drift", marker); err != nil {
				return err
			}
			expectedName = id + "-drift"
			return nil
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-reconciled", func() error {
			if err := departmentGet(client, org, canonical, id, id+"-updated", marker); err != nil {
				return err
			}
			expectedName = id + "-updated"
			return nil
		}},
		{"verify-safe-before-destroy", func() error { return departmentEmpty(client, org, canonical, id, id+"-updated", marker) }},
		{"destroy-department", run(0, "destroy", "-target=authing_department.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-department-absent", func() error {
			x := client.GetDepartment(&dto.GetDepartmentDto{OrganizationCode: org, DepartmentId: canonical})
			if x == nil || x.StatusCode != 404 {
				return errors.New("department absence not confirmed")
			}
			return nil
		}},
		{"verify-parent-safe", func() error { return verifyOwnedOrganization(client, org, org, "hermesacc ownership "+org) }},
		{"destroy-organization", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-parent-absent", func() error {
			x := client.GetOrganization(&dto.GetOrganizationDto{OrganizationCode: org})
			if x == nil || x.StatusCode != 404 {
				return errors.New("organization absence not confirmed")
			}
			return nil
		}},
	}}).execute()
}
