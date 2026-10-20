package acceptance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"

	"terraform-provider-authing/internal/authingapi"
)

// Recovery has its own request allowlist. POST list operations are read-only;
// no create/update/delete or guessed endpoint can pass this boundary.
var cleanupReadPaths = map[string]string{
	"/api/v3/list-groups": "GET", "/api/v3/get-group": "GET", "/api/v3/list-group-members": "GET",
	"/api/v3/list-users": "POST", "/api/v3/get-user": "GET", "/api/v3/get-user-groups": "GET",
	"/api/v3/list-public-accounts": "POST", "/api/v3/get-public-account": "GET",
	"/api/v3/list-applications": "GET", "/api/v3/get-application": "GET",
	"/api/v3/list-namespaces": "GET", "/api/v3/get-namespace": "GET",
	"/api/v3/list-ext-idp": "GET", "/api/v3/get-ext-idp": "GET", "/api/v3/list-ext-idp-conns": "GET",
	"/api/v3/list-webhooks": "GET", "/api/v3/get-webhook": "GET",
	"/api/v3/list-pipeline-functions": "GET", "/api/v3/get-pipeline-function": "GET",
	"/api/v3/list-tenants": "GET", "/api/v3/get-tenant": "GET", "/api/v3/list-tenant-users": "POST", "/api/v3/list-tenant-admin": "POST",
	"/api/v3/list-organizations": "GET", "/api/v3/get-organization": "GET", "/api/v3/list-children-departments": "GET", "/api/v3/list-department-members": "GET",
}

func cleanupRead(c *authingapi.Client, path, method string, arg any) (json.RawMessage, int, string) {
	want, allowed := cleanupReadPaths[path]
	if !allowed || want != method {
		return nil, 0, "guard"
	}
	body, err := c.SendHttpRequest(path, method, arg)
	if err != nil {
		return nil, 0, "transport-or-http-error"
	}
	var e struct {
		Status *int            `json:"statusCode"`
		Data   json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &e) != nil || e.Status == nil {
		return nil, 0, "invalid-envelope"
	}
	if *e.Status == 404 {
		return nil, 404, "absent"
	}
	if *e.Status != 200 {
		return nil, *e.Status, "business-error"
	}
	if len(e.Data) == 0 || string(e.Data) == "null" {
		return nil, 200, "invalid-data"
	}
	return e.Data, 200, "complete"
}

type cleanupFamily struct {
	Family, Path, ID, Name, Get, GetID, PageKind string
	Paged                                        bool
	Extra                                        map[string]any
}
type cleanupEvent struct {
	Kind      string `json:"kind"`
	Family    string `json:"family"`
	ScopeHash string `json:"scope_sha256,omitempty"`
	Code      string `json:"generated_code,omitempty"`
	Hash      string `json:"id_sha256,omitempty"`
	State     string `json:"state"`
	Authority string `json:"authority,omitempty"`
	Marker    bool   `json:"marker_matches"`
	Business  int    `json:"business,omitempty"`
	Page      int    `json:"page,omitempty"`
	Count     int    `json:"count"`
	Total     int    `json:"total"`
}

func cleanupHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func cleanupString(m map[string]json.RawMessage, k string) string {
	var s string
	_ = json.Unmarshal(m[k], &s)
	return s
}
func cleanupInventory(c *authingapi.Client, f cleanupFamily, scope map[string]any, emit func(cleanupEvent)) ([]map[string]json.RawMessage, string) {
	seen := map[string]bool{}
	all := []map[string]json.RawMessage{}
	total := -1
	for page := 1; page <= 100; page++ {
		arg := map[string]any{}
		for k, v := range scope {
			arg[k] = v
		}
		for k, v := range f.Extra {
			arg[k] = v
		}
		if f.Paged {
			switch f.PageKind {
			case "options":
				arg["options"] = map[string]any{"pagination": map[string]int{"page": page, "limit": 50}}
			case "strings":
				arg["page"] = fmt.Sprint(page)
				arg["limit"] = "50"
			default:
				arg["page"] = page
				arg["limit"] = 50
			}
		}
		raw, business, state := cleanupRead(c, f.Path, cleanupReadPaths[f.Path], arg)
		if state != "complete" {
			if state == "absent" {
				return nil, "inventory-unavailable"
			}
			return nil, state
		}
		var d struct {
			Total *int                          `json:"totalCount"`
			List  *[]map[string]json.RawMessage `json:"list"`
		}
		if json.Unmarshal(raw, &d) != nil || d.Total == nil || *d.Total < 0 || d.List == nil {
			return nil, "invalid-page"
		}
		if total < 0 {
			total = *d.Total
		}
		items := *d.List
		if total != *d.Total || len(all)+len(items) > total || len(items) == 0 && len(all) < total || f.Paged && len(items) != min(50, total-len(all)) {
			return nil, "incomplete-inventory"
		}
		for _, item := range items {
			id := cleanupString(item, f.ID)
			if id == "" || seen[id] {
				return nil, "duplicate-or-missing-identity"
			}
			seen[id] = true
		}
		all = append(all, items...)
		if emit != nil {
			emit(cleanupEvent{Kind: "page", Family: f.Family, State: "observed-not-authority", Business: business, Page: page, Count: len(items), Total: total})
		}
		if len(all) == total {
			return all, "complete"
		}
		if !f.Paged {
			return nil, "incomplete-inventory"
		}
	}
	return nil, "page-cap"
}

var cleanupFingerprint = regexp.MustCompile(`^[0-9a-f]{64}$`)

func cleanupPinnedGuard(original, observed string, marker, emptyDependencies bool) bool {
	return cleanupFingerprint.MatchString(original) && original == observed && marker && emptyDependencies
}
