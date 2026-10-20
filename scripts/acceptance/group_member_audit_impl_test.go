package acceptance

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"terraform-provider-authing/internal/authingapi"
)

// This test is intentionally separate from destructive acceptance. A generated
// prefix alone is not proof of ownership by the failed run; no deletion follows.
var groupMemberAuditLive = flag.Bool("authing-group-member-audit", false, "opt in to read-only group-member orphan audit")

type groupMemberAuditResult struct {
	State             string
	Groups, Users     int
	Codes, UserHashes []string
}

func groupMemberAuditGuard(enabled bool, env map[string]string, start, end string) (time.Time, time.Time, bool) {
	a, e1 := time.Parse(time.RFC3339, start)
	b, e2 := time.Parse(time.RFC3339, end)
	return a, b, enabled && e1 == nil && e2 == nil && a.Before(b) && b.Sub(a) <= 24*time.Hour && env["AUTHING_ACCEPTANCE_CONFIRM"] == "READ_ONLY_SANDBOX" && env["AUTHING_ACCESS_KEY_ID"] != "" && env["AUTHING_ACCESS_KEY_SECRET"] != ""
}

type auditEnvelope struct {
	StatusCode *int            `json:"statusCode"`
	Data       json.RawMessage `json:"data"`
}

func auditData(body []byte, dst any) bool {
	var e auditEnvelope
	return json.Unmarshal(body, &e) == nil && e.StatusCode != nil && *e.StatusCode == 200 && len(e.Data) > 0 && string(e.Data) != "null" && json.Unmarshal(e.Data, dst) == nil
}
func auditPage(body []byte) (int, []json.RawMessage, bool) {
	var d struct {
		TotalCount *int               `json:"totalCount"`
		List       *[]json.RawMessage `json:"list"`
	}
	if !auditData(body, &d) || d.TotalCount == nil || *d.TotalCount < 0 || d.List == nil || len(*d.List) > 50 {
		return 0, nil, false
	}
	return *d.TotalCount, *d.List, true
}
func auditInventory(client *authingapi.Client, path, method string) ([]json.RawMessage, bool) {
	var all []json.RawMessage
	seen := map[string]bool{}
	total := -1
	for page := 1; page <= 100; page++ {
		var req any
		if method == http.MethodGet {
			req = map[string]any{"keywords": "hermesacc", "page": page, "limit": 50}
		} else {
			req = map[string]any{"keywords": "hermesacc", "options": map[string]any{"pagination": map[string]int{"page": page, "limit": 50}, "fuzzySearchOn": []string{"username"}}}
		}
		body, err := client.SendHttpRequest(path, method, req)
		if err != nil {
			return nil, false
		}
		n, items, ok := auditPage(body)
		if !ok || total >= 0 && total != n || len(all)+len(items) > n || len(items) == 0 && len(all) < n {
			return nil, false
		}
		total = n
		for _, item := range items {
			var id struct {
				Code   string `json:"code"`
				UserID string `json:"userId"`
			}
			if json.Unmarshal(item, &id) != nil {
				return nil, false
			}
			key := id.Code
			if method == http.MethodPost {
				key = id.UserID
			}
			if key == "" || seen[key] {
				return nil, false
			}
			seen[key] = true
		}
		all = append(all, items...)
		if len(all) == total {
			return all, true
		}
	}
	return nil, false
}
func auditTimestamp(raw string, start, end time.Time) (bool, bool) {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return false, false
	}
	return !t.Before(start) && !t.After(end), true
}
func probeGroupMemberAudit(client *authingapi.Client, start, end time.Time) groupMemberAuditResult {
	unknown := groupMemberAuditResult{State: "unknown"}
	groups, ok := auditInventory(client, "/api/v3/list-groups", http.MethodGet)
	if !ok {
		return unknown
	}
	users, ok := auditInventory(client, "/api/v3/list-users", http.MethodPost)
	if !ok {
		return unknown
	}
	result := groupMemberAuditResult{State: "zero"}
	for _, raw := range groups {
		var g struct{ Code, Name, Description, Type string }
		if json.Unmarshal(raw, &g) != nil {
			return unknown
		}
		if !sandboxCode.MatchString(g.Code) {
			continue
		}
		body, err := client.SendHttpRequest("/api/v3/get-group", http.MethodGet, map[string]string{"code": g.Code})
		if err != nil {
			return unknown
		}
		var exact struct{ Code, Name, Description, Type string }
		if !auditData(body, &exact) || exact.Code != g.Code || exact.Name != g.Name || exact.Description != g.Description || exact.Type != g.Type {
			return unknown
		}
		if g.Name == g.Code && g.Description == "hermesacc ownership "+g.Code && g.Type == "static" {
			result.Codes = append(result.Codes, g.Code)
		}
	}
	for _, raw := range users {
		var u struct{ UserID, Username, CreatedAt string }
		if json.Unmarshal(raw, &u) != nil {
			return unknown
		}
		if !sandboxCode.MatchString(u.Username) {
			continue
		}
		within, valid := auditTimestamp(u.CreatedAt, start, end)
		if !valid {
			return unknown
		}
		if !within {
			continue
		}
		body, err := client.SendHttpRequest("/api/v3/get-user", http.MethodGet, map[string]string{"userId": u.UserID})
		if err != nil {
			return unknown
		}
		var exact struct{ UserID, Username, Nickname, CreatedAt string }
		if !auditData(body, &exact) || exact.UserID != u.UserID || exact.Username != u.Username || exact.Nickname != u.Username || exact.CreatedAt != u.CreatedAt {
			return unknown
		}
		sum := sha256.Sum256([]byte(u.UserID))
		result.UserHashes = append(result.UserHashes, fmt.Sprintf("%x", sum[:8]))
	}
	sort.Strings(result.Codes)
	sort.Strings(result.UserHashes)
	result.Groups = len(result.Codes)
	result.Users = len(result.UserHashes)
	if result.Groups > 0 || result.Users > 0 {
		result.State = "candidates"
	}
	return result
}
func formatGroupMemberAudit(r groupMemberAuditResult) string {
	// Failures discard all partially collected identities. Never log raw user IDs.
	if r.State != "zero" && r.State != "candidates" {
		return "group-member-audit status=unknown"
	}
	return fmt.Sprintf("group-member-audit status=%s group_candidates=%d user_candidates=%d codes=%s user_hash=%s group_time=unavailable", r.State, r.Groups, r.Users, strings.Join(r.Codes, ","), strings.Join(r.UserHashes, ","))
}
func TestReadOnlyGroupMemberAudit(t *testing.T) {
	if !*groupMemberAuditLive {
		t.Skip("requires -authing-group-member-audit and READ_ONLY_SANDBOX")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET")}
	start, end, ok := groupMemberAuditGuard(*groupMemberAuditLive, env, os.Getenv("AUTHING_AUDIT_START"), os.Getenv("AUTHING_AUDIT_END"))
	if !ok {
		t.Fatal("group-member audit guard rejected input (output suppressed)")
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: env["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: env["AUTHING_ACCESS_KEY_SECRET"], Host: os.Getenv("AUTHING_HOST")})
	if err != nil {
		t.Fatal("audit client unavailable (output suppressed)")
	}
	t.Log(formatGroupMemberAudit(probeGroupMemberAudit(client, start, end)))
}
