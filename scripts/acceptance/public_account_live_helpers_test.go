package acceptance

import (
	"context"
	"crypto/sha256"
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
		Nickname string `json:"nickname"`
		Email    string `json:"email"`
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
	if err != nil || got.StatusCode != 200 || got.Data.UserID != id || !publicAccountOwnedFields(got, username) {
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
		if got.StatusCode != 200 || got.Data.UserID != id || !publicAccountOwnedFields(got, username) {
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
	if r.Address != "authing_public_account.sandbox" || r.Values.ID == "" || r.Values.Username != username && r.Values.Username != username+"-updated" && r.Values.Username != username+"-drift" || r.Values.Name != username && r.Values.Name != username+"-updated" && r.Values.Name != username+"-drift" {
		return "", errors.New("Terraform state ownership unverified")
	}
	return r.Values.ID, nil
}

func publicAccountOwnedFields(got publicAccountResponse, username string) bool {
	return (got.Data.Username == username || got.Data.Username == username+"-updated" || got.Data.Username == username+"-drift") &&
		(got.Data.Name == username || got.Data.Name == username+"-updated" || got.Data.Name == username+"-drift")
}

func runPublicAccountTrace(root string, credentials map[string]string, username string) (result error) {
	if !sandboxCode.MatchString(username) {
		return errors.New("public account tracer requires generated username")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		return fmt.Errorf("public-account phase=fresh-workspace code=%s (output suppressed)", username)
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("public-account phase=terraform-cli code=%s (output suppressed)", username)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("public-account phase=client code=%s (output suppressed)", username)
	}
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("public-account phase=workspace code=%s (output suppressed)", username)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("public-account phase=provider-build code=%s (output suppressed)", username)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n dev_overrides {\n %q = %q\n }\n direct {}\n}\n", source, providerDir)
	makeHCL := func(suffix string) string {
		return fmt.Sprintf(`terraform {
 required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_public_account" "sandbox" {
 username = %q
 name = %q
 nickname = %q
 email = %q
}
`, source, username+suffix, username+suffix, username+suffix, username+suffix+"@example.invalid")
	}
	hcl := makeHCL("")
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("public-account phase=config code=%s (output suppressed)", username)
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
		cleanup := "unknown"
		if id != "" {
			cleanup = "incomplete"
			if cleanupPublicAccount(client, id, username) == nil {
				cleanup = "confirmed"
			}
		}
		if result == nil && cleanup != "confirmed" {
			result = fmt.Errorf("public-account phase=cleanup-incomplete code=%s (output suppressed)", username)
		}
		fingerprint := ""
		if id != "" {
			fingerprint = fmt.Sprintf(" state_id_sha256=%x", sha256.Sum256([]byte(id)))
		}
		if result != nil {
			result = fmt.Errorf("%w cleanup=%s%s", result, cleanup, fingerprint)
		} else {
			fmt.Printf("public-account phase=complete code=%s cleanup=confirmed%s\n", username, fingerprint)
		}
	}()
	verify := func(suffix string) error {
		got, e := publicAccountGet(client, id)
		if e != nil || got.StatusCode != 200 || got.Data.UserID != id || got.Data.Username != username+suffix || got.Data.Name != username+suffix || got.Data.Nickname != username+suffix || got.Data.Email != username+suffix+"@example.invalid" {
			return errors.New("configured fields readback unverified")
		}
		return nil
	}
	importRoot := filepath.Join(root, "import")
	importEnv := traceEnvironment(importRoot, config, credentials)
	phases := []tracePhase{
		{"preflight-absent", func() error {
			got, e := publicAccountRequest(client, "/api/v3/get-public-account", "GET", map[string]string{"userId": username, "userIdType": "username"})
			if e != nil || got.StatusCode != 404 {
				return errors.New("absence unproved")
			}
			return nil
		}},
		{"apply-create", func() error {
			started = true
			return run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")()
		}},
		{"capture-id", func() error { var e error; id, e = publicAccountStateID(root, terraform, env, username); return e }},
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
		{"import-fresh-state", func() error {
			if os.MkdirAll(filepath.Join(importRoot, "example"), 0700) != nil || os.WriteFile(filepath.Join(importRoot, "example/main.tf"), []byte(hcl), 0600) != nil {
				return errors.New("fresh import workspace failed")
			}
			return terraformExit(importRoot, importEnv, terraform, 0, "import", "-input=false", "-no-color", "authing_public_account.sandbox", id)
		}},
		{"verify-imported-id", func() error {
			got, e := publicAccountStateID(importRoot, terraform, importEnv, username)
			if e != nil || got != id {
				return errors.New("imported identity differs")
			}
			return verify("")
		}},
		{"plan-import-converged", func() error {
			return terraformExit(importRoot, importEnv, terraform, 0, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
		}},
		{"remote-drift", func() error {
			if e := ownedPublicAccount(client, id, username); e != nil {
				return e
			}
			got, e := publicAccountRequest(client, "/api/v3/update-public-account", "POST", map[string]string{"userId": id, "username": username + "-drift", "name": username + "-drift", "nickname": username + "-drift", "email": username + "-drift@example.invalid"})
			if e != nil || got.StatusCode != 200 || got.Data.UserID != id {
				return errors.New("drift mutation rejected")
			}
			got, e = publicAccountGet(client, id)
			if e != nil || got.StatusCode != 200 || got.Data.UserID != id || got.Data.Username != username+"-drift" || got.Data.Name != username+"-drift" || got.Data.Nickname != username+"-drift" || got.Data.Email != username+"-drift@example.invalid" {
				return errors.New("drift readback unverified")
			}
			return nil
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-owned-before-destroy", func() error { return verify("") }},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			got, e := publicAccountGet(client, id)
			if e != nil || got.StatusCode != 404 {
				return errors.New("absence unconfirmed")
			}
			return nil
		}},
	}
	for _, phase := range phases {
		if phase.run() != nil {
			return fmt.Errorf("public-account phase=%s code=%s (output suppressed)", phase.name, username)
		}
		fmt.Printf("public-account phase=%s code=%s result=passed\n", phase.name, username)
	}
	return nil
}
