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

// The invitee endpoint has no GET: an exhaustive roster-scoped list is its readback.
type tracedInvitee struct{ ID, RosterID, Name, Email, Phone string }

func invitationInvitees(client *authingapi.Client, roster string) ([]tracedInvitee, error) {
	if roster == "" {
		return nil, errors.New("missing roster ID")
	}
	rows := []tracedInvitee{}
	ids := map[string]bool{}
	total := -1
	for page := 1; page <= 1000; page++ {
		out, err := invitationRequest(client, "/api/v3/list-invitation-invitees", http.MethodPost, map[string]any{"rosterId": roster, "page": page, "limit": 50})
		if err != nil || out.StatusCode != 200 {
			return nil, errors.New("invitee listing failed")
		}
		var data struct {
			Total *int `json:"totalCount"`
			List  *[]struct {
				ID     string `json:"inviteeId"`
				Roster string `json:"rosterId"`
				Name   string `json:"name"`
				Email  string `json:"email"`
				Phone  string `json:"phone"`
			} `json:"list"`
		}
		if json.Unmarshal(out.Data, &data) != nil || data.Total == nil || data.List == nil || *data.Total < 0 || len(*data.List) > 50 || len(rows)+len(*data.List) > *data.Total || total >= 0 && total != *data.Total {
			return nil, errors.New("invalid invitee listing")
		}
		total = *data.Total
		for _, row := range *data.List {
			if row.ID == "" || row.Roster != roster || row.Name == "" || row.Email == "" || ids[row.ID] {
				return nil, errors.New("invalid invitee row")
			}
			ids[row.ID] = true
			rows = append(rows, tracedInvitee{row.ID, row.Roster, row.Name, row.Email, row.Phone})
		}
		if len(rows) == total {
			return rows, nil
		}
		if len(*data.List) != 50 {
			return nil, errors.New("incomplete invitee listing")
		}
	}
	return nil, errors.New("invitee page limit exceeded")
}
func ownedInvitee(client *authingapi.Client, roster, id, name, email string) error {
	if roster == "" || id == "" || !sandboxCode.MatchString(name) || email != name+"@example.invalid" {
		return errors.New("invalid invitee ownership")
	}
	rows, err := invitationInvitees(client, roster)
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return errors.New("foreign invitees in roster")
	}
	matches := 0
	for _, row := range rows {
		if row.ID == id {
			if row.Name != name && row.Name != name+"-drift" || row.Email != email || row.Phone != "" {
				return errors.New("invitee identity mismatch")
			}
			matches++
		} else if strings.EqualFold(row.Email, email) {
			return errors.New("duplicate invitee email")
		}
	}
	if matches != 1 {
		return errors.New("owned invitee not uniquely listed")
	}
	return nil
}
func absentInvitee(client *authingapi.Client, roster, id, email string) error {
	rows, err := invitationInvitees(client, roster)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.ID == id || strings.EqualFold(row.Email, email) {
			return errors.New("invitee still present")
		}
	}
	return nil
}
func cleanupInvitationInvitee(client *authingapi.Client, roster, id, name, email string) error {
	if err := ownedInvitee(client, roster, id, name, email); err != nil {
		return err
	}
	out, err := invitationRequest(client, "/api/v3/batch-delete-invitation-invitees", http.MethodPost, map[string]any{"rosterId": roster, "inviteeIds": []string{id}})
	if err != nil || out.StatusCode != 200 {
		return errors.New("invitee deletion failed")
	}
	var data struct {
		Success *bool `json:"success"`
	}
	if json.Unmarshal(out.Data, &data) != nil || data.Success == nil || !*data.Success {
		return errors.New("invitee deletion not confirmed")
	}
	return absentInvitee(client, roster, id, email)
}

// Only the state-backed exact ID authorizes routine deletion. On failed create,
// an absent-at-preflight unique synthetic email may recover an otherwise orphaned row.
func inviteeStateID(root string, env []string, terraform, roster, name string) (string, error) {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, err := cmd.Output()
	if err != nil {
		return "", errors.New("state inspection failed")
	}
	var state struct {
		Values struct {
			Root struct {
				Resources []struct {
					Address string `json:"address"`
					Values  struct {
						ID      string `json:"id"`
						Roster  string `json:"roster_id"`
						Invitee string `json:"invitee_id"`
						Name    string `json:"name"`
						Email   string `json:"email"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return "", errors.New("invalid state")
	}
	for _, r := range state.Values.Root.Resources {
		if r.Address == "authing_invitation_invitee.sandbox" {
			if r.Values.Roster != roster || r.Values.Name != name || r.Values.Email != name+"@example.invalid" || r.Values.Invitee == "" || r.Values.ID != invitationCompositeID(roster, r.Values.Invitee) {
				return "", errors.New("invitee state identity mismatch")
			}
			return r.Values.Invitee, nil
		}
	}
	return "", errors.New("invitee ID absent from state")
}
func invitationCompositeID(roster, id string) string {
	b, _ := json.Marshal([2]string{roster, id})
	return "ii1." + base64.RawURLEncoding.EncodeToString(b)
}

func runInvitationInviteeTrace(root string, credentials map[string]string, name string) (result error) {
	if !sandboxCode.MatchString(name) {
		return errors.New("invitee tracer requires generated name")
	}
	email := name + "@example.invalid"
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("invitation-invitee phase=terraform-cli code=%s (output suppressed)", name)
	}
	goBinary, goErr := exec.LookPath("go")
	if goErr != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
	}
	if _, err := os.Stat(goBinary); err != nil {
		return fmt.Errorf("invitation-invitee phase=go-toolchain code=%s (output suppressed)", name)
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("invitation-invitee phase=client code=%s (output suppressed)", name)
	}
	if id, e := findInvitationPolicy(client, name); e != nil || id != "" {
		return fmt.Errorf("invitation-invitee phase=preflight-policy code=%s (output suppressed)", name)
	}
	if id, e := findInvitationRoster(client, name); e != nil || id != "" {
		return fmt.Errorf("invitation-invitee phase=preflight-roster code=%s (output suppressed)", name)
	}
	started := false
	policyID, rosterID, inviteeID := "", "", ""
	defer func() {
		if !started {
			return
		}
		failed := false
		// Never infer absence from a failed listing. Never delete a foreign row.
		discovered, e := findInvitationRoster(client, name)
		if e != nil || rosterID != "" && discovered != "" && rosterID != discovered {
			failed = true
		} else if discovered != "" {
			rosterID = discovered
		}
		discoveredPolicy, e := findInvitationPolicy(client, name)
		if e != nil || policyID != "" && discoveredPolicy != "" && policyID != discoveredPolicy {
			failed = true
		} else if discoveredPolicy != "" {
			policyID = discoveredPolicy
		}
		if !failed && rosterID != "" && discovered != "" {
			if e = ownedInvitationRoster(client, rosterID, name, policyID); e != nil {
				failed = true
			}
			if !failed {
				rows, readErr := invitationInvitees(client, rosterID)
				if readErr != nil {
					failed = true
				} else {
					for _, row := range rows {
						if strings.EqualFold(row.Email, email) {
							if inviteeID != "" && inviteeID != row.ID || inviteeID == "" && (row.Name != name && row.Name != name+"-drift" || row.Phone != "") {
								failed = true
							} else {
								inviteeID = row.ID
							}
						}
					}
					if !failed && inviteeID != "" && cleanupInvitationInvitee(client, rosterID, inviteeID, name, email) != nil {
						failed = true
					}
				}
			}
			if !failed && cleanupInvitationRoster(client, rosterID, name, policyID) != nil {
				failed = true
			}
		}
		if !failed && policyID != "" && cleanupInvitationPolicy(client, policyID, name) != nil {
			failed = true
		}
		if failed || result != nil && (discovered == "" && rosterID != "" || discoveredPolicy == "" && policyID != "") {
			if result == nil {
				result = fmt.Errorf("invitation-invitee phase=cleanup code=%s (output suppressed)", name)
			}
			result = fmt.Errorf("%w; cleanup=incomplete", result)
		}
	}()
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("invitation-invitee phase=workspace code=%s (output suppressed)", name)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err = build.CombinedOutput(); err != nil {
		return fmt.Errorf("invitation-invitee phase=provider-build code=%s (output suppressed)", name)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	hcl := func(inviteeName string) string {
		return fmt.Sprintf(`terraform {
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
resource "authing_invitation_invitee" "sandbox" {
 roster_id = authing_invitation_roster.sandbox.id
 name = %q
 email = %q
}
`, source, name, name, inviteeName, email)
	}
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl(name)), 0600) != nil {
		return fmt.Errorf("invitation-invitee phase=config code=%s (output suppressed)", name)
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	verify := func(expected string) error {
		if e := ownedInvitationPolicy(client, policyID, name); e != nil {
			return e
		}
		got, assigned, found, e := invitationRosterDetail(client, rosterID)
		if e != nil || !found || got != name || assigned != policyID {
			return errors.New("roster association not verified")
		}
		if listed, e := findInvitationRoster(client, name); e != nil || listed != rosterID {
			return errors.New("roster listing not verified")
		}
		if listed, e := findInvitationPolicy(client, name); e != nil || listed != policyID {
			return errors.New("policy listing not verified")
		}
		if e := ownedInvitee(client, rosterID, inviteeID, name, email); e != nil {
			return e
		}
		rows, e := invitationInvitees(client, rosterID)
		if e != nil || len(rows) != 1 || rows[0].ID != inviteeID || rows[0].Name != expected {
			return errors.New("invitee name or exclusive inventory not verified")
		}
		return nil
	}
	started = true
	return (traceCase{name: "invitation-invitee", code: name, phases: []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-state-id", func() error {
			var e error
			policyID, rosterID, e = invitationRosterStateIDs(root, env, terraform, name)
			if e != nil {
				return e
			}
			inviteeID, e = inviteeStateID(root, env, terraform, rosterID, name)
			if e != nil {
				return e
			}
			return verify(name)
		}},
		{"plan-converged", plan(0)},
		{"mutable-name-update", func() error {
			return os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl(name+"-drift")), 0600)
		}},
		{"plan-name-update", plan(2)},
		{"apply-name-update", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-name-update", func() error { return verify(name + "-drift") }},
		{"plan-updated", plan(0)},
		{"restore-config", func() error { return os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl(name)), 0600) }},
		{"apply-restore", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-restored", plan(0)},
		{"remote-drift", func() error {
			if e := verify(name); e != nil {
				return e
			}
			out, e := invitationRequest(client, "/api/v3/edit-invitation-invitee", http.MethodPost, map[string]any{"rosterId": rosterID, "inviteeId": inviteeID, "name": name + "-drift", "email": email, "phone": ""})
			if e != nil || out.StatusCode != 200 {
				return errors.New("remote invitee edit failed")
			}
			return verify(name + "-drift")
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-before-destroy", func() error { return verify(name) }},
		{"destroy-invitee", run(0, "destroy", "-target=authing_invitation_invitee.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-invitee-absent", func() error {
			if e := absentInvitee(client, rosterID, inviteeID, email); e != nil {
				return e
			}
			return noInvitationInvitees(client, rosterID)
		}},
		{"destroy-parents", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			if listed, e := findInvitationRoster(client, name); e != nil || listed != "" {
				return errors.New("roster listing absence not confirmed")
			}
			if listed, e := findInvitationPolicy(client, name); e != nil || listed != "" {
				return errors.New("policy listing absence not confirmed")
			}
			_, _, found, e := invitationRosterDetail(client, rosterID)
			if e != nil || found {
				return errors.New("roster GET absence not confirmed")
			}
			_, found, e = invitationPolicyDetail(client, policyID)
			if e != nil || found {
				return errors.New("policy GET absence not confirmed")
			}
			return nil
		}},
	}}).execute()
}
