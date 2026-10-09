package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Authing/authing-golang-sdk/v3/dto"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"terraform-provider-authing/internal/authingapi"
	"time"
)

// Pin only the creating Terraform state's exact address, code and identity.
func groupStateID(root, terraform string, env []string, code string) (string, error) {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.New("state unavailable")
	}
	var state struct {
		Values struct {
			RootModule struct {
				Resources []struct {
					Address string                                             `json:"address"`
					Values  struct{ ID, Code, Name, Description, Type string } `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil || len(state.Values.RootModule.Resources) != 1 {
		return "", errors.New("invalid state")
	}
	v := state.Values.RootModule.Resources[0]
	if v.Address != "authing_group.sandbox" || v.Values.ID != code || v.Values.Code != code || v.Values.Type != "static" || (v.Values.Name != code && v.Values.Name != code+"-updated" && v.Values.Name != code+"-drift") {
		return "", errors.New("creating state ownership unverified")
	}
	return v.Values.ID, nil
}
func emptyGroupMembers(client *authingapi.Client, code string) error {
	raw, err := client.SendHttpRequestContext(context.Background(), "/api/v3/list-group-members", "GET", map[string]any{"code": code, "page": 1, "limit": 50})
	var v struct {
		StatusCode int `json:"statusCode"`
		Data       *struct {
			TotalCount *int               `json:"totalCount"`
			List       *[]json.RawMessage `json:"list"`
		} `json:"data"`
	}
	if err != nil || json.Unmarshal(raw, &v) != nil || v.StatusCode != 200 || v.Data == nil || v.Data.TotalCount == nil || *v.Data.TotalCount != 0 || v.Data.List == nil || len(*v.Data.List) != 0 {
		return errors.New("empty membership unproved")
	}
	return nil
}
func pinnedGroupFields(client *authingapi.Client, id, code string) (bool, error) {
	if id == "" || id != code || !sandboxCode.MatchString(code) {
		return false, errors.New("state-pinned identity unavailable")
	}
	got := client.GetGroup(&dto.GetGroupDto{Code: id})
	if got != nil && got.StatusCode == 404 {
		return false, nil
	}
	if got == nil || got.StatusCode != 200 || got.Data.Code != id || got.Data.Type != "static" {
		return false, errors.New("ownership unverified")
	}
	owned := false
	for _, suffix := range []string{"", "-updated", "-drift"} {
		if got.Data.Name == code+suffix && got.Data.Description == "hermesacc ownership "+code+suffix {
			owned = true
		}
	}
	if got.Data.Name == code && got.Data.Description == "" {
		owned = true
	}
	if !owned {
		return false, errors.New("ownership fields unverified")
	}
	return true, nil
}
func verifyPinnedGroup(client *authingapi.Client, id, code string) error {
	found, e := pinnedGroupFields(client, id, code)
	if e != nil {
		return e
	}
	if !found {
		return errors.New("owned group absent")
	}
	return emptyGroupMembers(client, code)
}
func cleanupPinnedGroup(client *authingapi.Client, id, code string) error {
	for attempt := 0; attempt < 3; attempt++ {
		found, e := pinnedGroupFields(client, id, code)
		if e != nil {
			return e
		}
		if !found {
			return nil
		}
		if e := emptyGroupMembers(client, id); e != nil {
			return e
		}
		deleted := client.DeleteGroupsBatch(&dto.DeleteGroupsReqDto{CodeList: []string{id}})
		if deleted != nil && deleted.StatusCode != 200 && deleted.StatusCode != 404 {
			return errors.New("cleanup rejected")
		}
		if attempt < 2 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	found, e := pinnedGroupFields(client, id, code)
	if e != nil || found {
		return errors.New("absence unproved")
	}
	return nil
}

// Inspect replacement actions but never apply a replacement in the sandbox.
func groupReplacementPlan(root, terraform string, env []string, hcl, field, value string) error {
	file := filepath.Join(root, "example/main.tf")
	lines := strings.Split(hcl, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), field+" = ") {
			lines[i] = fmt.Sprintf(" %s = %q", field, value)
		}
	}
	if e := os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0600); e != nil {
		return e
	}
	defer os.WriteFile(file, []byte(hcl), 0600)
	if e := terraformExit(root, env, terraform, 2, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode", "-out=replacement.tfplan"); e != nil {
		return e
	}
	cmd := exec.Command(terraform, "show", "-json", "replacement.tfplan")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, e := cmd.CombinedOutput()
	if e != nil {
		return errors.New("replacement plan unavailable")
	}
	var plan struct {
		Changes []struct {
			Address string `json:"address"`
			Change  struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if json.Unmarshal(raw, &plan) != nil || len(plan.Changes) != 1 || plan.Changes[0].Address != "authing_group.sandbox" || strings.Join(plan.Changes[0].Change.Actions, ",") != "delete,create" {
		return errors.New("replacement actions not proved")
	}
	return nil
}
