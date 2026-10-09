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
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

const flowScene = "PRE_AUTHENTICATION"

// Harmless even if invoked accidentally; enabled=false is still mandatory.
const flowInertSource = "async function pipe(user, context, callback) { callback(null, user, context); }"

func flowMarker(name string) string { return "hermesacc ownership " + name }

type flowDetail struct {
	FuncID          string `json:"funcId"`
	FuncName        string `json:"funcName"`
	FuncDescription string `json:"funcDescription"`
	Scene           string `json:"scene"`
	SourceCode      string `json:"sourceCode"`
	Enabled         *bool  `json:"enabled"`
}

func authFlowRead(c *authingapi.Client, id string) (int, flowDetail, error) {
	var detail flowDetail
	if id == "" {
		return 0, detail, errors.New("missing function ID")
	}
	raw, err := c.SendHttpRequest("/api/v3/get-auth-flow-function", http.MethodGet, map[string]string{"funcId": id})
	if err != nil {
		return 0, detail, errors.New("function GET failed")
	}
	var reply struct {
		StatusCode *int        `json:"statusCode"`
		Data       *flowDetail `json:"data"`
	}
	if json.Unmarshal(raw, &reply) != nil || reply.StatusCode == nil {
		return 0, detail, errors.New("invalid function GET")
	}
	if *reply.StatusCode == 200 {
		if reply.Data == nil {
			return 0, detail, errors.New("missing function data")
		}
		detail = *reply.Data
	}
	return *reply.StatusCode, detail, nil
}
func ownedAuthFlow(c *authingapi.Client, id, name string, expectedName string) error {
	if id == "" || !sandboxCode.MatchString(name) || expectedName != name && expectedName != name+"-drift" {
		return errors.New("invalid ownership key")
	}
	status, detail, err := authFlowRead(c, id)
	if err != nil || status != 200 || detail.FuncID != id || detail.FuncName != expectedName || detail.FuncDescription != flowMarker(name) || detail.Scene != flowScene || detail.SourceCode != flowInertSource || detail.Enabled == nil || *detail.Enabled {
		return errors.New("function ownership not verified")
	}
	return nil
}
func cleanupAuthFlow(c *authingapi.Client, id, name string) error {
	if id == "" || !sandboxCode.MatchString(name) {
		return errors.New("no exact state-derived ownership ID")
	}
	status, _, err := authFlowRead(c, id)
	if err != nil {
		return err
	}
	if status == 404 {
		return nil
	}
	if ownedAuthFlow(c, id, name, name) != nil && ownedAuthFlow(c, id, name, name+"-drift") != nil {
		return errors.New("function ownership not verified")
	}
	raw, err := c.SendHttpRequest("/api/v3/delete-auth-flow-function", http.MethodPost, map[string]string{"funcId": id})
	if err != nil {
		return errors.New("function delete failed")
	}
	var reply struct {
		StatusCode *int `json:"statusCode"`
		Data       *struct {
			Success *bool `json:"success"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &reply) != nil || reply.StatusCode == nil || *reply.StatusCode != 200 && *reply.StatusCode != 404 || reply.Data != nil && reply.Data.Success != nil && !*reply.Data.Success {
		return errors.New("function delete rejected")
	}
	status, _, err = authFlowRead(c, id)
	if err != nil || status != 404 {
		return errors.New("function absence not confirmed")
	}
	return nil
}

// terraform show -json contains source_code. Parse only the ID and discard the
// full buffer without ever logging it or returning its contents in diagnostics.
func authFlowStateID(root string, env []string, terraform string) (string, error) {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, err := cmd.Output()
	if err != nil {
		return "", errors.New("state inspection failed")
	}
	var state struct {
		Values struct {
			RootModule struct {
				Resources []struct {
					Address string `json:"address"`
					Values  struct {
						ID string `json:"id"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return "", errors.New("invalid state")
	}
	var id string
	for _, r := range state.Values.RootModule.Resources {
		if r.Address == "authing_auth_flow_function.sandbox" {
			if id != "" || r.Values.ID == "" {
				return "", errors.New("ambiguous function ID")
			}
			id = r.Values.ID
		}
	}
	if id == "" {
		return "", errors.New("no function ID in state")
	}
	return id, nil
}
func runAuthFlowTrace(root string, credentials map[string]string, name string) (result error) {
	if !sandboxCode.MatchString(name) {
		return errors.New("function tracer requires generated hermesacc name")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("auth-flow phase=terraform-cli code=%s (output suppressed)", name)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("auth-flow phase=go-toolchain code=%s (output suppressed)", name)
		}
	}
	c, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("auth-flow phase=client code=%s (output suppressed)", name)
	}
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("auth-flow phase=workspace code=%s (output suppressed)", name)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err = build.CombinedOutput(); err != nil {
		return fmt.Errorf("auth-flow phase=provider-build code=%s (output suppressed)", name)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	hcl := fmt.Sprintf(`terraform {
 required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_auth_flow_function" "sandbox" {
 func_name = %q
 func_description = %q
 scene = %q
 source_code = %q
 enabled = false
 is_asynchronous = false
 timeout = 3
 terminate_on_timeout = false
}
`, source, name, flowMarker(name), flowScene, flowInertSource)
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("auth-flow phase=config code=%s (output suppressed)", name)
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	id := ""
	// There is no reliable name lookup to recover an ID when apply creates
	// remotely but leaves no state. Never delete by guessed name or ID.
	defer func() {
		// result contains only a traceCase phase and generated code, never raw
		// Terraform/API output. Keep it even when cleanup cannot be verified.
		if id == "" {
			if result != nil {
				result = fmt.Errorf("%v cleanup=incomplete (no state-pinned ID; output suppressed)", result)
			}
			return
		}
		if cleanupAuthFlow(c, id, name) != nil {
			if result == nil {
				result = fmt.Errorf("auth-flow phase=cleanup code=%s cleanup=unknown (output suppressed)", name)
			} else {
				result = fmt.Errorf("%v cleanup=unknown (output suppressed)", result)
			}
		} else if result != nil {
			result = fmt.Errorf("%v cleanup=confirmed", result)
		}
	}()
	result = (traceCase{name: "auth-flow", code: name, phases: []tracePhase{
		{"apply-create", func() error {
			e := run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")()
			id, _ = authFlowStateID(root, env, terraform)
			return e
		}},
		{"plan-converged", plan(0)},
		{"verify-created", func() error { return ownedAuthFlow(c, id, name, name) }},
		{"remote-name-drift", func() error {
			if err := ownedAuthFlow(c, id, name, name); err != nil {
				return err
			}
			raw, err := c.SendHttpRequest("/api/v3/update-auth-flow-function", http.MethodPost, map[string]any{"funcId": id, "funcName": name + "-drift", "enabled": false})
			var reply struct {
				StatusCode int `json:"statusCode"`
				Data       struct {
					FuncID string `json:"funcId"`
				} `json:"data"`
			}
			if err != nil || json.Unmarshal(raw, &reply) != nil || reply.StatusCode != 200 || reply.Data.FuncID != id {
				return errors.New("drift update rejected")
			}
			return ownedAuthFlow(c, id, name, name+"-drift")
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-before-destroy", func() error { return ownedAuthFlow(c, id, name, name) }},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			status, _, err := authFlowRead(c, id)
			if err != nil || status != 404 {
				return errors.New("function still present")
			}
			return nil
		}},
	}}).execute()
	return result
}
func TestDestructiveLiveAuthFlowFunctionTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	name, err := newGroupCode()
	if err != nil {
		t.Fatal("function name generation failed")
	}
	if err := runAuthFlowTrace(t.TempDir(), env, name); err != nil {
		t.Fatal(err)
	}
}
