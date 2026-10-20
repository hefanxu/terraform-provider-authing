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

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"terraform-provider-authing/internal/authingapi"
)

func childCode(code string) string   { return code + "-child" }
func childMarker(code string) string { return "hermesacc ownership " + childCode(code) }

// Only an exact, generated, namespace-scoped child may be removed. Never infer
// absence from an unsuccessful request or a mismatched identity.
func childExists(client *authingapi.Client, code, family string) (bool, string, error) {
	var status int
	var gotCode, gotNamespace, description, gotName, kind string
	switch family {
	case "role":
		res := client.GetRole(&dto.GetRoleDto{Code: childCode(code), Namespace: code})
		if res == nil {
			return false, "", errors.New("role GET failed")
		}
		status, gotCode, gotNamespace, description, gotName = res.StatusCode, res.Data.Code, res.Data.Namespace, res.Data.Description, res.Data.Name
	case "resource":
		res := client.GetResource(&dto.GetResourceDto{Code: childCode(code), Namespace: code})
		if res == nil {
			return false, "", errors.New("resource GET failed")
		}
		status, gotCode, gotNamespace, description, kind = res.StatusCode, res.Data.Code, res.Data.Namespace, res.Data.Description, res.Data.Type
	default:
		return false, "", errors.New("unknown family")
	}
	if status == 404 {
		return false, "", nil
	}
	if status != 200 || gotCode != childCode(code) || gotNamespace != code || (family == "role" && gotName != childCode(code)) || (family == "resource" && kind != "BUTTON") {
		return false, "", errors.New("child identity not verified")
	}
	return true, description, nil
}
func verifyOwnedPermissionChild(client *authingapi.Client, code, family string, drift bool) error {
	exists, description, err := childExists(client, code, family)
	if err != nil || !exists {
		return errors.New("child ownership not verified")
	}
	if description != childMarker(code) && (!drift || description != childMarker(code)+"-drift") {
		return errors.New("child ownership marker mismatch")
	}
	return nil
}
func verifyChildAbsent(client *authingapi.Client, code, family string) error {
	exists, _, err := childExists(client, code, family)
	if err != nil || exists {
		return errors.New("child absence not confirmed")
	}
	return nil
}

// A nonempty data-policy inventory cannot be scoped by namespace in Authing's
// API; refuse namespace deletion whenever any global data policy is reported.
// This is deliberately conservative, even when the policy may be unrelated.
func verifyChildNamespaceInventory(client *authingapi.Client, code, family string, active bool) error {
	if !sandboxCode.MatchString(code) || code == "default" || (family != "role" && family != "resource") {
		return errors.New("invalid inventory scope")
	}
	if err := verifyOwnedNamespaceIdentity(client, code); err != nil {
		return err
	}
	entries := []struct {
		path   string
		query  map[string]any
		nested bool
		own    bool
	}{
		{"/api/v3/list-permission-namespace-roles", map[string]any{"code": code, "page": 1, "limit": 1}, false, family == "role" && active},
		{"/api/v3/list-resources", map[string]any{"namespace": code, "page": 1, "limit": 1}, true, family == "resource" && active},
		{"/api/v3/list-data-resources", map[string]any{"namespaceCodes": []string{code}, "page": 1, "limit": 1}, false, false},
		{"/api/v3/list-data-policies", map[string]any{"page": 1, "limit": 1}, false, false},
	}
	for _, entry := range entries {
		body, err := client.SendHttpRequest(entry.path, http.MethodGet, entry.query)
		if err != nil {
			return errors.New("inventory failed")
		}
		var envelope struct {
			StatusCode *int            `json:"statusCode"`
			Data       json.RawMessage `json:"data"`
		}
		var data struct {
			StatusCode *int            `json:"statusCode"`
			TotalCount *int            `json:"totalCount"`
			List       json.RawMessage `json:"list"`
		}
		if json.Unmarshal(body, &envelope) != nil || envelope.StatusCode == nil || *envelope.StatusCode != 200 || json.Unmarshal(envelope.Data, &data) != nil || data.TotalCount == nil || (entry.nested && (data.StatusCode == nil || *data.StatusCode != 200)) {
			return errors.New("inventory incomplete")
		}
		if !entry.own {
			if *data.TotalCount != 0 || string(data.List) != "[]" {
				return errors.New("foreign children or policies present")
			}
			continue
		}
		var list []struct {
			Code      string `json:"code"`
			Namespace string `json:"namespace"`
		}
		if *data.TotalCount != 1 || json.Unmarshal(data.List, &list) != nil || len(list) != 1 || list[0].Code != childCode(code) || list[0].Namespace != code {
			return errors.New("child inventory not exclusive")
		}
	}
	return nil
}
func verifyOwnedNamespaceIdentity(client *authingapi.Client, code string) error {
	got := client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code})
	if got == nil || got.StatusCode != 200 || got.Data.Code != code || got.Data.Name != code || got.Data.Description != "hermesacc ownership "+code {
		return errors.New("namespace ownership not verified")
	}
	return nil
}
func cleanupPermissionChild(client *authingapi.Client, code, family string) error {
	if !sandboxCode.MatchString(code) || (family != "role" && family != "resource") {
		return errors.New("invalid cleanup identity")
	}
	exists, _, err := childExists(client, code, family)
	if err != nil {
		return err
	}
	if exists {
		if err = verifyOwnedPermissionChild(client, code, family, true); err != nil {
			return err
		}
		if err = verifyChildNamespaceInventory(client, code, family, true); err != nil {
			return err
		}
		switch family {
		case "role":
			res := client.DeleteRolesBatch(&dto.DeleteRoleDto{CodeList: []string{childCode(code)}, Namespace: code})
			if res == nil || res.StatusCode != 200 || !res.Data.Success {
				return errors.New("role cleanup rejected")
			}
		case "resource":
			res := client.DeleteResource(&dto.DeleteResourceDto{Code: childCode(code), Namespace: code})
			if res == nil || res.StatusCode != 200 || !res.Data.Success {
				return errors.New("resource cleanup rejected")
			}
		}
	}
	if err = verifyChildAbsent(client, code, family); err != nil {
		return err
	}
	got := client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code})
	if got != nil && got.StatusCode == 404 {
		return nil
	}
	if err = verifyChildNamespaceInventory(client, code, family, false); err != nil {
		return err
	}
	return cleanupNamespace(client, code, code, "hermesacc ownership "+code)
}
func runPermissionChildTrace(root string, credentials map[string]string, code, family string) (result error) {
	if !sandboxCode.MatchString(code) || (family != "role" && family != "resource") {
		return errors.New("child tracer requires a generated namespace and known family")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("%s phase=terraform-cli code=%s (output suppressed)", family, code)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("%s phase=go-toolchain code=%s (output suppressed)", family, code)
		}
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("%s phase=client code=%s (output suppressed)", family, code)
	}
	existing := client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code})
	if existing == nil || existing.StatusCode != 404 {
		return fmt.Errorf("%s phase=preflight-namespace code=%s (output suppressed)", family, code)
	}
	if err = verifyChildAbsent(client, code, family); err != nil {
		return fmt.Errorf("%s phase=preflight-child code=%s (output suppressed)", family, code)
	}
	started := false
	defer func() {
		if started {
			if err := cleanupPermissionChild(client, code, family); err != nil {
				result = fmt.Errorf("%s phase=cleanup-incomplete code=%s child=%s (output suppressed)", family, code, childCode(code))
			}
		}
	}()
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("%s phase=workspace code=%s (output suppressed)", family, code)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err = build.CombinedOutput(); err != nil {
		return fmt.Errorf("%s phase=provider-build code=%s (output suppressed)", family, code)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	childHCL := fmt.Sprintf("resource \"authing_role\" \"sandbox\" {\n  code = %q\n  name = %q\n  namespace = authing_namespace.sandbox.code\n  description = %q\n}\n", childCode(code), childCode(code), childMarker(code))
	if family == "resource" {
		childHCL = fmt.Sprintf("resource \"authing_resource\" \"sandbox\" {\n  code = %q\n  type = \"BUTTON\"\n  namespace = authing_namespace.sandbox.code\n  description = %q\n  actions = []\n}\n", childCode(code), childMarker(code))
	}
	hcl := fmt.Sprintf("terraform {\n  required_providers { authing = { source = %q } }\n}\nprovider \"authing\" {}\nresource \"authing_namespace\" \"sandbox\" {\n  code = %q\n  name = %q\n  description = %q\n}\n%s", source, code, code, "hermesacc ownership "+code, childHCL)
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("%s phase=config code=%s (output suppressed)", family, code)
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	childTarget := "authing_" + family + ".sandbox"
	started = true
	return (traceCase{name: family, code: code, phases: []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-converged", plan(0)},
		{"verify-created", func() error {
			if err := verifyOwnedPermissionChild(client, code, family, false); err != nil {
				return err
			}
			return verifyChildNamespaceInventory(client, code, family, true)
		}},
		{"remote-drift", func() error {
			if err := verifyOwnedPermissionChild(client, code, family, false); err != nil {
				return err
			}
			if family == "role" {
				res := client.UpdateRole(&dto.UpdateRoleDto{Code: childCode(code), NewCode: childCode(code), Name: childCode(code), Namespace: code, Description: childMarker(code) + "-drift"})
				if res == nil || res.StatusCode != 200 {
					return errors.New("drift mutation rejected")
				}
			} else {
				res := client.UpdateResource(&dto.UpdateResourceDto{Code: childCode(code), Namespace: code, Type: "BUTTON", Actions: []dto.ResourceAction{}, Description: childMarker(code) + "-drift"})
				if res == nil || res.StatusCode != 200 {
					return errors.New("drift mutation rejected")
				}
			}
			exists, description, err := childExists(client, code, family)
			if err != nil || !exists || description != childMarker(code)+"-drift" {
				return errors.New("drift not visible")
			}
			return nil
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-before-destroy", func() error {
			if err := verifyOwnedPermissionChild(client, code, family, false); err != nil {
				return err
			}
			return verifyChildNamespaceInventory(client, code, family, true)
		}},
		{"destroy-child", run(0, "destroy", "-target="+childTarget, "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-child-absent", func() error {
			if err := verifyChildAbsent(client, code, family); err != nil {
				return err
			}
			return verifyChildNamespaceInventory(client, code, family, false)
		}},
		{"destroy-namespace", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			if err := verifyChildAbsent(client, code, family); err != nil {
				return err
			}
			got := client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code})
			if got == nil || got.StatusCode != 404 {
				return errors.New("namespace absence not confirmed")
			}
			return nil
		}},
	}}).execute()
}
