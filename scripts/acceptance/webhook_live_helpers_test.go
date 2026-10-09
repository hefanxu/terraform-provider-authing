package acceptance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"terraform-provider-authing/internal/authingapi"
)

const webhookInertURL = "https://example.invalid/"
const webhookEvent = "user.created"

// Reject ambiguous/incomplete inventories; exact name is an ownership hint,
// never authorization to delete without a subsequent exact-ID GET.
func findWebhook(client *authingapi.Client, name string) (string, error) {
	var match string
	seen := 0
	total := -1
	ids := map[string]bool{}
	for page := 1; page <= 100; page++ {
		body, err := client.SendHttpRequest("/api/v3/list-webhooks", "GET", map[string]any{"page": page, "limit": 50})
		if err != nil {
			return "", errors.New("webhook listing failed")
		}
		var result struct {
			StatusCode int `json:"statusCode"`
			Data       *struct {
				List []struct {
					WebhookID string `json:"webhookId"`
					Name      string `json:"name"`
				} `json:"list"`
				TotalCount *int `json:"totalCount"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &result) != nil || result.StatusCode != 200 || result.Data == nil || result.Data.List == nil || result.Data.TotalCount == nil || *result.Data.TotalCount < 0 || total >= 0 && total != *result.Data.TotalCount || len(result.Data.List) > 50 || seen+len(result.Data.List) > *result.Data.TotalCount {
			return "", errors.New("invalid webhook listing")
		}
		total = *result.Data.TotalCount
		for _, hook := range result.Data.List {
			if hook.WebhookID == "" || ids[hook.WebhookID] {
				return "", errors.New("invalid webhook identity in listing")
			}
			ids[hook.WebhookID] = true
			if hook.Name == name || hook.Name == name+"-drift" {
				if match != "" {
					return "", errors.New("ambiguous webhook ownership")
				}
				match = hook.WebhookID
			}
		}
		seen += len(result.Data.List)
		if seen == total {
			return match, nil
		}
		if len(result.Data.List) == 0 {
			return "", errors.New("incomplete webhook listing")
		}
	}
	return "", errors.New("webhook listing exceeded page limit")
}

// Only an exact GET for the created ID can authorize a delete. Events cannot
// be asserted here: this API's GET may omit the subscribed event list.
func ownedWebhook(client *authingapi.Client, id, name string) error {
	if id == "" || !sandboxCode.MatchString(name) {
		return errors.New("invalid webhook ownership key")
	}
	got := client.GetWebhook(&dto.GetWebhookDto{WebhookId: id})
	if got == nil || got.StatusCode != 200 || got.Data.WebhookId != id || (got.Data.Name != name && got.Data.Name != name+"-drift") || got.Data.Url != webhookInertURL || got.Data.Enabled {
		return errors.New("webhook ownership not verified")
	}
	return nil
}
func cleanupWebhook(client *authingapi.Client, id, name string) error {
	if id == "" || !sandboxCode.MatchString(name) {
		return errors.New("invalid webhook ownership key")
	}
	for attempt := 0; attempt < 3; attempt++ {
		got := client.GetWebhook(&dto.GetWebhookDto{WebhookId: id})
		if got != nil && got.StatusCode == 404 {
			return nil
		}
		if got == nil || got.StatusCode != 200 || got.Data.WebhookId != id || (got.Data.Name != name && got.Data.Name != name+"-drift") || got.Data.Url != webhookInertURL || got.Data.Enabled {
			return errors.New("webhook ownership not verified")
		}
		deleted := client.DeleteWebhook(&dto.DeleteWebhookDto{WebhookIds: []string{id}})
		if deleted == nil || deleted.StatusCode != 200 && deleted.StatusCode != 404 {
			return errors.New("webhook delete rejected")
		}
		if attempt < 2 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	got := client.GetWebhook(&dto.GetWebhookDto{WebhookId: id})
	if got != nil && got.StatusCode == 404 {
		return nil
	}
	return errors.New("webhook absence not confirmed")
}
func webhookStateID(root string, env []string, terraform string) (string, error) {
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
						ID        string `json:"id"`
						WebhookID string `json:"webhook_id"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(out, &state) != nil {
		return "", errors.New("invalid Terraform state")
	}
	for _, r := range state.Values.RootModule.Resources {
		if r.Address == "authing_webhook.sandbox" && r.Values.ID != "" && r.Values.ID == r.Values.WebhookID {
			return r.Values.ID, nil
		}
	}
	return "", errors.New("webhook ID absent from Terraform state")
}
func runWebhookTrace(root string, credentials map[string]string, name string) (result error) {
	if !sandboxCode.MatchString(name) {
		return errors.New("webhook tracer requires generated hermesacc name")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("webhook phase=terraform-cli code=%s (output suppressed)", name)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("webhook phase=go-toolchain code=%s (output suppressed)", name)
		}
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("webhook phase=client code=%s (output suppressed)", name)
	}
	// Never adopt or overwrite a webhook left by another run.
	existing, err := findWebhook(client, name)
	if err != nil || existing != "" {
		return fmt.Errorf("webhook phase=preflight code=%s (output suppressed)", name)
	}
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("webhook phase=workspace code=%s (output suppressed)", name)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("webhook phase=provider-build code=%s (output suppressed)", name)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	hcl := fmt.Sprintf(`terraform {
  required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_webhook" "sandbox" {
  name = %q
  url = %q
  content_type = "application/json"
  enabled = false
  events = [%q]
}
`, source, name, webhookInertURL, webhookEvent)
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("webhook phase=config code=%s (output suppressed)", name)
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	id := ""
	// Only a webhook ID pinned to this fresh Terraform state authorizes cleanup.
	// A matching name after failed create is a candidate, not deletion proof.
	defer func() {
		// Keep the first failure untouched; cleanup is an independent result.
		cleanup := "confirmed"
		if id == "" {
			id, _ = webhookStateID(root, env, terraform)
		}
		discovered, e := findWebhook(client, name)
		if e != nil || discovered != "" && (id == "" || discovered != id) {
			cleanup = "incomplete"
		} else if id != "" && cleanupWebhook(client, id, name) != nil {
			cleanup = "incomplete"
		}
		if result != nil {
			result = fmt.Errorf("%v cleanup=%s", result, cleanup)
		} else if cleanup != "confirmed" {
			result = fmt.Errorf("webhook phase=cleanup code=%s cleanup=incomplete (output suppressed)", name)
		}
	}()
	result = (traceCase{name: "webhook", code: name, phases: []tracePhase{
		{"apply-create", func() error {
			e := run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")()
			id, _ = webhookStateID(root, env, terraform)
			if e != nil && id == "" {
				return errors.New("apply failed without a recoverable ID; cleanup unverified")
			}
			return e
		}},
		{"plan-converged", plan(0)},
		{"verify-exact-id", func() error {
			var e error
			id, e = webhookStateID(root, env, terraform)
			if e != nil {
				return e
			}
			return ownedWebhook(client, id, name)
		}},
		{"remote-name-drift", func() error {
			if e := ownedWebhook(client, id, name); e != nil {
				return e
			}
			// Explicitly disabled; never issue a trigger/delivery endpoint request.
			body, err := client.SendHttpRequest("/api/v3/update-webhook", "POST", map[string]any{"webhookId": id, "name": name + "-drift", "url": webhookInertURL, "events": []string{webhookEvent}, "enabled": false})
			var res dto.UpdateWebhooksRespDto
			if err != nil || json.Unmarshal(body, &res) != nil || res.StatusCode != 200 {
				return errors.New("webhook drift mutation rejected")
			}
			got := client.GetWebhook(&dto.GetWebhookDto{WebhookId: id})
			if got == nil || got.StatusCode != 200 || got.Data.WebhookId != id || got.Data.Name != name+"-drift" || got.Data.Url != webhookInertURL || got.Data.Enabled {
				return errors.New("webhook name drift not visible")
			}
			return nil
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-owned-before-destroy", func() error {
			if e := ownedWebhook(client, id, name); e != nil {
				return e
			}
			got := client.GetWebhook(&dto.GetWebhookDto{WebhookId: id})
			if got == nil || got.Data.Name != name {
				return errors.New("webhook name not reconciled")
			}
			return nil
		}},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			got := client.GetWebhook(&dto.GetWebhookDto{WebhookId: id})
			if got == nil || got.StatusCode != 404 {
				return errors.New("webhook still present")
			}
			return nil
		}},
	}}).execute()
	return result
}
func TestDestructiveLiveWebhookTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	name, err := newGroupCode()
	if err != nil {
		t.Fatal("random webhook name generation failed")
	}
	if err := runWebhookTrace(t.TempDir(), env, name); err != nil {
		t.Fatal(err)
	}
}
