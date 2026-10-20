package acceptance

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
)

func assignmentTraceID(policy, user string) string {
	raw, _ := json.Marshal([3]string{policy, "USER", user})
	return "v1." + base64.RawURLEncoding.EncodeToString(raw)
}
func verifyRelationState(root, terraform string, env []string, address, id string, want map[string]string) error {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, e := cmd.Output()
	if e != nil {
		return errors.New("relation state unavailable")
	}
	var state struct {
		Values struct {
			Root struct {
				Resources []struct {
					Address string                     `json:"address"`
					Values  map[string]json.RawMessage `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return errors.New("relation state malformed")
	}
	matched := 0
	for _, r := range state.Values.Root.Resources {
		if r.Address != address {
			continue
		}
		matched++
		checks := map[string]string{"id": id}
		for k, v := range want {
			checks[k] = v
		}
		for k, v := range checks {
			var got string
			if json.Unmarshal(r.Values[k], &got) != nil || got != v {
				return errors.New("relation state identity mismatch")
			}
		}
	}
	if matched != 1 {
		return errors.New("relation state absent or duplicated")
	}
	return nil
}
