package acceptance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"terraform-provider-authing/internal/authingapi"
)

// External IdPs have no description field in the Management API create/GET DTOs.
// The generated name itself is the ownership marker; no connection is configured.
func ownedExtIdp(client *authingapi.Client, id, name string, drift bool) error {
	if id == "" || !sandboxCode.MatchString(name) {
		return errors.New("invalid external IdP ownership key")
	}
	got := client.GetExtIdp(&dto.GetExtIdpDto{Id: id})
	if got == nil || got.StatusCode != 200 || got.Data.Id != id || got.Data.Type != "oidc" || got.Data.TenantId != "" || got.Data.Name != name && (!drift || got.Data.Name != name+"-drift" && got.Data.Name != name+"-updated") {
		return errors.New("external IdP ownership not verified")
	}
	// Refuse deletion if the provider acquired a connection, or if the GET
	// response cannot establish that this test-only IdP has no connections.
	conns, ok := got.Data.Connections.([]any)
	if !ok || len(conns) != 0 {
		return errors.New("external IdP connections not verified empty")
	}
	return nil
}

func cleanupExtIdp(client *authingapi.Client, id, name string) error {
	if id == "" || !sandboxCode.MatchString(name) {
		return errors.New("external IdP cleanup requires a known ID and generated name")
	}
	for attempt := 0; attempt < 3; attempt++ {
		got := client.GetExtIdp(&dto.GetExtIdpDto{Id: id})
		if got != nil && got.StatusCode == 404 {
			return nil
		}
		if err := ownedExtIdp(client, id, name, true); err != nil {
			return err
		}
		deleted := client.DeleteExtIdp(&dto.DeleteExtIdpDto{Id: id})
		if deleted == nil || deleted.StatusCode != 200 && deleted.StatusCode != 404 || deleted.StatusCode == 200 && !deleted.Data.Success {
			return errors.New("external IdP delete rejected")
		}
		if attempt < 2 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	got := client.GetExtIdp(&dto.GetExtIdpDto{Id: id})
	if got != nil && got.StatusCode == 404 {
		return nil
	}
	return errors.New("external IdP absence not confirmed")
}

// Only the ID assigned by Terraform in this fresh workspace can authorize
// cleanup. A name search after failed create cannot prove which object was made.
func extIdpStateID(root string, env []string, terraform string) (string, error) {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("Terraform state inspection failed")
	}
	var state struct {
		Values struct {
			RootModule struct {
				Resources []struct {
					Address string `json:"address"`
					Values  struct {
						ID       string `json:"id"`
						ExtIdpID string `json:"ext_idp_id"`
						Name     string `json:"name"`
						Type     string `json:"type"`
						TenantID string `json:"tenant_id"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(out, &state) != nil {
		return "", errors.New("invalid Terraform state")
	}
	for _, r := range state.Values.RootModule.Resources {
		if r.Address == "authing_ext_idp.sandbox" && r.Values.ID != "" && r.Values.ID == r.Values.ExtIdpID && sandboxCode.MatchString(r.Values.Name) && r.Values.Type == "oidc" && r.Values.TenantID == "" {
			return r.Values.ID, nil
		}
	}
	return "", errors.New("external IdP ID absent from Terraform state")
}

func runExtIdpTrace(root string, credentials map[string]string, name string) (result error) {
	if !sandboxCode.MatchString(name) {
		return errors.New("external IdP tracer requires a generated hermesacc name")
	}
	// Do not accept a caller-supplied old state as evidence of a new create.
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		return fmt.Errorf("external-idp phase=fresh-workspace code=%s (output suppressed)", name)
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("external-idp phase=terraform-cli code=%s (output suppressed)", name)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("external-idp phase=go-toolchain code=%s (output suppressed)", name)
		}
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("external-idp phase=client code=%s (output suppressed)", name)
	}
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("external-idp phase=workspace code=%s (output suppressed)", name)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("external-idp phase=provider-build code=%s (output suppressed)", name)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	hcl := fmt.Sprintf(`terraform {
  required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_ext_idp" "sandbox" {
  name = %q
  type = "oidc"
}
`, source, name)
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("external-idp phase=config code=%s (output suppressed)", name)
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
		// Terraform might fail after creating an IdP. If no ID reached state,
		// deletion is forbidden: name matching alone is not creation proof.
		if id == "" {
			id, _ = extIdpStateID(root, env, terraform)
		}
		if id == "" {
			if result == nil {
				result = fmt.Errorf("external-idp phase=cleanup-incomplete code=%s (output suppressed)", name)
			}
			result = fmt.Errorf("%w cleanup=unknown", result)
			return
		}
		if cleanupExtIdp(client, id, name) != nil {
			if result == nil {
				result = fmt.Errorf("external-idp phase=cleanup-incomplete code=%s (output suppressed)", name)
			}
			result = fmt.Errorf("%w cleanup=incomplete", result)
		} else if result != nil {
			result = fmt.Errorf("%w cleanup=confirmed", result)
		} else {
			fmt.Printf("external-idp phase=complete code=%s cleanup=confirmed\n", name)
		}
	}()
	writeName := func(value string) error {
		return os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(strings.Replace(hcl, fmt.Sprintf("name = %q", name), fmt.Sprintf("name = %q", value), 1)), 0600)
	}
	verifyUpdated := func() error {
		if err := ownedExtIdp(client, id, name, true); err != nil {
			return err
		}
		got := client.GetExtIdp(&dto.GetExtIdpDto{Id: id})
		if got == nil || got.StatusCode != 200 || got.Data.Id != id || got.Data.Name != name+"-updated" {
			return errors.New("configured name update not confirmed")
		}
		return nil
	}
	importRoot := filepath.Join(root, "import")
	importEnv := traceEnvironment(importRoot, config, credentials)
	phases := []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-created-id", func() error {
			var e error
			id, e = extIdpStateID(root, env, terraform)
			if e != nil {
				return e
			}
			return ownedExtIdp(client, id, name, false)
		}},
		{"plan-converged", plan(0)},
		{"configure-name-update", func() error { return writeName(name + "-updated") }},
		{"plan-name-update", plan(2)},
		{"apply-name-update", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-name-update", verifyUpdated},
		{"plan-name-converged", plan(0)},
		{"configure-original-name", func() error { return writeName(name) }},
		{"apply-original-name", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-original-name", func() error { return ownedExtIdp(client, id, name, false) }},
		{"import-fresh-state", func() error {
			if os.MkdirAll(filepath.Join(importRoot, "example"), 0700) != nil || os.WriteFile(filepath.Join(importRoot, "example/main.tf"), []byte(hcl), 0600) != nil {
				return errors.New("fresh import workspace failed")
			}
			return terraformExit(importRoot, importEnv, terraform, 0, "import", "-input=false", "-no-color", "authing_ext_idp.sandbox", id)
		}},
		{"verify-imported-id", func() error {
			got, err := extIdpStateID(importRoot, importEnv, terraform)
			if err != nil || got != id {
				return errors.New("imported identity differs")
			}
			return ownedExtIdp(client, id, name, false)
		}},
		{"plan-import-converged", func() error {
			return terraformExit(importRoot, importEnv, terraform, 0, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
		}},
		{"remote-drift", func() error {
			if err := ownedExtIdp(client, id, name, false); err != nil {
				return err
			}
			res := client.UpdateExtIdp(&dto.UpdateExtIdpDto{Id: id, Name: name + "-drift"})
			if res == nil || res.StatusCode != 200 || res.Data.Id != id || res.Data.Name != name+"-drift" {
				return errors.New("external IdP drift mutation rejected")
			}
			got := client.GetExtIdp(&dto.GetExtIdpDto{Id: id})
			if got == nil || got.StatusCode != 200 || got.Data.Id != id || got.Data.Name != name+"-drift" || got.Data.Type != "oidc" || got.Data.TenantId != "" {
				return errors.New("external IdP drift not visible")
			}
			return ownedExtIdp(client, id, name, true)
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-owned-before-destroy", func() error { return ownedExtIdp(client, id, name, false) }},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			got := client.GetExtIdp(&dto.GetExtIdpDto{Id: id})
			if got == nil || got.StatusCode != 404 {
				return errors.New("external IdP still present")
			}
			return nil
		}},
	}
	for _, phase := range phases {
		if phase.run() != nil {
			return fmt.Errorf("external-idp phase=%s code=%s (output suppressed)", phase.name, name)
		}
		fmt.Printf("external-idp phase=%s code=%s result=passed\n", phase.name, name)
	}
	return result
}

func TestDestructiveLiveExtIdpTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	name, err := newGroupCode()
	if err != nil {
		t.Fatal("random external IdP name generation failed")
	}
	if err := runExtIdpTrace(t.TempDir(), env, name); err != nil {
		t.Fatal(err)
	}
}
