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
	"terraform-provider-authing/internal/authingapi"
	"testing"
)

const pipelineScene = "PRE_REGISTER"

// An inert callback even if the isolated registration scene is invoked accidentally.
const pipelineInertSource = "async function pipe(context, callback) { callback(null, context); }"

func pipelineMarker(code string) string { return "hermesacc ownership " + code }

type pipelineDetail struct {
	ID          string `json:"funcId"`
	Name        string `json:"funcName"`
	Description string `json:"funcDescription"`
	Scene       string `json:"scene"`
	Source      string `json:"sourceCode"`
	Async       *bool  `json:"isAsynchronous"`
	Enabled     *bool  `json:"enabled"`
}

func pipelineRead(c *authingapi.Client, id string) (int, pipelineDetail, error) {
	var d pipelineDetail
	if id == "" {
		return 0, d, errors.New("missing ID")
	}
	raw, e := c.SendHttpRequest("/api/v3/get-pipeline-function", http.MethodGet, map[string]string{"funcId": id})
	if e != nil {
		return 0, d, errors.New("pipeline GET failed")
	}
	var reply struct {
		Status *int            `json:"statusCode"`
		Data   *pipelineDetail `json:"data"`
	}
	if json.Unmarshal(raw, &reply) != nil || reply.Status == nil {
		return 0, d, errors.New("invalid pipeline GET")
	}
	if *reply.Status == 200 {
		if reply.Data == nil || reply.Data.ID != id {
			return 0, d, errors.New("pipeline identity mismatch")
		}
		d = *reply.Data
	}
	return *reply.Status, d, nil
}
func ownedPipeline(c *authingapi.Client, id, code, name string) error {
	if !sandboxCode.MatchString(code) || id == "" || name != code && name != code+"-drift" {
		return errors.New("invalid pipeline ownership key")
	}
	status, d, e := pipelineRead(c, id)
	if e != nil || status != 200 || d.ID != id || d.Name != name || d.Description != pipelineMarker(code) || d.Scene != pipelineScene || d.Source != pipelineInertSource || d.Async == nil || *d.Async || d.Enabled == nil || *d.Enabled {
		return errors.New("pipeline ownership unverified")
	}
	return nil
}
func cleanupPipeline(c *authingapi.Client, id, code string) error {
	if !sandboxCode.MatchString(code) || id == "" {
		return errors.New("missing state-derived ID")
	}
	status, _, e := pipelineRead(c, id)
	if e != nil {
		return e
	}
	if status == 404 {
		return nil
	}
	if ownedPipeline(c, id, code, code) != nil && ownedPipeline(c, id, code, code+"-drift") != nil {
		return errors.New("pipeline ownership unverified")
	}
	raw, e := c.SendHttpRequest("/api/v3/delete-pipeline-function", http.MethodPost, map[string]string{"funcId": id})
	var reply struct {
		Status *int `json:"statusCode"`
		Data   *struct {
			Success *bool `json:"success"`
		} `json:"data"`
	}
	if e != nil || json.Unmarshal(raw, &reply) != nil || reply.Status == nil || *reply.Status != 200 && *reply.Status != 404 || reply.Data != nil && reply.Data.Success != nil && !*reply.Data.Success {
		return errors.New("pipeline deletion rejected")
	}
	status, _, e = pipelineRead(c, id)
	if e != nil || status != 404 {
		return errors.New("pipeline absence unconfirmed")
	}
	return nil
}

// Terraform state contains source_code: extract only the ID and never print the buffer.
func pipelineStateID(root string, env []string, terraform string) (string, error) {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, e := cmd.Output()
	if e != nil {
		return "", errors.New("state inspection failed")
	}
	var state struct {
		Values struct {
			Root struct {
				Resources []struct {
					Address string `json:"address"`
					Values  struct {
						ID     string `json:"id"`
						FuncID string `json:"func_id"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return "", errors.New("invalid state")
	}
	id := ""
	for _, r := range state.Values.Root.Resources {
		if r.Address == "authing_pipeline_function.sandbox" {
			if id != "" || r.Values.ID == "" || r.Values.FuncID != r.Values.ID {
				return "", errors.New("ambiguous pipeline ID")
			}
			id = r.Values.ID
		}
	}
	if id == "" {
		return "", errors.New("pipeline state ID missing")
	}
	return id, nil
}
func runPipelineTrace(root string, credentials map[string]string, code string) (result error) {
	if !sandboxCode.MatchString(code) {
		return errors.New("pipeline tracer requires generated name")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, e := os.Stat(terraform); e != nil {
		return fmt.Errorf("pipeline phase=terraform-cli code=%s (output suppressed)", code)
	}
	goBinary, e := exec.LookPath("go")
	if e != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, e = os.Stat(goBinary); e != nil {
			return fmt.Errorf("pipeline phase=go-toolchain code=%s (output suppressed)", code)
		}
	}
	c, e := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if e != nil {
		return fmt.Errorf("pipeline phase=client code=%s (output suppressed)", code)
	}
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("pipeline phase=workspace code=%s (output suppressed)", code)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, e = build.CombinedOutput(); e != nil {
		return fmt.Errorf("pipeline phase=provider-build code=%s (output suppressed)", code)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n dev_overrides {\n %q = %q\n }\n direct {}\n}\n", source, providerDir)
	hcl := fmt.Sprintf(`terraform {
 required_providers {
  authing = { source = %q }
 }
}
provider "authing" {}
resource "authing_pipeline_function" "sandbox" {
 func_name = %q
 func_description = %q
 scene = %q
 source_code = %q
 is_asynchronous = false
}
`, source, code, pipelineMarker(code), pipelineScene, pipelineInertSource)
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("pipeline phase=config code=%s (output suppressed)", code)
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
		if id == "" {
			if result != nil {
				result = fmt.Errorf("%v; cleanup-incomplete (no state ID)", result)
			}
			return
		}
		if cleanupPipeline(c, id, code) != nil {
			result = fmt.Errorf("%v; cleanup-incomplete id=%s (output suppressed)", result, id)
		}
	}()
	result = (traceCase{name: "pipeline", code: code, phases: []tracePhase{
		{"apply-create", func() error {
			e := run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")()
			id, _ = pipelineStateID(root, env, terraform)
			return e
		}},
		{"state-id", func() error {
			if id == "" {
				return errors.New("missing ID")
			}
			return nil
		}},
		{"plan-converged", plan(0)},
		{"verify-created", func() error { return ownedPipeline(c, id, code, code) }},
		{"remote-name-drift", func() error {
			if e := ownedPipeline(c, id, code, code); e != nil {
				return e
			}
			raw, e := c.SendHttpRequest("/api/v3/update-pipeline-function", http.MethodPost, map[string]any{"funcId": id, "funcName": code + "-drift", "sourceCode": pipelineInertSource, "isAsynchronous": false, "enabled": false})
			var reply struct {
				Status *int `json:"statusCode"`
				Data   struct {
					ID string `json:"funcId"`
				} `json:"data"`
			}
			if e != nil || json.Unmarshal(raw, &reply) != nil || reply.Status == nil || *reply.Status != 200 || reply.Data.ID != id {
				return errors.New("name drift rejected")
			}
			return ownedPipeline(c, id, code, code+"-drift")
		}},
		{"plan-drift", plan(2)}, {"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")}, {"plan-reconverged", plan(0)},
		{"verify-before-destroy", func() error { return ownedPipeline(c, id, code, code) }},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			status, _, e := pipelineRead(c, id)
			if e != nil || status != 404 {
				return errors.New("pipeline absence unconfirmed")
			}
			return nil
		}},
	}}).execute()
	return result
}
func TestDestructiveLivePipelineFunctionTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if e := destructiveGuard(*destructiveLive, env); e != nil {
		t.Fatal(e)
	}
	code, e := newGroupCode()
	if e != nil {
		t.Fatal("name generation failed")
	}
	if e = runPipelineTrace(t.TempDir(), env, code); e != nil {
		t.Fatal(e)
	}
}
