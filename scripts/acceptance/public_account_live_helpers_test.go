package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"terraform-provider-authing/internal/authingapi"
)

type publicAccountResponse struct {
	StatusCode int `json:"statusCode"`
	Data       struct {
		UserID   string `json:"userId"`
		Username string `json:"username"`
		Name     string `json:"name"`
		Success  bool   `json:"success"`
	} `json:"data"`
}

// Never expose API bodies or transport errors: they may contain credentials or PII.
func publicAccountRequest(client *authingapi.Client, path, method string, body any) (publicAccountResponse, error) {
	var result publicAccountResponse
	raw, err := client.SendHttpRequestContext(context.Background(), path, method, body)
	if err != nil || json.Unmarshal(raw, &result) != nil || result.StatusCode == 0 {
		return result, errors.New("public account API request failed")
	}
	return result, nil
}
func publicAccountGet(client *authingapi.Client, id string) (publicAccountResponse, error) {
	if id == "" {
		return publicAccountResponse{}, errors.New("empty public account ID")
	}
	return publicAccountRequest(client, "/api/v3/get-public-account", "GET", map[string]string{"userId": id, "userIdType": "user_id"})
}
func ownedPublicAccount(client *authingapi.Client, id, username string) error {
	if id == "" || !sandboxCode.MatchString(username) {
		return errors.New("public account ownership unverified")
	}
	got, err := publicAccountGet(client, id)
	if err != nil || got.StatusCode != 200 || got.Data.UserID != id || got.Data.Username != username || got.Data.Name != username && got.Data.Name != username+"-drift" {
		return errors.New("public account ownership unverified")
	}
	return nil
}

// No username-based deletion: only a Terraform state-derived, exact-ID-verified
// account may be removed. Re-read identity before every retry.
func cleanupPublicAccount(client *authingapi.Client, id, username string) error {
	if id == "" || !sandboxCode.MatchString(username) {
		return errors.New("public account ownership unverified")
	}
	for attempt := 0; attempt < 3; attempt++ {
		got, err := publicAccountGet(client, id)
		if err != nil {
			return errors.New("cleanup GET failed")
		}
		if got.StatusCode == 404 {
			return nil
		}
		if got.StatusCode != 200 || got.Data.UserID != id || got.Data.Username != username || got.Data.Name != username && got.Data.Name != username+"-drift" {
			return errors.New("public account ownership unverified")
		}
		deleted, err := publicAccountRequest(client, "/api/v3/delete-public-accounts-batch", "POST", map[string]any{"userIds": []string{id}})
		if err != nil || deleted.StatusCode != 200 || !deleted.Data.Success {
			// A transient failure may have deleted remotely; confirm through GET on retry.
			if attempt == 2 {
				return errors.New("public account cleanup deletion rejected")
			}
		}
		if attempt < 2 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	got, err := publicAccountGet(client, id)
	if err == nil && got.StatusCode == 404 {
		return nil
	}
	return errors.New("public account cleanup absence not confirmed")
}

// Terraform state is read in memory only; reject unexpected addresses and
// require the generated username and configured name alongside the remote ID.
func publicAccountStateID(root, terraform string, env []string, username string) (string, error) {
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
						ID       string `json:"id"`
						Username string `json:"username"`
						Name     string `json:"name"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil || len(state.Values.RootModule.Resources) != 1 {
		return "", errors.New("Terraform state invalid")
	}
	r := state.Values.RootModule.Resources[0]
	if r.Address != "authing_public_account.sandbox" || r.Values.ID == "" || r.Values.Username != username || r.Values.Name != username && r.Values.Name != username+"-drift" {
		return "", errors.New("Terraform state ownership unverified")
	}
	return r.Values.ID, nil
}

func runPublicAccountTrace(root string, credentials map[string]string, username string) (result error) {
	if !sandboxCode.MatchString(username) {
		return errors.New("public account tracer requires generated hermesacc username")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("public-account phase=terraform-cli username=%s (output suppressed)", username)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("public-account phase=go-toolchain username=%s (output suppressed)", username)
		}
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("public-account phase=client username=%s (output suppressed)", username)
	}
	providerDir := filepath.Join(root, "provider")
	exampleDir := filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("public-account phase=workspace username=%s (output suppressed)", username)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("public-account phase=provider-build username=%s (output suppressed)", username)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	hcl := fmt.Sprintf(`terraform {
 required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_public_account" "sandbox" {
 username = %q
 name = %q
}
`, source, username, username)
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("public-account phase=config username=%s (output suppressed)", username)
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
			id, _ = publicAccountStateID(root, terraform, env, username)
		}
		if id == "" {
			if result != nil {
				result = fmt.Errorf("public-account phase=cleanup-unknown-id username=%s (output suppressed)", username)
			}
			return
		}
		if cleanupPublicAccount(client, id, username) != nil {
			result = fmt.Errorf("public-account phase=cleanup-incomplete username=%s id=%s (output suppressed)", username, id)
		}
	}()
	started = true
	result = (traceCase{name: "public-account", code: username, phases: []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"capture-id", func() error { var e error; id, e = publicAccountStateID(root, terraform, env, username); return e }},
		{"plan-converged", plan(0)},
		{"verify-created", func() error {
			got, e := publicAccountGet(client, id)
			if e != nil || got.StatusCode != 200 || got.Data.UserID != id || got.Data.Username != username || got.Data.Name != username {
				return errors.New("created ID unverified")
			}
			return nil
		}},
		{"remote-drift", func() error {
			if e := ownedPublicAccount(client, id, username); e != nil {
				return e
			}
			updated, e := publicAccountRequest(client, "/api/v3/update-public-account", "POST", map[string]string{"userId": id, "name": username + "-drift"})
			if e != nil || updated.StatusCode != 200 || updated.Data.UserID != id {
				return errors.New("drift mutation failed")
			}
			got, e := publicAccountGet(client, id)
			if e != nil || got.StatusCode != 200 || got.Data.UserID != id || got.Data.Username != username || got.Data.Name != username+"-drift" {
				return errors.New("drift not visible")
			}
			return nil
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-owned-before-destroy", func() error {
			got, e := publicAccountGet(client, id)
			if e != nil || got.StatusCode != 200 || got.Data.UserID != id || got.Data.Username != username || got.Data.Name != username {
				return errors.New("reconciled ID unverified")
			}
			return nil
		}},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			got, e := publicAccountGet(client, id)
			if e != nil || got.StatusCode != 404 {
				return errors.New("public account still present")
			}
			return nil
		}},
	}}).execute()
	return result
}
