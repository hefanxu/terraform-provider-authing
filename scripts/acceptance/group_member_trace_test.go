package acceptance

import (
	"context"
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

// get-user-groups has no page parameters. Its total must equal the returned
// list length: a truncated response cannot establish presence or absence.
func exactMembership(client *authingapi.Client, userID, groupCode string) (bool, error) {
	body, err := client.SendHttpRequestContext(context.Background(), "/api/v3/get-user-groups", "GET", &dto.GetUserGroupsDto{UserId: userID})
	var response struct {
		StatusCode int `json:"statusCode"`
		Data       *struct {
			TotalCount *int `json:"totalCount"`
			List       *[]struct {
				Code string `json:"code"`
			} `json:"list"`
		} `json:"data"`
	}
	if err != nil || json.Unmarshal(body, &response) != nil || response.StatusCode != 200 || response.Data == nil || response.Data.TotalCount == nil || response.Data.List == nil || *response.Data.TotalCount < 0 || *response.Data.TotalCount != len(*response.Data.List) {
		return false, errors.New("membership inventory incomplete")
	}
	found := false
	seen := map[string]bool{}
	for _, item := range *response.Data.List {
		if item.Code == "" || seen[item.Code] {
			return false, errors.New("membership inventory invalid")
		}
		seen[item.Code] = true
		if item.Code == groupCode {
			found = true
		}
	}
	return found, nil
}
func verifyMembership(client *authingapi.Client, userID, groupCode string, want bool) error {
	present, err := exactMembership(client, userID, groupCode)
	if err != nil || present != want {
		return errors.New("exact membership not verified")
	}
	return nil
}
func relationStateUserID(root, terraform string, env []string, username, groupCode string) (string, error) {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.New("Terraform state unavailable")
	}
	var state struct {
		Values struct {
			RootModule struct {
				Resources []struct {
					Address string `json:"address"`
					Values  struct {
						ID        string `json:"id"`
						Username  string `json:"username"`
						GroupCode string `json:"group_code"`
						UserID    string `json:"user_id"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return "", errors.New("Terraform state invalid")
	}
	id := ""
	relation := false
	group := false
	for _, r := range state.Values.RootModule.Resources {
		switch r.Address {
		case "authing_user.sandbox":
			if id != "" || r.Values.ID == "" || r.Values.Username != username {
				return "", errors.New("user ownership unverified")
			}
			id = r.Values.ID
		case "authing_group.sandbox":
			if group || r.Values.ID != groupCode {
				return "", errors.New("group ownership unverified")
			}
			group = true
		case "authing_group_member.sandbox":
			if relation || r.Values.GroupCode != groupCode {
				return "", errors.New("relation identity unverified")
			}
			relation = true
		default:
			return "", errors.New("unexpected resource in state")
		}
	}
	if id == "" || !group || !relation {
		return "", errors.New("relation state incomplete")
	}
	for _, r := range state.Values.RootModule.Resources {
		if r.Address == "authing_group_member.sandbox" && (r.Values.UserID != id || r.Values.ID != groupCode+":"+id) {
			return "", errors.New("relation identity mismatch")
		}
	}
	return id, nil
}

func groupMemberInventory(client *authingapi.Client, code, userID string, active bool) error {
	const limit = 100
	seen := map[string]bool{}
	total := -1
	for page := 1; ; page++ {
		body, err := client.SendHttpRequestContext(context.Background(), "/api/v3/list-group-members", http.MethodGet, &dto.ListGroupMembersDto{Code: code, Page: page, Limit: limit})
		var response struct {
			StatusCode int `json:"statusCode"`
			Data       *struct {
				TotalCount *int `json:"totalCount"`
				List       *[]struct {
					UserID string `json:"userId"`
				} `json:"list"`
			} `json:"data"`
		}
		if err != nil || json.Unmarshal(body, &response) != nil || response.StatusCode != 200 || response.Data == nil || response.Data.TotalCount == nil || response.Data.List == nil || *response.Data.TotalCount < 0 {
			return errors.New("group member inventory incomplete")
		}
		if total == -1 {
			total = *response.Data.TotalCount
		}
		if total != *response.Data.TotalCount {
			return errors.New("group member inventory changed")
		}
		expected := total - len(seen)
		if expected > limit {
			expected = limit
		}
		if len(*response.Data.List) != expected {
			return errors.New("group member page incomplete")
		}
		for _, item := range *response.Data.List {
			if item.UserID == "" || seen[item.UserID] || item.UserID != userID || !active {
				return errors.New("foreign or duplicate group member")
			}
			seen[item.UserID] = true
		}
		if len(seen) == total {
			break
		}
	}
	if active && len(seen) != 1 || !active && len(seen) != 0 {
		return errors.New("group member inventory not exclusive")
	}
	return nil
}

// Cleanup may revoke only a relation between independently verified owned
// parents. An unknown state ID is never guessed from the generated username.
func cleanupGroupMember(client *authingapi.Client, userID, username, groupCode string) error {
	if userID == "" {
		return errors.New("unknown user ID; manual cleanup required")
	}
	if err := verifyOwnedUser(client, userID, username); err != nil {
		return err
	}
	if err := verifyOwnedGroup(client, groupCode, groupCode, "hermesacc ownership "+groupCode); err != nil {
		return err
	}
	present, err := exactMembership(client, userID, groupCode)
	if err != nil {
		return err
	}
	if err := groupMemberInventory(client, groupCode, userID, present); err != nil {
		return err
	}
	if present {
		res := client.RemoveGroupMembers(&dto.RemoveGroupMembersReqDto{Code: groupCode, UserIds: []string{userID}})
		if res == nil || res.StatusCode != 200 || !res.Data.Success {
			return errors.New("relation cleanup rejected")
		}
	}
	if err := verifyMembership(client, userID, groupCode, false); err != nil {
		return err
	}
	if err := groupMemberInventory(client, groupCode, userID, false); err != nil {
		return err
	}
	if err := verifyOwnedUser(client, userID, username); err != nil {
		return err
	}
	if err := cleanupGroup(client, groupCode, groupCode, "hermesacc ownership "+groupCode); err != nil {
		return err
	}
	return cleanupUser(client, userID, username)
}

// Only validated, locally generated identifiers and the first controlled phase
// enter diagnostics; cleanup is an independent outcome.
func groupMemberDiagnostic(first error, username, code, cleanup string) error {
	if first == nil {
		first = fmt.Errorf("group-member phase=cleanup-incomplete code=%s (output suppressed)", code)
	}
	if !sandboxCode.MatchString(username) || !sandboxCode.MatchString(code) {
		return errors.New("group-member diagnostic identity invalid (output suppressed)")
	}
	if cleanup != "confirmed" && cleanup != "incomplete" && cleanup != "unknown" {
		cleanup = "unknown"
	}
	return fmt.Errorf("%s username=%s cleanup=%s", first, username, cleanup)
}
func runGroupMemberTrace(root string, credentials map[string]string, username, groupCode string) (result error) {
	if !sandboxCode.MatchString(username) || !sandboxCode.MatchString(groupCode) || username == groupCode {
		return errors.New("relation requires distinct generated identities")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return groupMemberDiagnostic(fmt.Errorf("group-member phase=terraform-cli code=%s (output suppressed)", groupCode), username, groupCode, "unknown")
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return groupMemberDiagnostic(fmt.Errorf("group-member phase=go-cli code=%s (output suppressed)", groupCode), username, groupCode, "unknown")
		}
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return groupMemberDiagnostic(fmt.Errorf("group-member phase=client code=%s (output suppressed)", groupCode), username, groupCode, "unknown")
	}
	group := client.GetGroup(&dto.GetGroupDto{Code: groupCode})
	if group == nil || group.StatusCode != 404 {
		return groupMemberDiagnostic(fmt.Errorf("group-member phase=preflight-group code=%s (output suppressed)", groupCode), username, groupCode, "unknown")
	}
	// No safe username-to-ID deletion authority exists before Terraform records it.
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return groupMemberDiagnostic(fmt.Errorf("group-member phase=workspace code=%s (output suppressed)", groupCode), username, groupCode, "unknown")
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return groupMemberDiagnostic(fmt.Errorf("group-member phase=provider-build code=%s (output suppressed)", groupCode), username, groupCode, "unknown")
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
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
resource "authing_group" "sandbox" {
 code = %q
 name = %q
 description = %q
 type = "static"
}
resource "authing_group_member" "sandbox" {
 group_code = authing_group.sandbox.code
 user_id = authing_user.sandbox.id
}
`, source, username, username, groupCode, groupCode, "hermesacc ownership "+groupCode)
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return groupMemberDiagnostic(fmt.Errorf("group-member phase=config code=%s (output suppressed)", groupCode), username, groupCode, "unknown")
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	id := ""
	defer func() {
		// No username-derived ID can authorize a cleanup write. A missing
		// state-backed ID is explicitly unknown, not an absent user.
		if id == "" {
			id, _ = userStateID(root, terraform, env, username)
		}
		if id == "" {
			result = groupMemberDiagnostic(result, username, groupCode, "unknown")
			return
		}
		// Normal destroy removes both parents; fallback only from verified ID.
		user := client.GetUser(&dto.GetUserDto{UserId: id})
		group := client.GetGroup(&dto.GetGroupDto{Code: groupCode})
		cleanup := "confirmed"
		switch {
		case user != nil && user.StatusCode == 404 && group != nil && group.StatusCode == 404:
		case user != nil && user.StatusCode == 200 && group != nil && group.StatusCode == 404:
			if cleanupUser(client, id, username) != nil {
				cleanup = "incomplete"
			}
		case user != nil && user.StatusCode == 200 && group != nil && group.StatusCode == 200:
			if cleanupGroupMember(client, id, username, groupCode) != nil {
				cleanup = "incomplete"
			}
		default:
			cleanup = "unknown"
		}
		if result != nil || cleanup != "confirmed" {
			result = groupMemberDiagnostic(result, username, groupCode, cleanup)
		}
	}()
	result = (traceCase{name: "group-member", code: groupCode, phases: []tracePhase{
		{"apply-user", run(0, "apply", "-target=authing_user.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"capture-user-id", func() error { var e error; id, e = userStateID(root, terraform, env, username); return e }},
		{"apply-group", run(0, "apply", "-target=authing_group.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-parents", func() error {
			if err := verifyOwnedUser(client, id, username); err != nil {
				return err
			}
			return verifyOwnedGroup(client, groupCode, groupCode, "hermesacc ownership "+groupCode)
		}},
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"capture-relation-state", func() error {
			var e error
			id, e = relationStateUserID(root, terraform, env, username, groupCode)
			return e
		}},
		{"plan-converged", plan(0)},
		{"verify-created", func() error {
			if err := verifyOwnedUser(client, id, username); err != nil {
				return err
			}
			if err := verifyOwnedGroup(client, groupCode, groupCode, "hermesacc ownership "+groupCode); err != nil {
				return err
			}
			return verifyMembership(client, id, groupCode, true)
		}},
		{"remote-revoke", func() error {
			res := client.RemoveGroupMembers(&dto.RemoveGroupMembersReqDto{Code: groupCode, UserIds: []string{id}})
			if res == nil || res.StatusCode != 200 || !res.Data.Success {
				return errors.New("revoke failed")
			}
			if err := verifyMembership(client, id, groupCode, false); err != nil {
				return err
			}
			return verifyOwnedUser(client, id, username)
		}},
		{"plan-drift", plan(2)},
		{"apply-restore", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-restored", func() error { return verifyMembership(client, id, groupCode, true) }},
		{"verify-owned-before-destroy", func() error {
			if err := verifyOwnedUser(client, id, username); err != nil {
				return err
			}
			if err := verifyOwnedGroup(client, groupCode, groupCode, "hermesacc ownership "+groupCode); err != nil {
				return err
			}
			return groupMemberInventory(client, groupCode, id, true)
		}},
		{"destroy-relation", run(0, "destroy", "-target=authing_group_member.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-relation-absent", func() error {
			if err := verifyMembership(client, id, groupCode, false); err != nil {
				return err
			}
			if err := verifyOwnedUser(client, id, username); err != nil {
				return err
			}
			if err := verifyOwnedGroup(client, groupCode, groupCode, "hermesacc ownership "+groupCode); err != nil {
				return err
			}
			return groupMemberInventory(client, groupCode, id, false)
		}},
		{"destroy-parents", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-parents-absent", func() error {
			got := client.GetUser(&dto.GetUserDto{UserId: id})
			if got == nil || got.StatusCode != 404 {
				return errors.New("user remains")
			}
			group := client.GetGroup(&dto.GetGroupDto{Code: groupCode})
			if group == nil || group.StatusCode != 404 {
				return errors.New("group remains")
			}
			return nil
		}},
	}}).execute()
	return result
}
