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

func policyName(code string) string       { return code + "-policy" }
func policyMarker(code string) string     { return "hermesacc ownership " + policyName(code) }
func policyPermission(code string) string { return code + "/" + dataResourceCode(code) + "/read" }

// The published GET DTO does not expose statementList. Require an explicit,
// complete statementList in the raw response; absent/null is NOT an empty list.
func ownedPolicy(c *authingapi.Client, code, id string) (bool, string, error) {
	if id == "" {
		return false, "", errors.New("missing state-backed policy ID")
	}
	raw, err := c.SendHttpRequest("/api/v3/get-data-policy", http.MethodGet, map[string]string{"policyId": id})
	if err != nil {
		return false, "", errors.New("policy GET failed")
	}
	var envelope struct {
		StatusCode *int            `json:"statusCode"`
		Data       json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.StatusCode == nil {
		return false, "", errors.New("malformed policy GET")
	}
	if *envelope.StatusCode == 404 {
		return false, "", nil
	}
	var data struct {
		PolicyID    string          `json:"policyId"`
		Name        string          `json:"policyName"`
		Description string          `json:"description"`
		Statements  json.RawMessage `json:"statementList"`
	}
	if *envelope.StatusCode != 200 || json.Unmarshal(envelope.Data, &data) != nil || data.PolicyID != id || data.Description != policyMarker(code) || (data.Name != policyName(code) && data.Name != policyName(code)+"-hcl" && data.Name != policyName(code)+"-drift") {
		return false, "", errors.New("policy identity not verified")
	}
	// A missing field may be omitted by the server, not proof that permissions are empty.
	if len(data.Statements) == 0 || string(data.Statements) == "null" {
		return false, "", errors.New("complete policy statements unavailable")
	}
	var statements []struct {
		Effect      string   `json:"effect"`
		Permissions []string `json:"permissions"`
	}
	if json.Unmarshal(data.Statements, &statements) != nil || len(statements) != 1 || statements[0].Effect != "DENY" || len(statements[0].Permissions) != 1 || statements[0].Permissions[0] != policyPermission(code) {
		return false, "", errors.New("policy statements differ from owned permission")
	}
	return true, data.Name, nil
}

// Read every target page before deletion: a matching entry on an early page
// cannot justify ignoring later foreign entries or a truncated inventory.
func emptyPolicyTargets(c *authingapi.Client, id string) error {
	const limit = 50
	for page, seen := 1, 0; page <= 100; page++ {
		raw, err := c.SendHttpRequest("/api/v3/list-data-policy-targets", http.MethodGet, map[string]any{"policyId": id, "page": page, "limit": limit})
		if err != nil {
			return errors.New("target inventory failed")
		}
		var env struct {
			StatusCode *int `json:"statusCode"`
			Data       struct {
				TotalCount *int            `json:"totalCount"`
				List       json.RawMessage `json:"list"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &env) != nil || env.StatusCode == nil || *env.StatusCode != 200 || env.Data.TotalCount == nil || *env.Data.TotalCount < 0 {
			return errors.New("target inventory incomplete")
		}
		var entries []json.RawMessage
		if json.Unmarshal(env.Data.List, &entries) != nil || entries == nil || len(entries) > limit || seen+len(entries) > *env.Data.TotalCount {
			return errors.New("target inventory malformed")
		}
		if len(entries) > 0 {
			return errors.New("policy still has targets")
		}
		seen += len(entries)
		if seen == *env.Data.TotalCount {
			return nil
		}
		if len(entries) < limit {
			return errors.New("target inventory truncated")
		}
	}
	return errors.New("target inventory pagination overflow")
}

// Only a state-backed exact ID may be removed; a failed apply without ID is
// intentionally left for manual inspection, never guessed by policy name.
func removeOwnedPolicy(c *authingapi.Client, code, id string) error {
	exists, _, err := ownedPolicy(c, code, id)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err = emptyPolicyTargets(c, id); err != nil {
		return err
	}
	raw, requestErr := c.SendHttpRequest("/api/v3/delete-data-policy", http.MethodPost, map[string]string{"policyId": id})
	var response struct {
		StatusCode *int `json:"statusCode"`
		Data       struct {
			Success *bool `json:"success"`
		} `json:"data"`
	}
	if requestErr != nil || json.Unmarshal(raw, &response) != nil || response.StatusCode == nil || *response.StatusCode != 200 || response.Data.Success != nil && !*response.Data.Success {
		return errors.New("policy deletion rejected")
	}
	exists, _, err = ownedPolicy(c, code, id)
	if err != nil || exists {
		return errors.New("policy exact-ID absence not confirmed")
	}
	return nil
}

func policyPrerequisitesSafe(c *authingapi.Client, code string) error {
	if err := verifyOwnedNamespaceIdentity(c, code); err != nil {
		return err
	}
	raw, err := c.SendHttpRequest("/api/v3/get-data-resource", http.MethodGet, map[string]string{"namespaceCode": code, "resourceCode": dataResourceCode(code)})
	if err != nil {
		return errors.New("prerequisite GET failed")
	}
	var got struct {
		StatusCode *int `json:"statusCode"`
		Data       struct {
			NamespaceCode   string          `json:"namespaceCode"`
			ResourceCode    string          `json:"resourceCode"`
			ResourceName    string          `json:"resourceName"`
			Description     string          `json:"description"`
			Type            string          `json:"type"`
			Struct          json.RawMessage `json:"struct"`
			Actions         json.RawMessage `json:"actions"`
			ExtendFieldList json.RawMessage `json:"extendFieldList"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &got) != nil || got.StatusCode == nil || *got.StatusCode != 200 || got.Data.NamespaceCode != code || got.Data.ResourceCode != dataResourceCode(code) || got.Data.ResourceName != dataResourceCode(code) || got.Data.Description != dataResourceMarker(code) || got.Data.Type != "ARRAY" || string(got.Data.Struct) != "[]" || string(got.Data.Actions) != `["read"]` || (len(got.Data.ExtendFieldList) > 0 && string(got.Data.ExtendFieldList) != "[]") {
		return errors.New("prerequisite identity or contents changed")
	}
	for _, q := range []struct {
		path          string
		params        map[string]any
		nested, owned bool
	}{
		{"/api/v3/list-permission-namespace-roles", map[string]any{"code": code, "page": 1, "limit": 1}, false, false},
		{"/api/v3/list-resources", map[string]any{"namespace": code, "page": 1, "limit": 1}, true, false},
		{"/api/v3/list-data-resources", map[string]any{"namespaceCodes": []string{code}, "page": 1, "limit": 1}, false, true},
		{"/api/v3/list-data-policies", map[string]any{"page": 1, "limit": 1}, false, false},
	} {
		b, e := c.SendHttpRequest(q.path, http.MethodGet, q.params)
		if e != nil {
			return errors.New("prerequisite inventory failed")
		}
		var response struct {
			StatusCode *int `json:"statusCode"`
			Data       struct {
				StatusCode *int            `json:"statusCode"`
				TotalCount *int            `json:"totalCount"`
				List       json.RawMessage `json:"list"`
			} `json:"data"`
		}
		if json.Unmarshal(b, &response) != nil || response.StatusCode == nil || *response.StatusCode != 200 || response.Data.TotalCount == nil || q.nested && (response.Data.StatusCode == nil || *response.Data.StatusCode != 200) {
			return errors.New("prerequisite inventory incomplete")
		}
		if !q.owned {
			if *response.Data.TotalCount != 0 || string(response.Data.List) != "[]" {
				return errors.New("foreign prerequisite dependency")
			}
			continue
		}
		var entries []struct {
			NamespaceCode string `json:"namespaceCode"`
			ResourceCode  string `json:"resourceCode"`
		}
		if *response.Data.TotalCount != 1 || json.Unmarshal(response.Data.List, &entries) != nil || len(entries) != 1 || entries[0].NamespaceCode != code || entries[0].ResourceCode != dataResourceCode(code) {
			return errors.New("prerequisite inventory not exclusive")
		}
	}
	return nil
}

// Refuse a name collision before creating any prerequisite. Check every page;
// a failed filtered list cannot be treated as proof of absence.
func policyNameAvailable(c *authingapi.Client, name string) error {
	const limit = 50
	for page, seen := 1, 0; page <= 100; page++ {
		raw, err := c.SendHttpRequest("/api/v3/list-data-policies", http.MethodGet, map[string]any{"query": name, "page": page, "limit": limit})
		if err != nil {
			return errors.New("policy name preflight failed")
		}
		var response struct {
			StatusCode *int `json:"statusCode"`
			Data       struct {
				TotalCount *int            `json:"totalCount"`
				List       json.RawMessage `json:"list"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &response) != nil || response.StatusCode == nil || *response.StatusCode != 200 || response.Data.TotalCount == nil || *response.Data.TotalCount < 0 {
			return errors.New("policy name inventory incomplete")
		}
		var list []struct {
			PolicyID   string `json:"policyId"`
			PolicyName string `json:"policyName"`
		}
		if json.Unmarshal(response.Data.List, &list) != nil || list == nil || len(list) > limit || seen+len(list) > *response.Data.TotalCount {
			return errors.New("policy name inventory malformed")
		}
		for _, item := range list {
			if item.PolicyID == "" || item.PolicyName == "" {
				return errors.New("policy name inventory malformed")
			}
			if item.PolicyName == name {
				return errors.New("policy name already exists")
			}
		}
		seen += len(list)
		if seen == *response.Data.TotalCount {
			return nil
		}
		if len(list) < limit {
			return errors.New("policy name inventory truncated")
		}
	}
	return errors.New("policy name inventory pagination overflow")
}

func policyStateID(root string) string {
	// Terraform's state JSON is local to this isolated run; never print its contents.
	b, err := os.ReadFile(filepath.Join(root, "example", "terraform.tfstate"))
	if err != nil {
		return ""
	}
	var state struct {
		Resources []struct {
			Type      string `json:"type"`
			Name      string `json:"name"`
			Instances []struct {
				Attributes struct {
					ID string `json:"id"`
				} `json:"attributes"`
			} `json:"instances"`
		} `json:"resources"`
	}
	if json.Unmarshal(b, &state) != nil {
		return ""
	}
	for _, r := range state.Resources {
		if r.Type == "authing_data_policy" && r.Name == "sandbox" && len(r.Instances) == 1 {
			return r.Instances[0].Attributes.ID
		}
	}
	return ""
}

func runDataPolicyTrace(root string, creds map[string]string, code string) (result error) {
	if !sandboxCode.MatchString(code) {
		return errors.New("invalid generated policy code")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("data-policy phase=terraform-cli code=%s (output suppressed)", code)
	}
	goBinary := "/home/azureuser/workplace/.tools/go/bin/go"
	if _, err := os.Stat(goBinary); err != nil {
		goBinary = "/home/azureuser/.local/go/bin/go"
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("data-policy phase=go-toolchain code=%s (output suppressed)", code)
		}
	}
	c, err := authingapi.NewClient(authingapi.Options{AccessKeyID: creds["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: creds["AUTHING_ACCESS_KEY_SECRET"], Host: creds["AUTHING_HOST"]})
	if err != nil {
		return errors.New("data-policy client setup failed (output suppressed)")
	}
	ns := c.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code})
	if ns == nil || ns.StatusCode != 404 {
		return fmt.Errorf("data-policy phase=preflight-namespace code=%s (output suppressed)", code)
	}
	child, _, err := sandboxDataResource(c, code)
	if err != nil || child {
		return fmt.Errorf("data-policy phase=preflight-resource code=%s (output suppressed)", code)
	}
	if err := policyNameAvailable(c, policyName(code)); err != nil {
		return fmt.Errorf("data-policy phase=preflight-policy code=%s (output suppressed)", code)
	}
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return errors.New("data-policy workspace failed (output suppressed)")
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err = build.CombinedOutput(); err != nil {
		return errors.New("data-policy provider build failed (output suppressed)")
	}
	rcPath := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n dev_overrides { %q = %q }\n direct {}\n}\n", source, providerDir)
	template := `terraform {
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
 actions = ["read"]
 description = %q
}
resource "authing_data_policy" "sandbox" {
 policy_name = %q
 description = %q
 statement_list = [{ effect = "DENY", permissions = ["${authing_data_resource.sandbox.namespace_code}/${authing_data_resource.sandbox.resource_code}/read"] }]
}
`
	config := func(name string) string {
		return fmt.Sprintf(template, source, code, code, "hermesacc ownership "+code, dataResourceCode(code), dataResourceCode(code), dataResourceMarker(code), name, policyMarker(code))
	}
	hclPath := filepath.Join(exampleDir, "main.tf")
	if os.WriteFile(rcPath, []byte(rc), 0600) != nil || os.WriteFile(hclPath, []byte(config(policyName(code))), 0600) != nil {
		return errors.New("data-policy config failed (output suppressed)")
	}
	env := traceEnvironment(root, rcPath, creds)
	probe := exec.Command(terraform, "validate", "-no-color")
	probe.Dir = exampleDir
	probe.Env = env
	if output, e := probe.CombinedOutput(); e != nil {
		category := "unknown"
		for _, heading := range []string{"Unsupported argument", "Invalid reference", "Incorrect attribute value type", "Missing required argument", "Invalid expression", "Invalid provider configuration", "Failed to load plugin schemas", "Unsupported block type", "Invalid resource type", "Missing required provider", "Provider installation not found", "Invalid character", "Missing attribute separator", "Invalid block definition", "Invalid multi-line string", "Invalid template interpolation value", "Invalid attribute name", "Inconsistent dependency lock file", "Failed to query available provider packages"} {
			if strings.Contains(string(output), "Error: "+heading) {
				category = heading
				break
			}
		}
		return fmt.Errorf("data-policy phase=validate category=%s code=%s (output suppressed)", category, code)
	}
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	verify := func(name string) error {
		id := policyStateID(root)
		exists, got, e := ownedPolicy(c, code, id)
		if e != nil || !exists || got != name {
			return errors.New("owned policy not verified")
		}
		return emptyPolicyTargets(c, id)
	}
	defer func() {
		id := policyStateID(root)
		if id == "" {
			if result != nil {
				result = fmt.Errorf("%w; cleanup=manual-review code=%s (policy ID unavailable)", result, code)
			}
			return
		}
		if e := removeOwnedPolicy(c, code, id); e != nil {
			if result == nil {
				result = fmt.Errorf("data-policy phase=cleanup-incomplete code=%s (output suppressed)", code)
			} else {
				result = fmt.Errorf("%w; cleanup=incomplete code=%s", result, code)
			}
			return
		}
		if result != nil {
			result = fmt.Errorf("%w; cleanup=manual-review code=%s (prerequisites not removed)", result, code)
		}
		// Do not guess or silently remove untracked prerequisites after a failure.
	}()
	return (traceCase{name: "data-policy", code: code, phases: []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-converged", plan(0)},
		{"verify-created", func() error { return verify(policyName(code)) }},
		{"hcl-name-change", func() error { return os.WriteFile(hclPath, []byte(config(policyName(code)+"-hcl")), 0600) }},
		{"plan-hcl-update", plan(2)},
		{"apply-hcl-update", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-hcl-update", func() error { return verify(policyName(code) + "-hcl") }},
		{"plan-updated", plan(0)},
		{"remote-name-drift", func() error {
			if err := verify(policyName(code) + "-hcl"); err != nil {
				return err
			}
			res := c.UpdateDataPolicy(&dto.UpdateDataPolicyDto{PolicyId: policyStateID(root), PolicyName: policyName(code) + "-drift", Description: policyMarker(code), StatementList: []dto.DataStatementPermissionDto{{Effect: "DENY", Permissions: []string{policyPermission(code)}}}})
			if res == nil || res.StatusCode != 200 {
				return errors.New("policy drift mutation rejected")
			}
			return verify(policyName(code) + "-drift")
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-before-destroy", func() error { return verify(policyName(code) + "-hcl") }},
		{"destroy-policy", func() error {
			id := policyStateID(root)
			exists, _, e := ownedPolicy(c, code, id)
			if e != nil || !exists {
				return errors.New("policy ownership not confirmed for Terraform destroy")
			}
			if e := emptyPolicyTargets(c, id); e != nil {
				return e
			}
			if e := terraformExit(root, env, terraform, 0, "destroy", "-target=authing_data_policy.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color"); e != nil {
				return e
			}
			exists, _, e = ownedPolicy(c, code, id)
			if e != nil || exists {
				return errors.New("policy exact-ID absence not confirmed")
			}
			return nil
		}},
		{"verify-policy-absent", func() error {
			id := policyStateID(root)
			if id != "" {
				return errors.New("policy state still present")
			}
			return nil
		}},
		{"destroy-prerequisites", func() error {
			if e := policyPrerequisitesSafe(c, code); e != nil {
				return e
			}
			return terraformExit(root, env, terraform, 0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")
		}},
		{"verify-absent", func() error {
			r, _, e := sandboxDataResource(c, code)
			if e != nil || r {
				return errors.New("data resource remains")
			}
			ns := c.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: code})
			if ns == nil || ns.StatusCode != 404 {
				return errors.New("namespace remains")
			}
			return nil
		}},
	}}).execute()
}
