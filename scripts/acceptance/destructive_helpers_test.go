package acceptance

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"terraform-provider-authing/internal/authingapi"
)

// Test-only destructive sandbox harness. Never call from normal provider code.
var destructiveLive = flag.Bool("authing-destructive-sandbox", false, "opt in to sandbox group creation, drift, and deletion")
var sandboxCode = regexp.MustCompile(`^hermesacc-[0-9a-f]{16}$`)

func destructiveGuard(enabled bool, env map[string]string) error {
	if !enabled || env["AUTHING_ACCEPTANCE_CONFIRM"] != "DESTRUCTIVE_SANDBOX" || env["AUTHING_ACCESS_KEY_ID"] == "" || env["AUTHING_ACCESS_KEY_SECRET"] == "" {
		return errors.New("destructive sandbox requires explicit flag, exact confirmation, and both environment credentials")
	}
	return nil
}

func newGroupCode() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "hermesacc-" + hex.EncodeToString(b[:]), nil
}

// traceCase is the reusable case contract for future resource-family tracers.
// Each phase checks an observable result; errors intentionally discard subprocess/API bodies.
type traceCase struct {
	name, code string
	phases     []tracePhase
}
type tracePhase struct {
	name string
	run  func() error
}

func (c traceCase) execute() error {
	for _, p := range c.phases {
		if p.run() != nil {
			return fmt.Errorf("%s phase=%s code=%s (output suppressed)", c.name, p.name, c.code)
		}
	}
	return nil
}

func traceEnvironment(root, config string, credentials map[string]string) []string {
	clean := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "AUTHING_") && !strings.HasPrefix(entry, "TF_") && !strings.HasPrefix(entry, "CHECKPOINT_") && !strings.HasPrefix(entry, "GIT_ASKPASS=") {
			clean = append(clean, entry)
		}
	}
	clean = append(clean, "AUTHING_ACCESS_KEY_ID="+credentials["AUTHING_ACCESS_KEY_ID"], "AUTHING_ACCESS_KEY_SECRET="+credentials["AUTHING_ACCESS_KEY_SECRET"], "TF_CLI_CONFIG_FILE="+config, "TF_DATA_DIR="+filepath.Join(root, "data"), "TF_INPUT=0", "TF_IN_AUTOMATION=1", "TF_LOG=OFF", "CHECKPOINT_DISABLE=1", "HOME="+root)
	if host := credentials["AUTHING_HOST"]; host != "" {
		clean = append(clean, "AUTHING_HOST="+host)
	}
	return clean
}

func terraformExit(root string, env []string, terraform string, want int, args ...string) error {
	cmd := exec.Command(terraform, args...)
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	// Deliberately capture and discard diagnostics even on failure.
	_, err := cmd.CombinedOutput()
	exit := 0
	if err != nil {
		var e *exec.ExitError
		if !errors.As(err, &e) {
			return errors.New("Terraform invocation failed")
		}
		exit = e.ExitCode()
	}
	if exit != want {
		return errors.New("unexpected Terraform exit code")
	}
	return nil
}

func runGroupTrace(root string, credentials map[string]string, code string) (result error) {
	if !sandboxCode.MatchString(code) {
		return errors.New("group tracer requires generated code")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		return fmt.Errorf("group phase=fresh-workspace code=%s (output suppressed)", code)
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("group phase=terraform-cli code=%s (output suppressed)", code)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("group phase=client code=%s (output suppressed)", code)
	}
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("group phase=workspace code=%s (output suppressed)", code)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("group phase=provider-build code=%s (output suppressed)", code)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n dev_overrides {\n %q = %q\n }\n direct {}\n}\n", source, providerDir)
	makeHCL := func(suffix string) string {
		return fmt.Sprintf(`terraform {
 required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_group" "sandbox" {
 code = %q
 name = %q
 description = %q
 type = "static"
}
`, source, code, code+suffix, "hermesacc ownership "+code+suffix)
	}
	hcl := makeHCL("")
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("group phase=config code=%s (output suppressed)", code)
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	id := ""
	started := false
	defer func() {
		if !started {
			return
		}
		if id == "" {
			id, _ = groupStateID(root, terraform, env, code)
		}
		cleanup := "unknown"
		if id != "" {
			cleanup = "incomplete"
			if cleanupPinnedGroup(client, id, code) == nil {
				cleanup = "confirmed"
			}
		}
		if result == nil && cleanup != "confirmed" {
			result = fmt.Errorf("group phase=cleanup-incomplete code=%s (output suppressed)", code)
		}
		fingerprint := ""
		if id != "" {
			fingerprint = fmt.Sprintf(" state_id_sha256=%x", sha256.Sum256([]byte(id)))
		}
		if result != nil {
			result = fmt.Errorf("%w cleanup=%s%s", result, cleanup, fingerprint)
		} else {
			fmt.Printf("group phase=complete code=%s cleanup=confirmed%s\n", code, fingerprint)
		}
	}()
	verify := func(suffix string) error {
		got := client.GetGroup(&dto.GetGroupDto{Code: id})
		if got == nil || got.StatusCode != 200 || got.Data.Code != id || id != code || got.Data.Name != code+suffix || got.Data.Description != "hermesacc ownership "+code+suffix || got.Data.Type != "static" {
			return errors.New("configured fields readback unverified")
		}
		return nil
	}
	importRoot := filepath.Join(root, "import")
	importEnv := traceEnvironment(importRoot, config, credentials)
	phases := []tracePhase{
		{"preflight-absent", func() error {
			got := client.GetGroup(&dto.GetGroupDto{Code: code})
			if got == nil || got.StatusCode != 404 {
				return errors.New("absence unproved")
			}
			return nil
		}},
		{"apply-create", func() error {
			started = true
			return run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")()
		}},
		{"capture-id", func() error { var e error; id, e = groupStateID(root, terraform, env, code); return e }},
		{"verify-created", func() error { return verify("") }},
		{"plan-converged", plan(0)},
		{"configure-all-update", func() error {
			return os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(makeHCL("-updated")), 0600)
		}},
		{"plan-all-update", plan(2)},
		{"apply-all-update", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-all-update", func() error { return verify("-updated") }},
		{"plan-update-converged", plan(0)},
		{"configure-original", func() error { return os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) }},
		{"apply-original", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-original", func() error { return verify("") }},
		{"configure-empty-description", func() error {
			return os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(strings.ReplaceAll(hcl, fmt.Sprintf("description = %q", "hermesacc ownership "+code), `description = ""`)), 0600)
		}},
		{"apply-empty-description", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-empty-description", func() error {
			got := client.GetGroup(&dto.GetGroupDto{Code: id})
			if got == nil || got.StatusCode != 200 || got.Data.Code != id || got.Data.Description != "" || got.Data.Name != code || got.Data.Type != "static" {
				return errors.New("empty description not confirmed")
			}
			return nil
		}},
		{"restore-description", func() error {
			if e := os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600); e != nil {
				return e
			}
			return run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")()
		}},
		{"plan-code-replacement", func() error { return groupReplacementPlan(root, terraform, env, hcl, "code", code+"-replacement") }},
		{"plan-type-replacement", func() error { return groupReplacementPlan(root, terraform, env, hcl, "type", "replacement-plan-only") }},
		{"import-fresh-state", func() error {
			if os.MkdirAll(filepath.Join(importRoot, "example"), 0700) != nil || os.WriteFile(filepath.Join(importRoot, "example/main.tf"), []byte(hcl), 0600) != nil {
				return errors.New("fresh import workspace failed")
			}
			return terraformExit(importRoot, importEnv, terraform, 0, "import", "-input=false", "-no-color", "authing_group.sandbox", id)
		}},
		{"verify-imported-id", func() error {
			got, e := groupStateID(importRoot, terraform, importEnv, code)
			if e != nil || got != id {
				return errors.New("imported identity differs")
			}
			return verify("")
		}},
		{"plan-import-converged", func() error {
			return terraformExit(importRoot, importEnv, terraform, 0, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
		}},
		{"remote-drift", func() error {
			if e := verifyPinnedGroup(client, id, code); e != nil {
				return e
			}
			got := client.UpdateGroup(&dto.UpdateGroupReqDto{Code: id, Name: code + "-drift", Description: "hermesacc ownership " + code + "-drift"})
			if got == nil || got.StatusCode != 200 || got.Data.Code != id {
				return errors.New("drift mutation rejected")
			}
			return verify("-drift")
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-owned-before-destroy", func() error {
			if e := verify(""); e != nil {
				return e
			}
			return verifyPinnedGroup(client, id, code)
		}},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			got := client.GetGroup(&dto.GetGroupDto{Code: id})
			if got == nil || got.StatusCode != 404 {
				return errors.New("absence unconfirmed")
			}
			return nil
		}},
	}
	for _, phase := range phases {
		if phase.run() != nil {
			return fmt.Errorf("group phase=%s code=%s (output suppressed)", phase.name, code)
		}
		fmt.Printf("group phase=%s code=%s result=passed\n", phase.name, code)
	}
	return nil
}

// Only a known code AND matching ownership marker, type, and expected name may
// be deleted. Unknown responses are not interpreted as absence.
func verifyOwnedGroup(client *authingapi.Client, code, name, marker string) error {
	got := client.GetGroup(&dto.GetGroupDto{Code: code})
	if got == nil || got.StatusCode != 200 || got.Data.Code != code || got.Data.Description != marker || got.Data.Type != "static" || (got.Data.Name != name && got.Data.Name != name+"-drift") {
		return errors.New("ownership not verified")
	}
	return nil
}

func cleanupGroup(client *authingapi.Client, code, name, marker string) error {
	for attempt := 0; attempt < 3; attempt++ {
		got := client.GetGroup(&dto.GetGroupDto{Code: code})
		if got != nil && got.StatusCode == 404 {
			return nil
		}
		if got == nil || got.StatusCode != 200 || got.Data.Code != code || got.Data.Description != marker || got.Data.Type != "static" || (got.Data.Name != name && got.Data.Name != name+"-drift") {
			return errors.New("ownership not verified")
		}
		if err := emptyGroupMembers(client, code); err != nil {
			return err
		}
		deleted := client.DeleteGroupsBatch(&dto.DeleteGroupsReqDto{CodeList: []string{code}})
		if deleted != nil && deleted.StatusCode != 200 && deleted.StatusCode != 404 {
			return errors.New("delete rejected")
		}
		if attempt < 2 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	got := client.GetGroup(&dto.GetGroupDto{Code: code})
	if got != nil && got.StatusCode == 404 {
		return nil
	}
	return errors.New("group not confirmed absent")
}
