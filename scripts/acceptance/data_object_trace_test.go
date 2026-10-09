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
)

func objectMarker(code string) string { return "hermesacc ownership " + code }

type objectResponse struct {
	StatusCode *int            `json:"statusCode"`
	Data       json.RawMessage `json:"data"`
}

func objectRequest(c *authingapi.Client, path, method string, query any) (objectResponse, error) {
	raw, err := c.SendHttpRequest(path, method, query)
	if err != nil {
		return objectResponse{}, errors.New("metadata request failed")
	}
	var response objectResponse
	if json.Unmarshal(raw, &response) != nil || response.StatusCode == nil {
		return response, errors.New("invalid metadata response")
	}
	return response, nil
}

type ownedModel struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Type        string          `json:"type"`
	DataType    string          `json:"dataType"`
	ParentKey   string          `json:"parentKey"`
	Enable      *bool           `json:"enable"`
	FieldOrder  json.RawMessage `json:"fieldOrder"`
	Config      json.RawMessage `json:"config"`
}

func validObject(m ownedModel, code string) bool {
	return m.ID != "" && (m.Name == code || m.Name == code+"-updated" || m.Name == code+"-updated-drift") && m.Description == objectMarker(code) && m.Type == "custom" && m.DataType == "list" && m.ParentKey == "" && m.Enable != nil && *m.Enable
}

// The list is needed to recover the server-assigned ID when create succeeded but
// Terraform did not record state. Never infer ownership from a guessed ID.
func discoverObject(c *authingapi.Client, code string) (string, error) {
	if !sandboxCode.MatchString(code) {
		return "", errors.New("invalid model code")
	}
	raw, err := c.SendHttpRequest("/api/v3/metadata/list-model", http.MethodGet, nil)
	if err != nil {
		return "", errors.New("model inventory failed")
	}
	var envelope struct {
		StatusCode *int            `json:"statusCode"`
		List       json.RawMessage `json:"list"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.StatusCode == nil || *envelope.StatusCode != 200 || len(envelope.List) == 0 {
		return "", errors.New("model inventory incomplete")
	}
	var entries []json.RawMessage
	if json.Unmarshal(envelope.List, &entries) != nil || entries == nil {
		return "", errors.New("model inventory malformed")
	}
	id := ""
	for _, item := range entries {
		var m ownedModel
		if json.Unmarshal(item, &m) != nil || m.ID == "" {
			return "", errors.New("model inventory malformed")
		}
		if m.Name != code && m.Name != code+"-updated" && m.Name != code+"-updated-drift" && m.Description != objectMarker(code) {
			continue
		}
		if !validObject(m, code) || id != "" {
			return "", errors.New("model identity ambiguous")
		}
		id = m.ID
	}
	return id, nil
}
func readOwnedObject(c *authingapi.Client, code, id string) (bool, string, error) {
	if !sandboxCode.MatchString(code) || id == "" {
		return false, "", errors.New("invalid model identity")
	}
	response, err := objectRequest(c, "/api/v3/metadata/get-model", http.MethodGet, map[string]string{"id": id})
	if err != nil {
		return false, "", err
	}
	if *response.StatusCode == 404 {
		return false, "", nil
	}
	var m ownedModel
	if *response.StatusCode != 200 || json.Unmarshal(response.Data, &m) != nil || m.ID != id || !validObject(m, code) {
		return false, "", errors.New("model ownership not verified")
	}
	return true, m.Name, nil
}
func verifyObjectEmpty(c *authingapi.Client, id string) error {
	// list-field has no pagination. A non-empty list is a hard stop, including
	// fields created out of band after Terraform's last refresh.
	fields, err := objectRequest(c, "/api/v3/metadata/list-field", http.MethodGet, map[string]string{"modelId": id, "from": "terraform"})
	if err != nil || *fields.StatusCode != 200 || string(fields.Data) != "[]" {
		return errors.New("model field inventory not proven empty")
	}
	// The filter response carries totalCount; require both count and first page
	// to be empty so a truncated or malformed response cannot authorize deletion.
	rows, err := objectRequest(c, "/api/v3/metadata/filter", http.MethodPost, map[string]any{"modelId": id, "page": 1, "limit": 1, "fetchAll": false})
	if err != nil || *rows.StatusCode != 200 {
		return errors.New("model row inventory failed")
	}
	var data struct {
		TotalCount *int            `json:"totalCount"`
		List       json.RawMessage `json:"list"`
	}
	if json.Unmarshal(rows.Data, &data) != nil || data.TotalCount == nil || *data.TotalCount != 0 || string(data.List) != "[]" {
		return errors.New("model row inventory not proven empty")
	}
	return nil
}
func cleanupObject(c *authingapi.Client, code, expectedID string) error {
	if !sandboxCode.MatchString(code) {
		return errors.New("invalid cleanup code")
	}
	id, err := discoverObject(c, code)
	if err != nil {
		return err
	}
	if id == "" {
		if expectedID != "" {
			exists, _, e := readOwnedObject(c, code, expectedID)
			if e != nil || exists {
				return errors.New("model absence not confirmed")
			}
		}
		return nil
	}
	if expectedID != "" && id != expectedID {
		return errors.New("model ID changed")
	}
	exists, _, err := readOwnedObject(c, code, id)
	if err != nil || !exists {
		return errors.New("model GET identity unverified")
	}
	if err = verifyObjectEmpty(c, id); err != nil {
		return err
	}
	reply, err := objectRequest(c, "/api/v3/metadata/remove-model", http.MethodPost, map[string]string{"id": id})
	if err != nil || *reply.StatusCode != 200 {
		return errors.New("model deletion rejected")
	}
	exists, _, err = readOwnedObject(c, code, id)
	if err != nil || exists {
		return errors.New("model exact-ID absence unconfirmed")
	}
	remaining, err := discoverObject(c, code)
	if err != nil || remaining != "" {
		return errors.New("model inventory absence unconfirmed")
	}
	return nil
}
func runObjectTrace(root string, credentials map[string]string, code string) (result error) {
	if !sandboxCode.MatchString(code) {
		return errors.New("model tracer requires generated code")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("model phase=terraform-cli code=%s (output suppressed)", code)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("model phase=go-toolchain code=%s (output suppressed)", code)
		}
	}
	c, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("model phase=client code=%s (output suppressed)", code)
	}
	id, err := discoverObject(c, code)
	if err != nil || id != "" {
		return fmt.Errorf("model phase=preflight code=%s (output suppressed)", code)
	}
	started := false
	defer func() {
		if started && cleanupObject(c, code, id) != nil {
			result = fmt.Errorf("model phase=cleanup-incomplete code=%s id=%s (output suppressed; manual inspection required)", code, id)
		}
	}()
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("model phase=workspace code=%s (output suppressed)", code)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err = build.CombinedOutput(); err != nil {
		return fmt.Errorf("model phase=provider-build code=%s (output suppressed)", code)
	}
	config := filepath.Join(root, "terraform.rc")
	tfFile := filepath.Join(exampleDir, "main.tf")
	rc := fmt.Sprintf("provider_installation {\n dev_overrides {\n %q = %q\n }\n direct {}\n}\n", source, providerDir)
	hcl := func(name string) string {
		return fmt.Sprintf(`terraform {
 required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_data_object" "sandbox" {
 name = %q
 description = %q
 type = "custom"
 data_type = "list"
 parent_key = ""
 enable = true
 show_field_key = ""
}
`, source, name, objectMarker(code))
	}
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(tfFile, []byte(hcl(code)), 0600) != nil {
		return fmt.Errorf("model phase=config code=%s (output suppressed)", code)
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	verify := func(name string) error {
		found, e := discoverObject(c, code)
		if e != nil || found == "" {
			return errors.New("model not uniquely discoverable")
		}
		if id != "" && id != found {
			return errors.New("model ID changed")
		}
		id = found
		exists, got, e := readOwnedObject(c, code, id)
		if e != nil || !exists || got != name {
			return errors.New("model name or identity not verified")
		}
		return verifyObjectEmpty(c, id)
	}
	started = true
	return (traceCase{name: "model", code: code, phases: []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-converged", plan(0)}, {"verify-created", func() error { return verify(code) }},
		{"configure-name", func() error { return os.WriteFile(tfFile, []byte(hcl(code+"-updated")), 0600) }},
		{"plan-name", plan(2)}, {"apply-name", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-name", func() error { return verify(code + "-updated") }}, {"plan-updated", plan(0)},
		{"remote-drift", func() error {
			if err := verify(code + "-updated"); err != nil {
				return err
			}
			current, e := objectRequest(c, "/api/v3/metadata/get-model", http.MethodGet, map[string]string{"id": id})
			if e != nil {
				return e
			}
			var model ownedModel
			if json.Unmarshal(current.Data, &model) != nil || model.ID != id {
				return errors.New("model metadata missing")
			}
			var config any
			if len(model.Config) != 0 && string(model.Config) != "null" {
				if json.Unmarshal(model.Config, &config) != nil {
					return errors.New("invalid model config")
				}
			} else {
				config = map[string]any{}
			}
			var order any
			if len(model.FieldOrder) != 0 {
				if json.Unmarshal(model.FieldOrder, &order) != nil {
					return errors.New("invalid model field order")
				}
			}
			reply, e := objectRequest(c, "/api/v3/metadata/update-model", http.MethodPost, map[string]any{"id": id, "name": code + "-updated-drift", "description": objectMarker(code), "type": "custom", "parentKey": "", "enable": true, "fieldOrder": order, "config": config, "showFieldKey": ""})
			if e != nil || *reply.StatusCode != 200 {
				return errors.New("model drift mutation rejected")
			}
			return verify(code + "-updated-drift")
		}},
		{"plan-drift", plan(2)}, {"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)}, {"verify-before-destroy", func() error { return verify(code + "-updated") }},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			exists, _, e := readOwnedObject(c, code, id)
			if e != nil || exists {
				return errors.New("model exact-ID GET 404 not verified")
			}
			found, e := discoverObject(c, code)
			if e != nil || found != "" {
				return errors.New("model inventory still contains owned model")
			}
			return nil
		}},
	}}).execute()
}
