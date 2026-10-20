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

const fieldKey = "hermesacc-field"

type listedField struct {
	ID       string          `json:"id"`
	ModelID  string          `json:"modelId"`
	Key      string          `json:"key"`
	Name     string          `json:"name"`
	Type     json.RawMessage `json:"type"`
	Show     *bool           `json:"show"`
	Editable *bool           `json:"editable"`
}

func listOwnedFields(c *authingapi.Client, modelID string) ([]listedField, error) {
	if modelID == "" {
		return nil, errors.New("missing model ID")
	}
	reply, e := objectRequest(c, "/api/v3/metadata/list-field", http.MethodGet, map[string]string{"modelId": modelID, "from": "terraform"})
	if e != nil || *reply.StatusCode != 200 {
		return nil, errors.New("field inventory unavailable")
	}
	var fields []listedField
	if json.Unmarshal(reply.Data, &fields) != nil || fields == nil {
		return nil, errors.New("invalid field inventory")
	}
	for _, f := range fields {
		if f.ID == "" || f.ModelID != modelID || f.Key == "" {
			return nil, errors.New("incomplete field inventory")
		}
	}
	return fields, nil
}
func ownedField(c *authingapi.Client, modelID, id, key string) (bool, error) {
	fields, e := listOwnedFields(c, modelID)
	if e != nil {
		return false, e
	}
	found := false
	for _, f := range fields {
		if f.ID == id {
			if found || f.Key != key || f.Name != "Owned field" || string(f.Type) != "1" || f.Show == nil || !*f.Show || f.Editable == nil || !*f.Editable {
				return false, errors.New("field identity mismatch")
			}
			found = true
		}
	}
	return found, nil
}
func verifyFieldDeletionSafe(c *authingapi.Client, code, modelID, fieldID, key string) error {
	exists, name, e := readOwnedObject(c, code, modelID)
	if e != nil || !exists || name != code {
		return errors.New("model ownership unverified")
	}
	reply, e := objectRequest(c, "/api/v3/metadata/filter", http.MethodPost, map[string]any{"modelId": modelID, "page": 1, "limit": 1, "fetchAll": false})
	if e != nil || *reply.StatusCode != 200 {
		return errors.New("row inventory unavailable")
	}
	var rows struct {
		TotalCount *int            `json:"totalCount"`
		List       json.RawMessage `json:"list"`
	}
	if json.Unmarshal(reply.Data, &rows) != nil || rows.TotalCount == nil || *rows.TotalCount != 0 || string(rows.List) != "[]" {
		return errors.New("model has rows")
	}
	fields, e := listOwnedFields(c, modelID)
	if e != nil || len(fields) != 1 {
		return errors.New("field inventory not exclusive")
	}
	f := fields[0]
	if f.ID != fieldID || f.Key != key || f.Name != "Owned field" || string(f.Type) != "1" || f.Show == nil || !*f.Show || f.Editable == nil || !*f.Editable {
		return errors.New("field ownership unverified")
	}
	return nil
}

func cleanupFieldModel(c *authingapi.Client, code, modelID, fieldID, key string) error {
	if !sandboxCode.MatchString(code) || modelID == "" {
		return errors.New("no owned model ID")
	}
	exists, name, e := readOwnedObject(c, code, modelID)
	if e != nil {
		return errors.New("model GET unverified")
	}
	if !exists {
		found, err := discoverObject(c, code)
		if err != nil || found != "" {
			return errors.New("model absence unverified")
		}
		return nil
	}
	if name != code {
		return errors.New("model ownership unverified")
	}
	// Field deletion may discard all cells. Require both an empty row inventory
	// and an exclusive field inventory before removing anything.
	reply, e := objectRequest(c, "/api/v3/metadata/filter", http.MethodPost, map[string]any{"modelId": modelID, "page": 1, "limit": 1, "fetchAll": false})
	if e != nil || *reply.StatusCode != 200 {
		return errors.New("row inventory unavailable")
	}
	var rows struct {
		TotalCount *int            `json:"totalCount"`
		List       json.RawMessage `json:"list"`
	}
	if json.Unmarshal(reply.Data, &rows) != nil || rows.TotalCount == nil || *rows.TotalCount != 0 || string(rows.List) != "[]" {
		return errors.New("model has rows")
	}
	fields, e := listOwnedFields(c, modelID)
	if e != nil {
		return e
	}
	if len(fields) > 1 {
		return errors.New("foreign fields present")
	}
	if len(fields) == 1 {
		f := fields[0]
		if fieldID == "" || f.ID != fieldID || f.Key != key || f.Name != "Owned field" || string(f.Type) != "1" || f.Show == nil || !*f.Show || f.Editable == nil || !*f.Editable {
			return errors.New("field ownership unverified")
		}
		deleted, e := objectRequest(c, "/api/v3/metadata/remove-field", http.MethodPost, map[string]string{"modelId": modelID, "id": fieldID})
		if e != nil || *deleted.StatusCode != 200 {
			return errors.New("field deletion rejected")
		}
	}
	fields, e = listOwnedFields(c, modelID)
	if e != nil || len(fields) != 0 {
		return errors.New("field absence unconfirmed")
	}
	return cleanupObject(c, code, modelID)
}
func fieldStateIDs(root string, env []string, terraform string) (string, string, error) {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, e := cmd.Output()
	if e != nil {
		return "", "", errors.New("state unavailable")
	}
	var state struct {
		Values struct {
			Root struct {
				Resources []struct {
					Address string `json:"address"`
					Values  struct {
						ID      string `json:"id"`
						ModelID string `json:"model_id"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return "", "", errors.New("invalid state")
	}
	modelID, fieldID := "", ""
	for _, r := range state.Values.Root.Resources {
		switch r.Address {
		case "authing_data_object.sandbox":
			if modelID != "" {
				return "", "", errors.New("duplicate model")
			}
			modelID = r.Values.ID
		case "authing_data_object_field.sandbox":
			if fieldID != "" || r.Values.ModelID == "" {
				return "", "", errors.New("duplicate field")
			}
			fieldID = r.Values.ID
			if modelID != "" && r.Values.ModelID != modelID {
				return "", "", errors.New("field model mismatch")
			}
		}
	}
	return modelID, fieldID, nil
}
func fieldReplacementPlanned(root string, env []string, terraform string) error {
	cmd := exec.Command(terraform, "show", "-json", filepath.Join(root, "replacement.tfplan"))
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, e := cmd.Output()
	if e != nil {
		return errors.New("replacement plan unreadable")
	}
	var plan struct {
		Changes []struct {
			Address string `json:"address"`
			Change  struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if json.Unmarshal(raw, &plan) != nil {
		return errors.New("invalid replacement plan")
	}
	foundField := false
	for _, change := range plan.Changes {
		a := change.Change.Actions
		if change.Address == "authing_data_object.sandbox" && (len(a) != 1 || a[0] != "no-op") {
			return errors.New("replacement would change parent model")
		}
		if change.Address == "authing_data_object_field.sandbox" {
			if foundField || len(a) != 2 || !(a[0] == "delete" && a[1] == "create" || a[0] == "create" && a[1] == "delete") {
				return errors.New("field change did not plan replacement")
			}
			foundField = true
		}
	}
	if foundField {
		return nil
	}
	return errors.New("field replacement missing from plan")
}
func runFieldTrace(root string, credentials map[string]string, code string) (result error) {
	if !sandboxCode.MatchString(code) {
		return errors.New("field tracer requires generated model name")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, e := os.Stat(terraform); e != nil {
		return fmt.Errorf("field phase=terraform-cli code=%s (output suppressed)", code)
	}
	goBinary, e := exec.LookPath("go")
	if e != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, e = os.Stat(goBinary); e != nil {
			return fmt.Errorf("field phase=go-toolchain code=%s (output suppressed)", code)
		}
	}
	c, e := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if e != nil {
		return fmt.Errorf("field phase=client code=%s (output suppressed)", code)
	}
	prior, e := discoverObject(c, code)
	if e != nil || prior != "" {
		return fmt.Errorf("field phase=preflight code=%s (output suppressed)", code)
	}
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return errors.New("field workspace failed")
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, e = build.CombinedOutput(); e != nil {
		return errors.New("field provider build failed (output suppressed)")
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n dev_overrides {\n %q = %q\n }\n direct {}\n}\n", source, providerDir)
	hcl := func(key string) string {
		field := ""
		if key != "" {
			field = fmt.Sprintf(`resource "authing_data_object_field" "sandbox" {
 model_id = authing_data_object.sandbox.id
 key = %q
 name = "Owned field"
 type = "Text"
 show = true
 editable = true
}
`, key)
		}
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
%s`, source, code, objectMarker(code), field)
	}
	tfFile := filepath.Join(exampleDir, "main.tf")
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(tfFile, []byte(hcl("")), 0600) != nil {
		return errors.New("field config failed")
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	modelID, fieldID, key := "", "", ""
	started := false
	defer func() {
		if !started {
			return
		}
		if modelID == "" {
			modelID, _ = discoverObject(c, code)
		}
		if modelID == "" {
			result = fmt.Errorf("%v; cleanup-incomplete model=%s (output suppressed)", result, code)
			return
		}
		if cleanupFieldModel(c, code, modelID, fieldID, key) != nil {
			result = fmt.Errorf("%v; cleanup-incomplete model=%s id=%s (output suppressed)", result, code, modelID)
		}
	}()
	started = true
	result = (traceCase{name: "field", code: code, phases: []tracePhase{
		{"apply-empty-model", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"pin-model", func() error {
			modelID, _, e = fieldStateIDs(root, env, terraform)
			if e != nil || modelID == "" {
				return errors.New("missing model ID")
			}
			found, err := discoverObject(c, code)
			if err != nil || found != modelID {
				return errors.New("model ownership mismatch")
			}
			exists, name, err := readOwnedObject(c, code, modelID)
			if err != nil || !exists || name != code {
				return errors.New("model GET mismatch")
			}
			return verifyObjectEmpty(c, modelID)
		}},
		{"configure-field", func() error { return os.WriteFile(tfFile, []byte(hcl(fieldKey)), 0600) }},
		{"apply-field", func() error {
			e := run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")()
			_, fieldID, _ = fieldStateIDs(root, env, terraform)
			key = fieldKey
			return e
		}},
		{"verify-field", func() error {
			if fieldID == "" {
				return errors.New("missing field ID")
			}
			exists, e := ownedField(c, modelID, fieldID, key)
			if e != nil || !exists {
				return errors.New("field list identity mismatch")
			}
			return nil
		}},
		{"plan-converged", plan(0)},
		{"configure-replacement", func() error { return os.WriteFile(tfFile, []byte(hcl(fieldKey+"-new")), 0600) }},
		{"plan-replacement", run(0, "plan", "-lock=false", "-input=false", "-no-color", "-out="+filepath.Join(root, "replacement.tfplan"))},
		{"assert-replacement", func() error { return fieldReplacementPlanned(root, env, terraform) }},
		{"verify-before-replacement", func() error { return verifyFieldDeletionSafe(c, code, modelID, fieldID, key) }},
		{"apply-replacement", func() error {
			e := run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")()
			if e == nil {
				_, newID, readErr := fieldStateIDs(root, env, terraform)
				if readErr != nil || newID == "" || newID == fieldID {
					return errors.New("replacement ID not changed")
				}
				fieldID = newID
				key = fieldKey + "-new"
			}
			return e
		}},
		{"verify-replacement", func() error {
			exists, e := ownedField(c, modelID, fieldID, key)
			if e != nil || !exists {
				return errors.New("replacement not listed")
			}
			return nil
		}},
		{"plan-reconverged", plan(0)},
		{"verify-before-destroy-field", func() error { return verifyFieldDeletionSafe(c, code, modelID, fieldID, key) }},
		{"destroy-field", run(0, "destroy", "-target=authing_data_object_field.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-field-absent", func() error {
			fields, e := listOwnedFields(c, modelID)
			if e != nil || len(fields) != 0 {
				return errors.New("field absence unconfirmed")
			}
			return nil
		}},
		{"verify-model-empty", func() error { return verifyObjectEmpty(c, modelID) }},
		{"destroy-model", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-model-absent", func() error {
			exists, _, e := readOwnedObject(c, code, modelID)
			if e != nil || exists {
				return errors.New("model exact GET not absent")
			}
			return nil
		}},
	}}).execute()
	return result
}
func TestDestructiveLiveDataObjectFieldReplacement(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if e := destructiveGuard(*destructiveLive, env); e != nil {
		t.Fatal(e)
	}
	code, e := newGroupCode()
	if e != nil {
		t.Fatal("model name generation failed")
	}
	if e = runFieldTrace(t.TempDir(), env, code); e != nil {
		t.Fatal(e)
	}
}
