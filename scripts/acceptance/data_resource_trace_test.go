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

func dataResourceCode(ns string) string   { return ns + "-data" }
func dataResourceMarker(ns string) string { return "hermesacc ownership " + dataResourceCode(ns) }

// A failed GET is not absence. Require exact identity, empty structure/actions,
// and no extensions before any mutation or deletion; no data rows are created.
func sandboxDataResource(client *authingapi.Client, ns string) (bool, string, error) {
	raw, err := client.SendHttpRequest("/api/v3/get-data-resource", http.MethodGet, map[string]string{"namespaceCode": ns, "resourceCode": dataResourceCode(ns)})
	if err != nil {
		return false, "", errors.New("data resource GET failed")
	}
	var envelope struct {
		StatusCode *int            `json:"statusCode"`
		Data       json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.StatusCode == nil {
		return false, "", errors.New("invalid data resource response")
	}
	if *envelope.StatusCode == 404 {
		return false, "", nil
	}
	var got struct {
		NamespaceCode   string          `json:"namespaceCode"`
		ResourceCode    string          `json:"resourceCode"`
		ResourceName    string          `json:"resourceName"`
		Description     string          `json:"description"`
		Type            string          `json:"type"`
		Struct          json.RawMessage `json:"struct"`
		Actions         json.RawMessage `json:"actions"`
		ExtendFieldList json.RawMessage `json:"extendFieldList"`
	}
	if *envelope.StatusCode != 200 || json.Unmarshal(envelope.Data, &got) != nil || got.NamespaceCode != ns || got.ResourceCode != dataResourceCode(ns) || got.Type != "ARRAY" || (got.ResourceName != dataResourceCode(ns) && got.ResourceName != dataResourceCode(ns)+"-drift") || got.Description != dataResourceMarker(ns) || string(got.Struct) != "[]" || string(got.Actions) != "[]" || (len(got.ExtendFieldList) != 0 && string(got.ExtendFieldList) != "[]") {
		return false, "", errors.New("data resource identity, ownership or empty contents not verified")
	}
	return true, got.ResourceName, nil
}

// Global policies cannot be scoped reliably; any policy blocks cleanup.
// An active namespace must contain only our exact data resource and no other children.
func verifyDataResourceInventory(client *authingapi.Client, ns string, active bool) error {
	if err := verifyOwnedNamespaceIdentity(client, ns); err != nil {
		return err
	}
	for _, entry := range []struct {
		path        string
		query       map[string]any
		nested, own bool
	}{
		{"/api/v3/list-permission-namespace-roles", map[string]any{"code": ns, "page": 1, "limit": 1}, false, false},
		{"/api/v3/list-resources", map[string]any{"namespace": ns, "page": 1, "limit": 1}, true, false},
		{"/api/v3/list-data-resources", map[string]any{"namespaceCodes": []string{ns}, "page": 1, "limit": 1}, false, active},
		{"/api/v3/list-data-policies", map[string]any{"page": 1, "limit": 1}, false, false},
	} {
		raw, err := client.SendHttpRequest(entry.path, http.MethodGet, entry.query)
		if err != nil {
			return errors.New("data resource inventory failed")
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
		if json.Unmarshal(raw, &envelope) != nil || envelope.StatusCode == nil || *envelope.StatusCode != 200 || json.Unmarshal(envelope.Data, &data) != nil || data.TotalCount == nil || (entry.nested && (data.StatusCode == nil || *data.StatusCode != 200)) {
			return errors.New("data resource inventory incomplete")
		}
		if !entry.own {
			if *data.TotalCount != 0 || string(data.List) != "[]" {
				return errors.New("dependent policies or foreign children present")
			}
			continue
		}
		var list []struct {
			NamespaceCode string `json:"namespaceCode"`
			ResourceCode  string `json:"resourceCode"`
		}
		if *data.TotalCount != 1 || json.Unmarshal(data.List, &list) != nil || len(list) != 1 || list[0].NamespaceCode != ns || list[0].ResourceCode != dataResourceCode(ns) {
			return errors.New("data resource inventory not exclusive")
		}
	}
	return nil
}

func cleanupDataResource(client *authingapi.Client, ns string) error {
	if !sandboxCode.MatchString(ns) {
		return errors.New("invalid data resource cleanup identity")
	}
	exists, _, err := sandboxDataResource(client, ns)
	if err != nil {
		return err
	}
	if exists {
		if err = verifyDataResourceInventory(client, ns, true); err != nil {
			return err
		}
		raw, e := client.SendHttpRequest("/api/v3/delete-data-resource", http.MethodPost, map[string]string{"namespaceCode": ns, "resourceCode": dataResourceCode(ns)})
		var res struct {
			StatusCode *int `json:"statusCode"`
			Data       struct {
				Success *bool `json:"success"`
			} `json:"data"`
		}
		if e != nil || json.Unmarshal(raw, &res) != nil || res.StatusCode == nil || *res.StatusCode != 200 || (res.Data.Success != nil && !*res.Data.Success) {
			return errors.New("data resource delete rejected")
		}
	}
	if exists, _, err = sandboxDataResource(client, ns); err != nil || exists {
		return errors.New("data resource absence not confirmed")
	}
	got := client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: ns})
	if got != nil && got.StatusCode == 404 {
		return nil
	}
	if err = verifyDataResourceInventory(client, ns, false); err != nil {
		return err
	}
	return cleanupNamespace(client, ns, ns, "hermesacc ownership "+ns)
}

func runDataResourceTrace(root string, credentials map[string]string, ns string) (result error) {
	if !sandboxCode.MatchString(ns) {
		return errors.New("data resource tracer requires generated namespace code")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("data-resource phase=terraform-cli code=%s (output suppressed)", ns)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("data-resource phase=go-toolchain code=%s (output suppressed)", ns)
		}
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("data-resource phase=client code=%s (output suppressed)", ns)
	}
	namespace := client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: ns})
	if namespace == nil || namespace.StatusCode != 404 {
		return fmt.Errorf("data-resource phase=preflight-namespace code=%s (output suppressed)", ns)
	}
	exists, _, err := sandboxDataResource(client, ns)
	if err != nil || exists {
		return fmt.Errorf("data-resource phase=preflight-resource code=%s (output suppressed)", ns)
	}
	started := false
	defer func() {
		if started {
			if err := cleanupDataResource(client, ns); err != nil {
				result = fmt.Errorf("data-resource phase=cleanup-incomplete code=%s resource=%s (output suppressed)", ns, dataResourceCode(ns))
			}
		}
	}()
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("data-resource phase=workspace code=%s (output suppressed)", ns)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err = build.CombinedOutput(); err != nil {
		return fmt.Errorf("data-resource phase=provider-build code=%s (output suppressed)", ns)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	hcl := fmt.Sprintf(`terraform {
 required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_namespace" "sandbox" {
 code = %q
 name = %q
 description = %q
}
resource "authing_data_resource" "sandbox" {
 namespace_code = authing_namespace.sandbox.code
 resource_code = %q
 resource_name = %q
 type = "ARRAY"
 struct = jsonencode([])
 actions = []
 description = %q
}
`, source, ns, ns, "hermesacc ownership "+ns, dataResourceCode(ns), dataResourceCode(ns), dataResourceMarker(ns))
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("data-resource phase=config code=%s (output suppressed)", ns)
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	verify := func(name string) error {
		exists, got, err := sandboxDataResource(client, ns)
		if err != nil || !exists || got != name {
			return errors.New("data resource ownership not verified")
		}
		return verifyDataResourceInventory(client, ns, true)
	}
	started = true
	return (traceCase{name: "data-resource", code: ns, phases: []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-converged", plan(0)},
		{"verify-created", func() error { return verify(dataResourceCode(ns)) }},
		{"remote-drift", func() error {
			if err := verify(dataResourceCode(ns)); err != nil {
				return err
			}
			raw, e := client.SendHttpRequest("/api/v3/update-data-resource", http.MethodPost, map[string]any{"namespaceCode": ns, "resourceCode": dataResourceCode(ns), "resourceName": dataResourceCode(ns) + "-drift", "struct": []string{}, "actions": []string{}, "description": dataResourceMarker(ns)})
			var response struct {
				StatusCode *int `json:"statusCode"`
			}
			if e != nil || json.Unmarshal(raw, &response) != nil || response.StatusCode == nil || *response.StatusCode != 200 {
				return errors.New("data resource drift mutation rejected")
			}
			return verify(dataResourceCode(ns) + "-drift")
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-before-destroy", func() error { return verify(dataResourceCode(ns)) }},
		{"destroy-resource", run(0, "destroy", "-target=authing_data_resource.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-resource-absent", func() error {
			exists, _, err := sandboxDataResource(client, ns)
			if err != nil || exists {
				return errors.New("data resource absence not confirmed")
			}
			return verifyDataResourceInventory(client, ns, false)
		}},
		{"destroy-namespace", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			exists, _, err := sandboxDataResource(client, ns)
			if err != nil || exists {
				return errors.New("data resource absence not confirmed")
			}
			got := client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: ns})
			if got == nil || got.StatusCode != 404 {
				return errors.New("namespace absence not confirmed")
			}
			return nil
		}},
	}}).execute()
}
