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

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"terraform-provider-authing/internal/authingapi"
)

// The exact GET is the only authority for memberId; a list row or a name is
// never used as a detach target.
func exactTenantMember(c *authingapi.Client, tenantID, userID string) (string, bool, error) {
	status, raw, err := tenantEnvelope(c, "/api/v3/get-tenant-user", http.MethodGet, map[string]string{"tenantId": tenantID, "linkUserId": userID})
	if err != nil {
		return "", false, err
	}
	if status == 404 {
		return "", false, nil
	}
	var data struct {
		TenantID   string `json:"tenantId"`
		LinkUserID string `json:"linkUserId"`
		MemberID   string `json:"memberId"`
		Admin      *bool  `json:"isTenantAdmin"`
	}
	if status != 200 || json.Unmarshal(raw, &data) != nil || data.TenantID != tenantID || data.LinkUserID != userID || data.MemberID == "" || data.Admin == nil || *data.Admin {
		return "", false, errors.New("exact tenant member identity or non-admin status unverified")
	}
	return data.MemberID, true, nil
}

// Require a complete, exclusive, unfiltered inventory. Even a missing empty
// list or a positive count with a short page blocks deletion.
func tenantMemberInventory(c *authingapi.Client, tenantID, userID, memberID string, active bool) error {
	const limit = 100
	seen := map[string]bool{}
	total := -1
	for page := 1; ; page++ {
		status, raw, err := tenantEnvelope(c, "/api/v3/list-tenant-users", http.MethodPost, map[string]any{"tenantId": tenantID, "options": map[string]any{"pagination": map[string]int{"page": page, "limit": limit}}})
		var data struct {
			Total *int `json:"totalCount"`
			List  *[]struct {
				MemberID   string `json:"memberId"`
				LinkUserID string `json:"linkUserId"`
				TenantID   string `json:"tenantId"`
				Admin      *bool  `json:"isTenantAdmin"`
			} `json:"list"`
		}
		if err != nil || status != 200 || json.Unmarshal(raw, &data) != nil || data.Total == nil || data.List == nil || *data.Total < 0 {
			return errors.New("tenant member inventory unknown")
		}
		if total < 0 {
			total = *data.Total
		}
		if *data.Total != total || len(*data.List) != min(limit, total-len(seen)) {
			return errors.New("tenant member inventory incomplete")
		}
		for _, row := range *data.List {
			if !active || row.MemberID == "" || row.MemberID != memberID || row.LinkUserID != userID || row.TenantID != tenantID || row.Admin == nil || *row.Admin || seen[row.MemberID] {
				return errors.New("foreign or duplicate tenant member")
			}
			seen[row.MemberID] = true
		}
		if len(seen) == total {
			break
		}
	}
	if active && total != 1 || !active && total != 0 {
		return errors.New("tenant member inventory not exclusive")
	}
	return nil
}

// Parse only known addresses; a state-pinned user/tenant is never inferred
// from an API list. The relation's member ID must match an exact remote GET.
func tenantRelationState(root, terraform string, env []string, username, tenantName, userID, tenantID string, relation bool) (string, string, string, error) {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", "", errors.New("Terraform state unavailable")
	}
	var state struct {
		Values struct {
			Root struct {
				Resources []struct {
					Address string `json:"address"`
					Values  struct {
						ID         string    `json:"id"`
						Username   string    `json:"username"`
						Name       string    `json:"name"`
						AppIDs     *[]string `json:"app_ids"`
						TenantID   string    `json:"tenant_id"`
						LinkUserID string    `json:"link_user_id"`
						MemberID   string    `json:"member_id"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return "", "", "", errors.New("Terraform state invalid")
	}
	seen := map[string]bool{}
	member := ""
	for _, r := range state.Values.Root.Resources {
		if seen[r.Address] {
			return "", "", "", errors.New("duplicate state address")
		}
		seen[r.Address] = true
		switch r.Address {
		case "authing_user.sandbox":
			if r.Values.ID == "" || r.Values.Username != username || userID != "" && r.Values.ID != userID {
				return "", "", "", errors.New("user state identity mismatch")
			}
			userID = r.Values.ID
		case "authing_tenant.sandbox":
			if r.Values.ID == "" || r.Values.Name != tenantName || r.Values.AppIDs == nil || len(*r.Values.AppIDs) != 0 || tenantID != "" && r.Values.ID != tenantID {
				return "", "", "", errors.New("tenant state identity mismatch")
			}
			tenantID = r.Values.ID
		case "authing_tenant_membership.sandbox":
			if !relation || r.Values.ID == "" || r.Values.MemberID == "" || r.Values.TenantID == "" || r.Values.LinkUserID == "" {
				return "", "", "", errors.New("relation state incomplete")
			}
			member = r.Values.MemberID
		default:
			return "", "", "", errors.New("unexpected resource in state")
		}
	}
	if !seen["authing_user.sandbox"] || !seen["authing_tenant.sandbox"] || relation != seen["authing_tenant_membership.sandbox"] {
		return "", "", "", errors.New("parent or relation state incomplete")
	}
	if relation {
		encoded, _ := json.Marshal([2]string{tenantID, userID})
		composite := "v1." + base64.RawURLEncoding.EncodeToString(encoded)
		for _, r := range state.Values.Root.Resources {
			if r.Address == "authing_tenant_membership.sandbox" && (r.Values.ID != composite || r.Values.TenantID != tenantID || r.Values.LinkUserID != userID) {
				return "", "", "", errors.New("relation state identity mismatch")
			}
		}
	}
	return userID, tenantID, member, nil
}

func detachTenantMember(c *authingapi.Client, tenantID, userID, memberID string) error {
	exact, found, err := exactTenantMember(c, tenantID, userID)
	if err != nil {
		return err
	}
	if found && (memberID == "" || memberID != exact) {
		return errors.New("pinned member ID changed")
	}
	if err := tenantMemberInventory(c, tenantID, userID, memberID, found); err != nil {
		return err
	}
	if found {
		status, raw, err := tenantEnvelope(c, "/api/v3/remove-tenant-users", http.MethodPost, map[string]any{"tenantId": tenantID, "memberIds": []string{memberID}})
		var data struct {
			Success *bool `json:"success"`
		}
		if err != nil || status != 200 || json.Unmarshal(raw, &data) != nil || data.Success == nil || !*data.Success {
			return errors.New("tenant member detach failed")
		}
	}
	_, found, err = exactTenantMember(c, tenantID, userID)
	if err != nil || found {
		return errors.New("tenant member detach unverified")
	}
	if err := tenantMemberInventory(c, tenantID, userID, memberID, false); err != nil {
		return err
	}
	return nil // caller independently verifies the exact owned user remains
}

func runTenantMembershipTrace(root string, credentials map[string]string, username, name string) (result error) {
	if !sandboxCode.MatchString(username) || !sandboxCode.MatchString(name) || username == name {
		return errors.New("generated distinct tenant and user names required")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	goBinary, goErr := exec.LookPath("go")
	if goErr != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
	}
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("tenant-membership phase=terraform-cli code=%s", name)
	}
	if _, err := os.Stat(goBinary); err != nil {
		return fmt.Errorf("tenant-membership phase=go-toolchain code=%s", name)
	}
	c, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return errors.New("tenant-membership client unavailable")
	}
	if tenantNameAbsent(c, name) != nil {
		return fmt.Errorf("tenant-membership phase=preflight code=%s", name)
	}
	providerDir, example := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(example, 0700) != nil {
		return errors.New("workspace unavailable")
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return errors.New("provider build failed (output suppressed)")
	}
	rc := filepath.Join(root, "terraform.rc")
	hcl := fmt.Sprintf(`terraform {
 required_providers {
  authing = { source = %q }
 }
}
provider "authing" {}
resource "authing_user" "sandbox" {
 username = %q
 nickname = %q
}
resource "authing_tenant" "sandbox" {
 name = %q
 app_ids = []
}
resource "authing_tenant_membership" "sandbox" {
 tenant_id = authing_tenant.sandbox.id
 link_user_id = authing_user.sandbox.id
}
`, source, username, username, name)
	if os.WriteFile(rc, []byte(fmt.Sprintf("provider_installation {\n dev_overrides { %q = %q }\n direct {}\n}\n", source, providerDir)), 0600) != nil || os.WriteFile(filepath.Join(example, "main.tf"), []byte(hcl), 0600) != nil {
		return errors.New("config unavailable")
	}
	env := traceEnvironment(root, rc, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	userID, tenantID, memberID := "", "", ""
	tenantStarted := false
	defer func() {
		if result == nil {
			return
		} // successful teardown independently verified
		// Only state-derived IDs can authorize cleanup after a partial apply.
		if userID == "" {
			userID, _ = userStateID(root, terraform, env, username)
		}
		if tenantID == "" && userID != "" {
			_, tenantID, _, _ = tenantRelationState(root, terraform, env, username, name, userID, "", false)
		}
		if tenantStarted && tenantID == "" {
			result = fmt.Errorf("%v cleanup=unknown (tenant ID missing)", result)
			return
		}
		if userID == "" && tenantID == "" {
			result = fmt.Errorf("%v cleanup=unknown (user ID missing)", result)
			return
		}
		if tenantID != "" {
			if userID == "" {
				result = fmt.Errorf("%v cleanup=unknown (user ID missing)", result)
				return
			}
			status, v, err := tenantGET(c, tenantID)
			if err != nil || status != 200 && status != 404 || status == 200 && (v.Name != name || len(*v.AppIDs) != 0) {
				result = fmt.Errorf("%v cleanup=incomplete (tenant ownership)", result)
				return
			}
			if status == 200 {
				if err := verifyOwnedUser(c, userID, username); err != nil {
					result = fmt.Errorf("%v cleanup=incomplete (user ownership)", result)
					return
				}
				if memberID == "" {
					_, _, memberID, _ = tenantRelationState(root, terraform, env, username, name, userID, tenantID, true)
				}
				if err := detachTenantMember(c, tenantID, userID, memberID); err != nil {
					result = fmt.Errorf("%v cleanup=incomplete (relation)", result)
					return
				}
				if err := cleanupTenant(c, tenantID, name); err != nil {
					result = fmt.Errorf("%v cleanup=incomplete (tenant)", result)
					return
				}
			}
		}
		if userID != "" {
			if err := cleanupUser(c, userID, username); err != nil {
				result = fmt.Errorf("%v cleanup=incomplete (user)", result)
			}
		}
	}()
	result = (traceCase{name: "tenant-membership", code: name, phases: []tracePhase{
		{"apply-user", run(0, "apply", "-target=authing_user.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"pin-user", func() error { var e error; userID, e = userStateID(root, terraform, env, username); return e }},
		{"verify-user", func() error { return verifyOwnedUser(c, userID, username) }},
		{"apply-tenant", func() error {
			tenantStarted = true
			return run(0, "apply", "-target=authing_tenant.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")()
		}},
		{"pin-tenant", func() error {
			var e error
			_, tenantID, _, e = tenantRelationState(root, terraform, env, username, name, userID, "", false)
			return e
		}},
		{"verify-tenant", func() error { return verifyEmptyOwnedTenant(c, tenantID, name) }},
		{"apply-relation", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"pin-member", func() error {
			var e error
			_, _, memberID, e = tenantRelationState(root, terraform, env, username, name, userID, tenantID, true)
			if e != nil {
				return e
			}
			remote, found, e := exactTenantMember(c, tenantID, userID)
			if e != nil || !found || remote != memberID {
				return errors.New("state member ID not pinned by exact GET")
			}
			return nil
		}},
		{"plan-converged", plan(0)},
		{"verify-created", func() error { return tenantMemberInventory(c, tenantID, userID, memberID, true) }},
		{"remote-revoke", func() error {
			if err := detachTenantMember(c, tenantID, userID, memberID); err != nil {
				return err
			}
			return verifyOwnedUser(c, userID, username)
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"pin-restored-member", func() error {
			var e error
			_, _, memberID, e = tenantRelationState(root, terraform, env, username, name, userID, tenantID, true)
			if e != nil {
				return e
			}
			remote, found, e := exactTenantMember(c, tenantID, userID)
			if e != nil || !found || remote != memberID {
				return errors.New("restored member ID not pinned")
			}
			return nil
		}},
		{"plan-reconverged", plan(0)},
		{"verify-restored", func() error { return tenantMemberInventory(c, tenantID, userID, memberID, true) }},
		{"destroy-relation", func() error {
			if err := tenantMemberInventory(c, tenantID, userID, memberID, true); err != nil {
				return err
			}
			return run(0, "destroy", "-target=authing_tenant_membership.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")()
		}},
		{"verify-user-survives-detach", func() error {
			_, found, e := exactTenantMember(c, tenantID, userID)
			if e != nil || found {
				return errors.New("membership remains")
			}
			if e := tenantMemberInventory(c, tenantID, userID, memberID, false); e != nil {
				return e
			}
			return verifyOwnedUser(c, userID, username)
		}},
		{"destroy-tenant", func() error {
			if err := verifyEmptyOwnedTenant(c, tenantID, name); err != nil {
				return err
			}
			if err := verifyOwnedUser(c, userID, username); err != nil {
				return err
			}
			return run(0, "destroy", "-target=authing_tenant.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")()
		}},
		{"verify-tenant-absent-user-survives", func() error {
			status, _, e := tenantGET(c, tenantID)
			if e != nil || status != 404 {
				return errors.New("tenant remains")
			}
			return verifyOwnedUser(c, userID, username)
		}},
		{"destroy-user", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-parents-absent", func() error {
			status, _, e := tenantGET(c, tenantID)
			if e != nil || status != 404 {
				return errors.New("tenant remains")
			}
			got := c.GetUser(&dto.GetUserDto{UserId: userID})
			if got == nil || got.StatusCode != 404 {
				return errors.New("user remains")
			}
			return nil
		}},
	}}).execute()
	return result
}
