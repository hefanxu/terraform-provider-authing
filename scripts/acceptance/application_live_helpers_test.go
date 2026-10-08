package acceptance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"terraform-provider-authing/internal/authingapi"
)

const applicationCallback = "https://example.invalid/callback"

// Read only exact Terraform diagnostic headings. The response body, HCL,
// identifiers and URLs are untrusted and must never enter the returned error.
var applicationAPICode = regexp.MustCompile(`(?m)^[│ ]*code=([0-9]{3,6})(?: apiCode=([0-9]{1,10}))? msg=([^\n]{0,500})`)

var applicationErrorFields = []string{
	"appIdentifier", "appName", "appType", "appDescription", "defaultProtocol",
	"redirectUris", "logoutRedirectUris", "ssoEnabled", "oidcConfig", "samlConfig",
	"oauthConfig", "casConfig", "loginConfig", "registerConfig", "brandingConfig",
}

func applicationFieldHint(message string) string {
	match := ""
	for _, field := range applicationErrorFields {
		pattern := regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(field) + `($|[^A-Za-z0-9_])`)
		if !pattern.MatchString(message) {
			continue
		}
		if match != "" {
			return "multiple"
		}
		match = field
	}
	return match
}

func classifyApplicationApply(output []byte) string {
	category := "unclassified"
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "│"))
		switch line {
		case "Error: Failed to create Authing application":
			category = "application-create"
		case "Error: Failed to set application permission strategy":
			category = "permission-strategy-update"
		case "Error: Failed to read application permission strategy":
			category = "permission-strategy-readback"
		case "Error: Failed to confirm application permission strategy":
			category = "permission-strategy-mismatch"
		case "Error: Provider produced inconsistent result after apply":
			return "failure=inconsistent-result"
		}
	}
	if category == "unclassified" {
		return "failure=unclassified"
	}
	if match := applicationAPICode.FindSubmatch(output); len(match) == 4 {
		result := "failure=" + category + " api_code=" + string(match[1])
		if len(match[2]) != 0 && string(match[2]) != "0" {
			result += " detail_code=" + string(match[2])
		}
		if field := applicationFieldHint(string(match[3])); field != "" {
			result += " field=" + field
		}
		return result
	}
	return "failure=" + category
}

type applicationApplyFailure struct{ classification string }

func (e applicationApplyFailure) Error() string { return e.classification }

func applicationApplyExit(root string, env []string, terraform string) error {
	cmd := exec.Command(terraform, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	output, err := cmd.CombinedOutput() // In memory only; never log, persist or wrap.
	if err != nil {
		return applicationApplyFailure{classification: classifyApplicationApply(output)}
	}
	return nil
}

func executeApplicationTrace(c traceCase) error {
	for _, phase := range c.phases {
		if err := phase.run(); err != nil {
			if phase.name == "apply-create" {
				classification := "failure=unclassified"
				var classified applicationApplyFailure
				if errors.As(err, &classified) {
					classification = classified.classification
				}
				return fmt.Errorf("application phase=%s code=%s %s (output suppressed)", phase.name, c.code, classification)
			}
			return fmt.Errorf("application phase=%s code=%s (output suppressed)", phase.name, c.code)
		}
	}
	return nil
}

// Query the entire unfiltered result set; a missing/incomplete page is never absence.
func findApplication(client *authingapi.Client, name string) (string, error) {
	var match string
	seen := 0
	for page := 1; page <= 100; page++ {
		body, err := client.SendHttpRequest("/api/v3/list-applications", "GET", map[string]any{"page": page, "limit": 100})
		if err != nil {
			return "", errors.New("application listing failed")
		}
		var result struct {
			StatusCode int `json:"statusCode"`
			Data       *struct {
				List []struct {
					AppId         string `json:"appId"`
					AppName       string `json:"appName"`
					AppIdentifier string `json:"appIdentifier"`
				} `json:"list"`
				TotalCount *int `json:"totalCount"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &result) != nil || result.StatusCode != 200 || result.Data == nil || result.Data.List == nil || result.Data.TotalCount == nil || *result.Data.TotalCount < 0 || len(result.Data.List) > 100 || seen+len(result.Data.List) > *result.Data.TotalCount {
			return "", errors.New("invalid application listing")
		}
		for _, app := range result.Data.List {
			if app.AppName == name || app.AppIdentifier == name {
				if app.AppId == "" || match != "" {
					return "", errors.New("ambiguous application ownership")
				}
				match = app.AppId
			}
		}
		seen += len(result.Data.List)
		if seen == *result.Data.TotalCount {
			return match, nil
		}
		if len(result.Data.List) == 0 {
			return "", errors.New("incomplete application listing")
		}
	}
	return "", errors.New("application listing exceeded page limit")
}

func ownedApplication(client *authingapi.Client, id, name, marker string) error {
	if id == "" || !sandboxCode.MatchString(name) || marker != "hermesacc ownership "+name {
		return errors.New("invalid application ownership key")
	}
	res := client.GetApplication(&dto.GetApplicationDto{AppId: id})
	if res == nil || res.StatusCode != 200 || res.Data.AppId != id || res.Data.AppName != name && res.Data.AppName != name+"-drift" || res.Data.AppIdentifier != name || res.Data.AppDescription != marker || res.Data.AppType != "web" {
		return errors.New("application ownership not verified")
	}
	return nil
}

func cleanupApplication(client *authingapi.Client, id, name, marker string) error {
	for attempt := 0; attempt < 3; attempt++ {
		res := client.GetApplication(&dto.GetApplicationDto{AppId: id})
		if res != nil && res.StatusCode == 404 {
			return nil
		}
		if err := ownedApplication(client, id, name, marker); err != nil {
			return err
		}
		deleted := client.DeleteApplication(&dto.DeleteApplicationDto{AppId: id})
		if deleted == nil || deleted.StatusCode != 200 && deleted.StatusCode != 404 {
			return errors.New("application delete rejected")
		}
		if attempt < 2 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	res := client.GetApplication(&dto.GetApplicationDto{AppId: id})
	if res != nil && res.StatusCode == 404 {
		return nil
	}
	return errors.New("application absence not confirmed")
}

// Terraform's machine-readable state is parsed in memory, never printed or saved.
func applicationStateID(root string, env []string, terraform string) (string, error) {
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
						ID    string `json:"id"`
						AppID string `json:"app_id"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(out, &state) != nil {
		return "", errors.New("invalid Terraform state")
	}
	for _, r := range state.Values.RootModule.Resources {
		if r.Address == "authing_application.sandbox" && r.Values.ID != "" && r.Values.ID == r.Values.AppID {
			return r.Values.ID, nil
		}
	}
	return "", errors.New("application ID absent from Terraform state")
}

func runApplicationTrace(root string, credentials map[string]string, name string) (result error) {
	if !sandboxCode.MatchString(name) {
		return errors.New("application tracer requires generated hermesacc name")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("application phase=terraform-cli code=%s (output suppressed)", name)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("application phase=go-toolchain code=%s (output suppressed)", name)
		}
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("application phase=client code=%s (output suppressed)", name)
	}
	marker := "hermesacc ownership " + name
	existing, err := findApplication(client, name)
	if err != nil || existing != "" {
		return fmt.Errorf("application phase=preflight code=%s (output suppressed)", name)
	}
	started := false
	id := ""
	defer func() {
		if !started {
			return
		}
		// Only a Terraform state ID from this run authorizes deletion. A name
		// match, even with a marker, cannot prove this run created the object.
		if id == "" {
			id, _ = applicationStateID(root, traceEnvironment(root, filepath.Join(root, "terraform.rc"), credentials), terraform)
		}
		discovered, e := findApplication(client, name)
		cleanup := "confirmed"
		if e != nil || discovered != "" && (id == "" || discovered != id) {
			cleanup = "incomplete"
		} else if id != "" && cleanupApplication(client, id, name, marker) != nil {
			cleanup = "incomplete"
		} else if id == "" && result != nil {
			// An empty listing after a failed apply does not prove no create occurred.
			cleanup = "incomplete"
		}
		if result != nil {
			result = fmt.Errorf("%s cleanup=%s", result, cleanup)
		} else if cleanup == "incomplete" {
			result = fmt.Errorf("application phase=cleanup-incomplete code=%s cleanup=incomplete (output suppressed)", name)
		}
	}()
	providerDir := filepath.Join(root, "provider")
	exampleDir := filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("application phase=workspace code=%s (output suppressed)", name)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("application phase=provider-build code=%s (output suppressed)", name)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	// No client secret, login endpoint, privilege grant, or active SSO.
	hcl := fmt.Sprintf(`terraform {
  required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_application" "sandbox" {
  app_name = %q
  app_identifier = %q
  app_type = "web"
  description = %q
  sso_enabled = false
  permission_strategy = "DENY_ALL"
  redirect_uris = [%q]
  logout_redirect_uris = []
}
`, source, name, name, marker, applicationCallback)
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("application phase=config code=%s (output suppressed)", name)
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	started = true
	result = executeApplicationTrace(traceCase{name: "application", code: name, phases: []tracePhase{
		{"apply-create", func() error { return applicationApplyExit(root, env, terraform) }},
		{"plan-converged", plan(0)},
		{"verify-id-and-marker", func() error {
			var e error
			id, e = applicationStateID(root, env, terraform)
			if e != nil {
				return e
			}
			found, e := findApplication(client, name)
			if e != nil || found != id {
				return errors.New("application ID not uniquely resolved")
			}
			return ownedApplication(client, id, name, marker)
		}},
		{"remote-drift", func() error {
			if err := ownedApplication(client, id, name, marker); err != nil {
				return err
			}
			body, err := client.SendHttpRequest("/api/v3/update-application", "POST", map[string]string{"appId": id, "appName": name + "-drift"})
			if err != nil {
				return errors.New("remote drift failed")
			}
			var res struct {
				StatusCode int `json:"statusCode"`
				Data       struct {
					Success bool `json:"success"`
				} `json:"data"`
			}
			if json.Unmarshal(body, &res) != nil || res.StatusCode != 200 || !res.Data.Success {
				return errors.New("remote drift rejected")
			}
			got := client.GetApplication(&dto.GetApplicationDto{AppId: id})
			if got == nil || got.StatusCode != 200 || got.Data.AppId != id || got.Data.AppName != name+"-drift" || got.Data.AppDescription != marker {
				return errors.New("remote drift not visible")
			}
			return nil
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-owned-before-destroy", func() error {
			if err := ownedApplication(client, id, name, marker); err != nil {
				return err
			}
			got := client.GetApplication(&dto.GetApplicationDto{AppId: id})
			if got.Data.AppName != name {
				return errors.New("application name not reconciled")
			}
			return nil
		}},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			got := client.GetApplication(&dto.GetApplicationDto{AppId: id})
			if got == nil || got.StatusCode != 404 {
				return errors.New("application still present")
			}
			return nil
		}},
	}})
	return result
}

func TestDestructiveLiveApplicationTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	name, err := newGroupCode()
	if err != nil {
		t.Fatal("random application name generation failed")
	}
	if err := runApplicationTrace(t.TempDir(), env, name); err != nil {
		t.Fatal(err)
	}
}
