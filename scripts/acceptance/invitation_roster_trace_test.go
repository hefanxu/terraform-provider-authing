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
	"terraform-provider-authing/internal/authingapi"
)

// Search the full result set even when the API's keyword search is fuzzy.
// A missing, duplicated or incomplete result never establishes ownership.
func findInvitationRoster(client *authingapi.Client, name string) (string, error) {
	var match string
	seen := 0
	for page := 1; page <= 1000; page++ {
		out, err := invitationRequest(client, "/api/v3/list-invitation-rosters", http.MethodPost, map[string]any{"page": page, "limit": 50, "keywords": name, "withRosterSecret": false})
		if err != nil || out.StatusCode != 200 {
			return "", errors.New("roster listing failed")
		}
		var data struct {
			TotalCount *int `json:"totalCount"`
			List       []struct {
				ID   string `json:"rosterId"`
				Name string `json:"name"`
			} `json:"list"`
		}
		if json.Unmarshal(out.Data, &data) != nil || data.TotalCount == nil || *data.TotalCount < 0 || data.List == nil || len(data.List) > 50 || seen+len(data.List) > *data.TotalCount {
			return "", errors.New("invalid roster listing")
		}
		for _, row := range data.List {
			if row.ID == "" || row.Name == "" {
				return "", errors.New("invalid roster row")
			}
			if row.Name == name || row.Name == name+"-drift" {
				if match != "" {
					return "", errors.New("ambiguous roster name")
				}
				match = row.ID
			}
		}
		seen += len(data.List)
		if seen == *data.TotalCount {
			return match, nil
		}
		if len(data.List) == 0 {
			return "", errors.New("incomplete roster listing")
		}
	}
	return "", errors.New("roster listing exceeded page limit")
}

// Listing is roster-scoped and unfiltered: require all pages and zero entries
// before deleting, including during best-effort cleanup after a failed apply.
func noInvitationInvitees(client *authingapi.Client, id string) error {
	if id == "" {
		return errors.New("missing roster ID")
	}
	seen := 0
	for page := 1; page <= 1000; page++ {
		out, err := invitationRequest(client, "/api/v3/list-invitation-invitees", http.MethodPost, map[string]any{"rosterId": id, "page": page, "limit": 50})
		if err != nil || out.StatusCode != 200 {
			return errors.New("invitee listing failed")
		}
		var data struct {
			TotalCount *int `json:"totalCount"`
			List       []struct {
				ID       string `json:"inviteeId"`
				RosterID string `json:"rosterId"`
			} `json:"list"`
		}
		if json.Unmarshal(out.Data, &data) != nil || data.TotalCount == nil || *data.TotalCount < 0 || data.List == nil || len(data.List) > 50 || seen+len(data.List) > *data.TotalCount {
			return errors.New("invalid invitee listing")
		}
		for _, row := range data.List {
			if row.ID == "" || row.RosterID != id {
				return errors.New("unverified invitee inventory")
			}
		}
		seen += len(data.List)
		if seen == *data.TotalCount {
			if seen != 0 {
				return errors.New("roster has assigned invitees")
			}
			return nil
		}
		if len(data.List) == 0 {
			return errors.New("incomplete invitee listing")
		}
	}
	return errors.New("invitee listing exceeded page limit")
}

func invitationRosterDetail(client *authingapi.Client, id string) (string, string, bool, error) {
	if id == "" {
		return "", "", false, errors.New("missing roster ID")
	}
	out, err := invitationRequest(client, "/api/v3/get-invitation-roster", http.MethodGet, map[string]any{"rosterId": id, "withAssignedPolicy": true})
	if err != nil {
		return "", "", false, err
	}
	if out.StatusCode == 404 {
		return "", "", false, nil
	}
	if out.StatusCode != 200 {
		return "", "", false, errors.New("roster GET failed")
	}
	var data struct {
		ID             string  `json:"rosterId"`
		Name           string  `json:"name"`
		PolicyID       *string `json:"policyId"`
		AssignedPolicy *struct {
			ID string `json:"policyId"`
		} `json:"assignedPolicy"`
	}
	if json.Unmarshal(out.Data, &data) != nil || data.ID != id || data.Name == "" {
		return "", "", false, errors.New("roster identity not verified")
	}
	if data.PolicyID != nil && data.AssignedPolicy != nil && *data.PolicyID != data.AssignedPolicy.ID {
		return "", "", false, errors.New("conflicting policy IDs")
	}
	policy := ""
	if data.PolicyID != nil {
		policy = *data.PolicyID
	} else if data.AssignedPolicy != nil {
		policy = data.AssignedPolicy.ID
	}
	return data.Name, policy, true, nil
}
func ownedInvitationRoster(client *authingapi.Client, id, name, policyID string) error {
	if !sandboxCode.MatchString(name) || id == "" || policyID == "" {
		return errors.New("invalid generated roster ownership")
	}
	got, assigned, found, err := invitationRosterDetail(client, id)
	if err != nil || !found || (got != name && got != name+"-drift") || (assigned != "" && assigned != policyID) {
		return errors.New("roster ownership not verified")
	}
	return nil
}
func cleanupInvitationRoster(client *authingapi.Client, id, name, policyID string) error {
	got, _, found, err := invitationRosterDetail(client, id)
	if err == nil && !found && sandboxCode.MatchString(name) {
		return nil
	}
	if err != nil || !found || (got != name && got != name+"-drift") {
		return errors.New("roster ownership not verified")
	}
	if err = ownedInvitationRoster(client, id, name, policyID); err != nil {
		return err
	}
	listed, err := findInvitationRoster(client, name)
	if err != nil || listed != id {
		return errors.New("roster listing does not confirm owned ID")
	}
	if err = noInvitationInvitees(client, id); err != nil {
		return err
	}
	out, err := invitationRequest(client, "/api/v3/delete-invitation-roster", http.MethodPost, map[string]any{"id": id})
	if err != nil || out.StatusCode != 200 {
		return errors.New("roster deletion rejected")
	}
	var data struct {
		Success *bool `json:"success"`
	}
	if json.Unmarshal(out.Data, &data) != nil || data.Success == nil || !*data.Success {
		return errors.New("roster deletion not confirmed")
	}
	_, _, found, err = invitationRosterDetail(client, id)
	if err != nil || found {
		return errors.New("roster GET absence not confirmed")
	}
	return nil
}

func invitationRosterStateIDs(root string, env []string, terraform, name string) (string, string, error) {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, err := cmd.Output()
	if err != nil {
		return "", "", errors.New("state inspection failed")
	}
	var state struct {
		Values struct {
			RootModule struct {
				Resources []struct {
					Address string `json:"address"`
					Values  struct {
						ID       string `json:"id"`
						Name     string `json:"name"`
						PolicyID string `json:"policy_id"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return "", "", errors.New("invalid state")
	}
	policy, roster := "", ""
	for _, r := range state.Values.RootModule.Resources {
		if r.Values.Name != name || r.Values.ID == "" {
			continue
		}
		switch r.Address {
		case "authing_invitation_policy.sandbox":
			policy = r.Values.ID
		case "authing_invitation_roster.sandbox":
			roster = r.Values.ID
		}
	}
	if policy == "" || roster == "" {
		return "", "", errors.New("both IDs absent from state")
	}
	for _, r := range state.Values.RootModule.Resources {
		if r.Address == "authing_invitation_roster.sandbox" && r.Values.PolicyID != policy {
			return "", "", errors.New("state association mismatch")
		}
	}
	return policy, roster, nil
}

func runInvitationRosterTrace(root string, credentials map[string]string, name string) (result error) {
	if !sandboxCode.MatchString(name) {
		return errors.New("roster tracer requires generated hermesacc name")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("invitation-roster phase=terraform-cli code=%s (output suppressed)", name)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("invitation-roster phase=go-toolchain code=%s (output suppressed)", name)
		}
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("invitation-roster phase=client code=%s (output suppressed)", name)
	}
	existing, err := findInvitationPolicy(client, name)
	if err != nil || existing != "" {
		return fmt.Errorf("invitation-roster phase=preflight-policy code=%s (output suppressed)", name)
	}
	existing, err = findInvitationRoster(client, name)
	if err != nil || existing != "" {
		return fmt.Errorf("invitation-roster phase=preflight-roster code=%s (output suppressed)", name)
	}
	started := false
	policyID, rosterID := "", ""
	defer func() {
		if !started {
			return
		}
		discovered, e := findInvitationRoster(client, name)
		if e != nil || (rosterID != "" && discovered != "" && discovered != rosterID) {
			result = fmt.Errorf("invitation-roster phase=cleanup-incomplete code=%s (output suppressed)", name)
			return
		}
		if discovered != "" {
			rosterID = discovered
		}
		discoveredPolicy, e := findInvitationPolicy(client, name)
		if e != nil || (policyID != "" && discoveredPolicy != "" && discoveredPolicy != policyID) {
			result = fmt.Errorf("invitation-roster phase=cleanup-incomplete code=%s (output suppressed)", name)
			return
		}
		if discoveredPolicy != "" {
			policyID = discoveredPolicy
		}
		if rosterID != "" {
			if e = cleanupInvitationRoster(client, rosterID, name, policyID); e != nil {
				result = fmt.Errorf("invitation-roster phase=cleanup-incomplete code=%s roster_id=%s (output suppressed)", name, rosterID)
				return
			}
		} else if result != nil { // A failed create with no state requires an unambiguous listing.
			if discovered != "" {
				result = fmt.Errorf("invitation-roster phase=cleanup-incomplete code=%s (output suppressed)", name)
				return
			}
		}
		if policyID != "" {
			if e = cleanupInvitationPolicy(client, policyID, name); e != nil {
				result = fmt.Errorf("invitation-roster phase=cleanup-incomplete code=%s policy_id=%s (output suppressed)", name, policyID)
			}
		} else if result != nil {
			result = fmt.Errorf("invitation-roster phase=cleanup-incomplete code=%s (output suppressed)", name)
		}
	}()
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("invitation-roster phase=workspace code=%s (output suppressed)", name)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err = build.CombinedOutput(); err != nil {
		return fmt.Errorf("invitation-roster phase=provider-build code=%s (output suppressed)", name)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	hcl := fmt.Sprintf(`terraform {
 required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_invitation_policy" "sandbox" {
 name = %q
 enabled_identifier_verify = false
 enabled_info_fill = false
}
resource "authing_invitation_roster" "sandbox" {
 name = %q
 policy_id = authing_invitation_policy.sandbox.id
}
`, source, name, name)
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("invitation-roster phase=config code=%s (output suppressed)", name)
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	started = true
	return (traceCase{name: "invitation-roster", code: name, phases: []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-id-and-assignment", func() error {
			var e error
			policyID, rosterID, e = invitationRosterStateIDs(root, env, terraform, name)
			if e != nil {
				return e
			}
			p, e := findInvitationPolicy(client, name)
			if e != nil || p != policyID {
				return errors.New("policy ID not uniquely listed")
			}
			r, e := findInvitationRoster(client, name)
			if e != nil || r != rosterID {
				return errors.New("roster ID not uniquely listed")
			}
			if e = ownedInvitationPolicy(client, policyID, name); e != nil {
				return e
			}
			got, assigned, found, e := invitationRosterDetail(client, rosterID)
			if e != nil || !found || got != name || assigned != policyID {
				return errors.New("created roster association not confirmed")
			}
			return noInvitationInvitees(client, rosterID)
		}},
		{"plan-converged", plan(0)},
		{"mutable-name-update", func() error {
			updated := fmt.Sprintf(`terraform {
 required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_invitation_policy" "sandbox" {
 name = %q
 enabled_identifier_verify = false
 enabled_info_fill = false
}
resource "authing_invitation_roster" "sandbox" {
 name = %q
 policy_id = authing_invitation_policy.sandbox.id
}
`, source, name, name+"-drift")
			return os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(updated), 0600)
		}},
		{"plan-name-update", plan(2)},
		{"apply-name-update", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-name-update", func() error {
			got, assigned, found, e := invitationRosterDetail(client, rosterID)
			if e != nil || !found || got != name+"-drift" || assigned != policyID {
				return errors.New("mutable name update not confirmed")
			}
			return nil
		}},
		{"plan-updated", plan(0)},
		{"restore-config", func() error { return os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) }},
		{"apply-restore", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-restored", plan(0)},
		{"remote-drift", func() error {
			if e := ownedInvitationRoster(client, rosterID, name, policyID); e != nil {
				return e
			}
			out, e := invitationRequest(client, "/api/v3/update-invitation-roster", http.MethodPost, map[string]any{"rosterId": rosterID, "name": name + "-drift"})
			if e != nil || out.StatusCode != 200 {
				return errors.New("roster drift mutation failed")
			}
			got, assigned, found, e := invitationRosterDetail(client, rosterID)
			if e != nil || !found || got != name+"-drift" || assigned != policyID {
				return errors.New("roster drift not visible")
			}
			return nil
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-before-destroy", func() error {
			if e := ownedInvitationPolicy(client, policyID, name); e != nil {
				return e
			}
			got, assigned, found, e := invitationRosterDetail(client, rosterID)
			if e != nil || !found || got != name || assigned != policyID {
				return errors.New("roster not reconciled")
			}
			r, e := findInvitationRoster(client, name)
			if e != nil || r != rosterID {
				return errors.New("roster ID not uniquely owned")
			}
			return noInvitationInvitees(client, rosterID)
		}},
		// Dependency edge causes Terraform to delete the roster before its policy.
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			_, _, found, e := invitationRosterDetail(client, rosterID)
			if e != nil || found {
				return errors.New("roster absence not confirmed")
			}
			_, found, e = invitationPolicyDetail(client, policyID)
			if e != nil || found {
				return errors.New("policy absence not confirmed")
			}
			return nil
		}},
	}}).execute()
}
