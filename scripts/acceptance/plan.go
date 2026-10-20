// Package acceptance holds the opt-in, read-only Terraform sandbox smoke test.
package acceptance

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const source = "registry.terraform.io/hefanxu/authing"

func guard(enabled bool, env map[string]string) error {
	if !enabled || env["AUTHING_ACCEPTANCE_CONFIRM"] != "READ_ONLY_SANDBOX" || env["AUTHING_ACCESS_KEY_ID"] == "" || env["AUTHING_ACCESS_KEY_SECRET"] == "" {
		return fmt.Errorf("read-only sandbox plan requires the explicit flag, exact confirmation, and both Authing environment credentials")
	}
	return nil
}

func runPlan(root string, credentials map[string]string) error {
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("verified Terraform CLI missing; run python3 scripts/protocol_smoke.py first")
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err := os.Stat(goBinary); err != nil {
			return fmt.Errorf("Go toolchain missing")
		}
	}
	providerDir := filepath.Join(root, "provider")
	exampleDir := filepath.Join(root, "example")
	if err := os.MkdirAll(providerDir, 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(exampleDir, 0700); err != nil {
		return err
	}
	binary := filepath.Join(providerDir, "terraform-provider-authing")
	build := exec.Command(goBinary, "build", "-o", binary, ".")
	build.Dir = repo
	// Never print subprocess output: external diagnostics can include credential material.
	if err := build.Run(); err != nil {
		return fmt.Errorf("provider build failed (output suppressed): %w", err)
	}
	config := filepath.Join(root, "terraform.rc")
	if err := os.WriteFile(config, []byte(fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)), 0600); err != nil {
		return err
	}
	hcl := fmt.Sprintf(`terraform {
  required_providers {
    authing = { source = %q }
  }
}
provider "authing" {}
data "authing_global_security_settings" "sandbox" {}
`, source)
	if err := os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600); err != nil {
		return err
	}
	plan := exec.Command(terraform, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	plan.Dir = exampleDir
	clean := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "AUTHING_") && !strings.HasPrefix(entry, "TF_") && !strings.HasPrefix(entry, "CHECKPOINT_") {
			clean = append(clean, entry)
		}
	}
	clean = append(clean, "AUTHING_ACCESS_KEY_ID="+credentials["AUTHING_ACCESS_KEY_ID"], "AUTHING_ACCESS_KEY_SECRET="+credentials["AUTHING_ACCESS_KEY_SECRET"],
		"TF_CLI_CONFIG_FILE="+config, "TF_DATA_DIR="+filepath.Join(root, "data"), "TF_INPUT=0", "TF_IN_AUTOMATION=1", "CHECKPOINT_DISABLE=1", "HOME="+root)
	if host := credentials["AUTHING_HOST"]; host != "" {
		clean = append(clean, "AUTHING_HOST="+host)
	}
	plan.Env = clean
	// Capture and discard Terraform output even on failure: never publish API response or secrets.
	if _, err := plan.CombinedOutput(); err != nil {
		return fmt.Errorf("read-only Terraform plan failed (output suppressed; check sandbox access without sharing credentials): %w", err)
	}
	if _, err := os.Stat(filepath.Join(exampleDir, "terraform.tfstate")); !os.IsNotExist(err) {
		return fmt.Errorf("unexpected Terraform state file after plan")
	}
	return nil
}
