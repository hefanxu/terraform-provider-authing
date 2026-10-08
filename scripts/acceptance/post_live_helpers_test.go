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

// A generated code, exact GET identity, ownership description and expected
// name must all agree before a post can be removed. Never infer ownership from
// a name search or from Terraform's state alone.
func postRead(client *authingapi.Client, code string) (int, *dto.CreatePostDto, error) {
	body, err := client.SendHttpRequest("/api/v3/get-post", http.MethodGet, &dto.GetPostDto{Code: code})
	if err != nil {
		return 0, nil, errors.New("post GET failed")
	}
	var result struct {
		StatusCode *int               `json:"statusCode"`
		Data       *dto.CreatePostDto `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil || result.StatusCode == nil {
		return 0, nil, errors.New("invalid post GET")
	}
	return *result.StatusCode, result.Data, nil
}
func verifyOwnedPost(client *authingapi.Client, code, name, marker string) error {
	if !sandboxCode.MatchString(code) || marker != "hermesacc ownership "+code || name != code && name != code+"-updated" {
		return errors.New("invalid post ownership key")
	}
	status, data, err := postRead(client, code)
	if err != nil || status != 200 || data == nil || data.Code != code || data.Description != marker || data.Name != name && data.Name != name+"-drift" && !(name == code+"-updated" && data.Name == code) {
		return errors.New("post ownership not verified")
	}
	return nil
}
func cleanupPost(client *authingapi.Client, code, name, marker string) error {
	if !sandboxCode.MatchString(code) || marker != "hermesacc ownership "+code {
		return errors.New("invalid post cleanup identity")
	}
	status, _, err := postRead(client, code)
	if err != nil {
		return err
	}
	if status == 404 {
		return nil
	}
	if err := verifyOwnedPost(client, code, name, marker); err != nil {
		return err
	}
	body, err := client.SendHttpRequest("/api/v3/remove-post", http.MethodPost, &dto.RemovePostDto{Code: code})
	var removed struct {
		StatusCode *int `json:"statusCode"`
		Data       *struct {
			Success *bool `json:"success"`
		} `json:"data"`
	}
	if err != nil || json.Unmarshal(body, &removed) != nil || removed.StatusCode == nil || *removed.StatusCode != 200 || removed.Data != nil && removed.Data.Success != nil && !*removed.Data.Success {
		return errors.New("post deletion not confirmed")
	}
	status, _, err = postRead(client, code)
	if err != nil || status != 404 {
		return errors.New("post absence not confirmed")
	}
	return nil
}
func runPostTrace(root string, credentials map[string]string, code string) (result error) {
	if !sandboxCode.MatchString(code) {
		return errors.New("post tracer requires generated hermesacc code")
	}
	name, marker := code, "hermesacc ownership "+code
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("post phase=terraform-cli code=%s (output suppressed)", code)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("post phase=go-toolchain code=%s (output suppressed)", code)
		}
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("post phase=client code=%s (output suppressed)", code)
	}
	status, _, err := postRead(client, code)
	if err != nil || status != 404 {
		return fmt.Errorf("post phase=preflight code=%s (output suppressed)", code)
	}
	started := false
	defer func() {
		if started && cleanupPost(client, code, name, marker) != nil {
			result = fmt.Errorf("post phase=cleanup-incomplete code=%s (output suppressed; manual inspection required)", code)
		}
	}()
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("post phase=workspace code=%s (output suppressed)", code)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("post phase=provider-build code=%s (output suppressed)", code)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	hcl := fmt.Sprintf(`terraform {
 required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_post" "sandbox" {
 code = %q
 name = %q
 description = %q
}
`, source, code, name, marker)
	tfFile := filepath.Join(exampleDir, "main.tf")
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(tfFile, []byte(hcl), 0600) != nil {
		return fmt.Errorf("post phase=config code=%s (output suppressed)", code)
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	started = true // Apply may create remotely yet leave no Terraform state.
	result = (traceCase{name: "post", code: code, phases: []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-converged", plan(0)},
		{"verify-created", func() error { return verifyOwnedPost(client, code, name, marker) }},
		{"configure-name-update", func() error {
			changed := strings.Replace(hcl, fmt.Sprintf("name = %q", name), fmt.Sprintf("name = %q", name+"-updated"), 1)
			if changed == hcl {
				return errors.New("post configuration did not change")
			}
			if err := os.WriteFile(tfFile, []byte(changed), 0600); err != nil {
				return err
			}
			name += "-updated"
			return nil
		}},
		{"plan-name-update", plan(2)},
		{"apply-name-update", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-name-update", func() error {
			status, data, err := postRead(client, code)
			if err != nil || status != 200 || data == nil || data.Code != code || data.Name != name || data.Description != marker {
				return errors.New("post Terraform name update not visible")
			}
			return nil
		}},
		{"plan-updated", plan(0)},
		{"remote-name-drift", func() error {
			if err := verifyOwnedPost(client, code, name, marker); err != nil {
				return err
			}
			body, err := client.SendHttpRequest("/api/v3/update-post", http.MethodPost, &dto.CreatePostDto{Code: code, Name: name + "-drift", Description: marker})
			var updated struct {
				StatusCode *int               `json:"statusCode"`
				Data       *dto.CreatePostDto `json:"data"`
			}
			if err != nil || json.Unmarshal(body, &updated) != nil || updated.StatusCode == nil || *updated.StatusCode != 200 || updated.Data == nil || updated.Data.Code != code || updated.Data.Name != name+"-drift" {
				return errors.New("post drift mutation failed")
			}
			status, data, err := postRead(client, code)
			if err != nil || status != 200 || data == nil || data.Code != code || data.Name != name+"-drift" || data.Description != marker {
				return errors.New("post drift not visible")
			}
			return nil
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-before-destroy", func() error {
			status, data, err := postRead(client, code)
			if err != nil || status != 200 || data == nil || data.Name != name {
				return errors.New("post reconciliation not visible")
			}
			return verifyOwnedPost(client, code, name, marker)
		}},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			status, _, err := postRead(client, code)
			if err != nil || status != 404 {
				return errors.New("post still present")
			}
			return nil
		}},
	}}).execute()
	return result
}
