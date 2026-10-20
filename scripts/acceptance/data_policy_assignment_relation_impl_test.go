package acceptance

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Authing/authing-golang-sdk/v3/dto"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"terraform-provider-authing/internal/authingapi"
	"testing"
)

func runDataPolicyAssignmentTrace(root string, creds map[string]string, code, username string) (result error) {
	if !sandboxCode.MatchString(code) || !sandboxCode.MatchString(username) {
		return errors.New("invalid generated identities")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	tf := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, e := os.Stat(tf); e != nil {
		return errors.New("Terraform CLI unavailable")
	}
	goBin, e := exec.LookPath("go")
	if e != nil {
		goBin = filepath.Join(repo, "../.tools/go/bin/go")
		if _, e = os.Stat(goBin); e != nil {
			return errors.New("Go unavailable")
		}
	}
	c, e := authingapi.NewClient(authingapi.Options{AccessKeyID: creds["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: creds["AUTHING_ACCESS_KEY_SECRET"], Host: creds["AUTHING_HOST"]})
	if e != nil {
		return errors.New("client unavailable")
	}
	if n := c.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code}); n == nil || n.StatusCode != 404 {
		return errors.New("namespace preflight failed")
	}
	if found, _, e := sandboxDataResource(c, code); e != nil || found {
		return errors.New("resource preflight failed")
	}
	if e := policyNameAvailable(c, policyName(code)); e != nil {
		return errors.New("policy preflight failed")
	}
	providerDir, example := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(example, 0700) != nil {
		return errors.New("workspace unavailable")
	}
	build := exec.Command(goBin, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, e = build.CombinedOutput(); e != nil {
		return errors.New("provider build failed")
	}
	hcl := fmt.Sprintf(`terraform {
 required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_namespace" "sandbox" {
 code = %q
 name = %q
 description = %q
}
resource "authing_data_resource" "sandbox" {
 namespace_code = authing_namespace.sandbox.code
 resource_code = %q
 resource_name = %q
 type = "ARRAY"
 struct = jsonencode([])
 actions = ["read"]
 description = %q
}
resource "authing_data_policy" "sandbox" {
 policy_name = %q
 description = %q
 statement_list = [{ effect = "DENY", permissions = ["${authing_data_resource.sandbox.namespace_code}/${authing_data_resource.sandbox.resource_code}/read"] }]
}
resource "authing_user" "sandbox" {
 username = %q
 nickname = %q
}
resource "authing_data_policy_assignment" "sandbox" {
 policy_id = authing_data_policy.sandbox.id
 target_type = "USER"
 target_id = authing_user.sandbox.id
}
`, source, code, code, "hermesacc ownership "+code, dataResourceCode(code), dataResourceCode(code), dataResourceMarker(code), policyName(code), policyMarker(code), username, username)
	rcPath := filepath.Join(root, "terraform.rc")
	if os.WriteFile(rcPath, []byte(fmt.Sprintf("provider_installation {\n dev_overrides { %q = %q }\n direct {}\n}\n", source, providerDir)), 0600) != nil || os.WriteFile(filepath.Join(example, "main.tf"), []byte(hcl), 0600) != nil {
		return errors.New("config unavailable")
	}
	env := traceEnvironment(root, rcPath, creds)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, tf, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	user, id := "", ""
	defer func() {
		if user == "" {
			user, _ = userStateID(root, tf, env, username)
		}
		if id == "" {
			id = policyStateID(root)
		}
		if user == "" || id == "" {
			if result != nil {
				result = fmt.Errorf("%v; cleanup=manual-review (missing state-backed identity)", result)
			}
			return
		}
		x := c.GetUser(&dto.GetUserDto{UserId: user})
		if x == nil || x.StatusCode != 200 && x.StatusCode != 404 {
			result = fmt.Errorf("%v; cleanup=incomplete (user identity)", result)
			return
		}
		if x.StatusCode == 200 {
			if e := verifyOwnedUser(c, user, username); e != nil {
				result = fmt.Errorf("%v; cleanup=incomplete (user identity)", result)
				return
			}
		}
		exists, _, e := ownedPolicy(c, code, id)
		if e != nil {
			result = fmt.Errorf("%v; cleanup=incomplete (policy identity)", result)
			return
		}
		if exists {
			present, e := exactPolicyTarget(c, id, "USER", user)
			if e != nil {
				result = fmt.Errorf("%v; cleanup=incomplete (target scope)", result)
				return
			}
			if present {
				raw, e := c.SendHttpRequest("/api/v3/revoke-data-policy", http.MethodPost, map[string]string{"policyId": id, "targetIdentifier": user, "targetType": "USER"})
				if e != nil || !successEnvelope(raw) {
					result = fmt.Errorf("%v; cleanup=incomplete (revoke)", result)
					return
				}
			}
			if e := removeOwnedPolicy(c, code, id); e != nil {
				result = fmt.Errorf("%v; cleanup=incomplete (policy)", result)
				return
			}
		}
		if e := cleanupUser(c, user, username); e != nil {
			result = fmt.Errorf("%v; cleanup=incomplete (user)", result)
			return
		}
		// Other prerequisites may remain after a failed Terraform apply: never guess
		// deletion from names without state-backed identities and full inventories.
		n := c.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code})
		if n == nil || n.StatusCode != 404 {
			result = fmt.Errorf("%v; cleanup=manual-review (namespace/resource retained)", result)
		}
	}()
	verify := func(want bool) error {
		got, e := exactPolicyTarget(c, id, "USER", user)
		if e != nil || got != want {
			return errors.New("scoped target not verified")
		}
		return nil
	}
	result = (traceCase{name: "data-policy-assignment", code: code, phases: []tracePhase{
		{"apply-user", run(0, "apply", "-target=authing_user.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"capture-user", func() error { var e error; user, e = userStateID(root, tf, env, username); return e }},
		{"apply-policy", run(0, "apply", "-target=authing_data_policy.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"capture-policy", func() error {
			id = policyStateID(root)
			if id == "" {
				return errors.New("policy state ID missing")
			}
			exists, _, e := ownedPolicy(c, code, id)
			if e != nil || !exists {
				return errors.New("owned policy unavailable")
			}
			return emptyPolicyTargets(c, id)
		}},
		{"apply-relation", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-relation-state", func() error {
			return verifyRelationState(root, tf, env, "authing_data_policy_assignment.sandbox", assignmentTraceID(id, user), map[string]string{"policy_id": id, "target_type": "USER", "target_id": user})
		}},
		{"verify-attached", func() error { return verify(true) }},
		{"plan-converged", plan(0)},
		{"remote-revoke", func() error {
			raw, e := c.SendHttpRequest("/api/v3/revoke-data-policy", http.MethodPost, map[string]string{"policyId": id, "targetIdentifier": user, "targetType": "USER"})
			if e != nil || !successEnvelope(raw) {
				return errors.New("remote revoke failed")
			}
			return verify(false)
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-restored", func() error { return verify(true) }},
		{"destroy-relation", run(0, "destroy", "-target=authing_data_policy_assignment.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-detached", func() error {
			if e := verify(false); e != nil {
				return e
			}
			if e := emptyPolicyTargets(c, id); e != nil {
				return e
			}
			if e := verifyOwnedUser(c, user, username); e != nil {
				return e
			}
			exists, _, e := ownedPolicy(c, code, id)
			if e != nil || !exists {
				return errors.New("policy identity changed")
			}
			return nil
		}},
		{"destroy-policy", run(0, "destroy", "-target=authing_data_policy.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-policy-absent", func() error {
			exists, _, e := ownedPolicy(c, code, id)
			if e != nil || exists {
				return errors.New("policy absence unverified")
			}
			return policyPrerequisitesSafe(c, code)
		}},
		{"destroy-parents", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absence", func() error {
			if x := c.GetUser(&dto.GetUserDto{UserId: user}); x == nil || x.StatusCode != 404 {
				return errors.New("user absence unverified")
			}
			if x := c.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code}); x == nil || x.StatusCode != 404 {
				return errors.New("namespace absence unverified")
			}
			if found, _, e := sandboxDataResource(c, code); e != nil || found {
				return errors.New("resource absence unverified")
			}
			return nil
		}},
	}}).execute()
	return result
}
func successEnvelope(raw []byte) bool {
	var v struct {
		StatusCode int `json:"statusCode"`
		Data       struct {
			Success *bool `json:"success"`
		} `json:"data"`
	}
	return json.Unmarshal(raw, &v) == nil && v.StatusCode == 200 && (v.Data.Success == nil || *v.Data.Success)
}
func TestDestructiveLiveDataPolicyAssignmentTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires destructive sandbox opt-in")
	}
	if os.Getenv("AUTHING_DATA_POLICY_STATEMENTS_READBACK_CONFIRMED") != "COMPLETE" {
		t.Skip("policy statement readback not confirmed")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if e := destructiveGuard(*destructiveLive, env); e != nil {
		t.Fatal(e)
	}
	code, e := newNamespaceCode()
	if e != nil {
		t.Fatal("identity generation failed")
	}
	user, e := newGroupCode()
	if e != nil || user == code {
		t.Fatal("identity generation failed")
	}
	if e := runDataPolicyAssignmentTrace(t.TempDir(), env, code, user); e != nil {
		t.Fatal(e)
	}
}
