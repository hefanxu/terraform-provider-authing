package acceptance

import (
	"encoding/json"
	"errors"
	"net/http"
	"terraform-provider-authing/internal/authingapi"
)

// Require a complete, stable policy-scoped inventory before deciding whether
// one exact (type, identifier) tuple exists. Never infer absence from a short page.
func exactPolicyTarget(c *authingapi.Client, policy, typ, id string) (bool, error) {
	if policy == "" || typ == "" || id == "" {
		return false, errors.New("missing target scope")
	}
	const limit = 50
	seen := map[string]bool{}
	total := -1
	found := false
	for page := 1; page <= 10000; page++ {
		raw, err := c.SendHttpRequest("/api/v3/list-data-policy-targets", http.MethodGet, map[string]any{"policyId": policy, "page": page, "limit": limit})
		var out struct {
			StatusCode int `json:"statusCode"`
			Data       *struct {
				Total *int `json:"totalCount"`
				List  *[]struct {
					Identifier string `json:"targetIdentifier"`
					Type       string `json:"targetType"`
				} `json:"list"`
			} `json:"data"`
		}
		if err != nil || json.Unmarshal(raw, &out) != nil || out.StatusCode != 200 || out.Data == nil || out.Data.Total == nil || out.Data.List == nil || *out.Data.Total < 0 {
			return false, errors.New("policy target inventory incomplete")
		}
		if total < 0 {
			total = *out.Data.Total
		}
		if total != *out.Data.Total {
			return false, errors.New("policy target count changed")
		}
		expected := total - len(seen)
		if expected > limit {
			expected = limit
		}
		if expected < 0 || len(*out.Data.List) != expected {
			return false, errors.New("policy target page incomplete")
		}
		for _, v := range *out.Data.List {
			if v.Identifier == "" || v.Type == "" || seen[v.Type+"\x00"+v.Identifier] {
				return false, errors.New("policy target identity invalid")
			}
			seen[v.Type+"\x00"+v.Identifier] = true
			if v.Type == typ && v.Identifier == id {
				found = true
			}
		}
		if len(seen) == total {
			return found, nil
		}
	}
	return false, errors.New("policy target pagination unbounded")
}
