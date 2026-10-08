package acceptance

import (
	"crypto/rand"
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
		return errors.New("group tracer requires a generated hermesacc code")
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
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("group phase=go-toolchain code=%s (output suppressed)", code)
		}
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("group phase=client code=%s (output suppressed)", code)
	}
	name := "hermesacc-" + strings.TrimPrefix(code, "hermesacc-")
	marker := "hermesacc ownership " + code
	// Preflight refuses existing resources, including same-code resources left by earlier runs.
	current := client.GetGroup(&dto.GetGroupDto{Code: code})
	if current == nil || current.StatusCode != 404 {
		return fmt.Errorf("group phase=preflight code=%s (output suppressed)", code)
	}
	// Once apply starts it may create remotely yet exit nonzero. Always inspect and
	// conditionally clean up, including when creation never reached local state.
	started := false
	defer func() {
		if !started {
			return
		}
		if err := cleanupGroup(client, code, name, marker); err != nil {
			result = fmt.Errorf("group phase=cleanup-incomplete code=%s (output suppressed)", code)
		}
	}()
	providerDir := filepath.Join(root, "provider")
	exampleDir := filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("group phase=workspace code=%s (output suppressed)", code)
	}
	binary := filepath.Join(providerDir, "terraform-provider-authing")
	build := exec.Command(goBinary, "build", "-o", binary, ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("group phase=provider-build code=%s (output suppressed)", code)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	hcl := fmt.Sprintf(`terraform {
  required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_group" "sandbox" {
  code = %q
  name = %q
  description = %q
  type = "static"
}
`, source, code, name, marker)
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
	started = true
	result = (traceCase{name: "group", code: code, phases: []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-converged", plan(0)},
		{"remote-drift", func() error {
			res := client.UpdateGroup(&dto.UpdateGroupReqDto{Code: code, Name: name + "-drift", Description: marker})
			if res == nil || res.StatusCode != 200 || res.Data.Code != code || res.Data.Name != name+"-drift" {
				return errors.New("drift mutation failed")
			}
			got := client.GetGroup(&dto.GetGroupDto{Code: code})
			if got == nil || got.StatusCode != 200 || got.Data.Name != name+"-drift" {
				return errors.New("drift not visible")
			}
			return nil
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-owned-before-destroy", func() error { return verifyOwnedGroup(client, code, name, marker) }},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			got := client.GetGroup(&dto.GetGroupDto{Code: code})
			if got == nil || got.StatusCode != 404 {
				return errors.New("group still present")
			}
			return nil
		}},
	}}).execute()
	return result
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
