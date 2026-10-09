package acceptance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

// Numeric HTTP status is available only on typed transport errors; zero means
// unavailable, NOT HTTP 200. No response text or raw identity is retained here.
type tenantProbeStage struct {
	Stage, Category, Shape                                string
	HTTP, Business, APICode, Pages, Count, Matches, Total int
}
type tenantProbeResult struct {
	List, GET, Apps, Users, Admins, Orgs tenantProbeStage
	Fingerprint                          string
}

func tenantProbeRequest(c *authingapi.Client, stage, path, method string, arg any) (tenantProbeStage, json.RawMessage) {
	s := tenantProbeStage{Stage: stage, Category: "unknown", Shape: "unavailable", Count: -1, Total: -1}
	raw, err := c.SendHttpRequest(path, method, arg)
	if err != nil {
		var h *authingapi.HTTPStatusError
		switch {
		case errors.As(err, &h):
			s.HTTP = h.StatusCode
			s.Category = "http-other"
			if h.StatusCode >= 400 && h.StatusCode < 500 {
				s.Category = "http-4xx"
			}
			if h.StatusCode >= 500 {
				s.Category = "http-5xx"
			}
		case errors.Is(err, authingapi.ErrInvalidResponse):
			s.Category = "invalid-envelope"
		default:
			s.Category = "transport"
		}
		return s, nil
	}
	var e struct {
		Status  *int            `json:"statusCode"`
		APICode *int            `json:"apiCode"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Status == nil {
		s.Category = "invalid-envelope"
		return s, nil
	}
	s.Business = *e.Status
	if e.APICode != nil {
		s.APICode = *e.APICode
	}
	if s.Business != 200 {
		s.Category = "business-other"
		if s.Business >= 400 && s.Business < 500 {
			s.Category = "business-4xx"
		}
		if s.Business >= 500 {
			s.Category = "business-5xx"
		}
		return s, nil
	}
	s.Category = "complete"
	return s, e.Data
}

// An explicit count and array are mandatory. All shape labels are closed.
func tenantProbePage(raw json.RawMessage, s tenantProbeStage) (tenantProbeStage, []json.RawMessage, int) {
	var data map[string]json.RawMessage
	if json.Unmarshal(raw, &data) != nil || data == nil {
		s.Category = "invalid-shape"
		s.Shape = "invalid-data"
		return s, nil, -1
	}
	var total *int
	if json.Unmarshal(data["totalCount"], &total) != nil || total == nil || *total < 0 {
		s.Category = "invalid-shape"
		s.Shape = "missing-or-invalid-count"
		return s, nil, -1
	}
	s.Total = *total
	var list []json.RawMessage
	if json.Unmarshal(data["list"], &list) != nil || list == nil {
		s.Category = "invalid-shape"
		s.Shape = "missing-or-invalid-list"
		return s, nil, -1
	}
	s.Shape = "count-list"
	return s, list, *total
}

// Public OpenAPI: user PaginationDto and organization limit max 50; tenant
// list/admin pagination is string-valued without a documented maximum. Use 50
// everywhere, exhaust all pages before trusting a candidate, cap at 100 pages.
// The candidate ID remains local; it is NEVER a state-pinned deletion authority.
func tenantProbeInventory(c *authingapi.Client, kind, id, name string) (tenantProbeStage, string) {
	if kind != "list" && id == "" || kind == "list" && name != incidentTenantName {
		return tenantProbeStage{Stage: "guard", Category: "guard", Shape: "unavailable", Count: -1, Total: -1}, ""
	}
	seen := map[string]bool{}
	total, matches := -1, 0
	candidate := ""
	var last tenantProbeStage
	for page := 1; page <= 100; page++ {
		path, method := "/api/v3/list-tenants", http.MethodGet
		var arg any = map[string]string{"page": fmt.Sprint(page), "limit": "50"}
		switch kind {
		case "users":
			path = "/api/v3/list-tenant-users"
			method = http.MethodPost
			arg = map[string]any{"tenantId": id, "options": map[string]any{"pagination": map[string]int{"page": page, "limit": 50}}}
		case "admins":
			path = "/api/v3/list-tenant-admin"
			method = http.MethodPost
			arg = map[string]string{"tenantId": id, "page": fmt.Sprint(page), "limit": "50"}
		case "organizations":
			path = "/api/v3/list-organizations"
			arg = map[string]any{"tenantId": id, "page": page, "limit": 50}
		case "list":
		default:
			return tenantProbeStage{Stage: "guard", Category: "guard", Shape: "unavailable", Count: -1, Total: -1}, ""
		}
		s, raw := tenantProbeRequest(c, kind, path, method, arg)
		s.Pages = page
		s.Count = len(seen)
		s.Matches = matches
		if s.Category != "complete" {
			return s, ""
		}
		s, list, n := tenantProbePage(raw, s)
		if s.Category != "complete" {
			return s, ""
		}
		if total < 0 {
			total = n
		}
		if total != n || len(list) != min(50, total-len(seen)) {
			s.Category = "incomplete-inventory"
			return s, ""
		}
		for _, item := range list {
			var v struct {
				TenantID string `json:"tenantId"`
				Name     string `json:"name"`
				MemberID string `json:"memberId"`
				OrgCode  string `json:"organizationCode"`
			}
			if json.Unmarshal(item, &v) != nil {
				s.Category = "invalid-shape"
				s.Shape = "invalid-item"
				return s, ""
			}
			key := v.TenantID
			if kind == "list" {
				if v.Name == "" || key == "" {
					s.Category = "invalid-shape"
					s.Shape = "invalid-identity"
					return s, ""
				}
				if v.Name == name {
					matches++
					candidate = key
				}
			} else {
				key = v.MemberID
				if kind == "organizations" {
					key = v.OrgCode
				}
				if key == "" || kind != "organizations" && v.TenantID == "" {
					s.Category = "invalid-shape"
					s.Shape = "invalid-identity"
					return s, ""
				}
				// tenantId is required on TenantUserDto, optional on OrganizationDto.
				if v.TenantID != id && (kind != "organizations" || v.TenantID != "") {
					s.Category = "foreign"
					return s, ""
				}
			}
			if seen[key] {
				s.Category = "duplicate"
				return s, ""
			}
			seen[key] = true
		}
		s.Count = len(seen)
		s.Matches = matches
		if len(seen) == total {
			if kind == "list" {
				s.Category = "absent"
				if matches == 1 {
					s.Category = "candidate"
				}
				if matches > 1 {
					s.Category = "duplicate"
					return s, ""
				}
			} else if total == 0 {
				s.Category = "empty"
			} else {
				s.Category = "nonempty"
			}
			return s, candidate
		}
		last = s
	}
	last.Category = "page-cap"
	return last, ""
}

func tenantProbeIdentity(c *authingapi.Client, id, name string, otherNames ...string) (tenantProbeStage, tenantProbeStage) {
	s, raw := tenantProbeRequest(c, "get", "/api/v3/get-tenant", http.MethodGet, map[string]string{"tenantId": id})
	apps := tenantProbeStage{Stage: "apps", Category: "not-run", Shape: "unavailable", Count: -1, Total: -1}
	if s.Category != "complete" {
		return s, apps
	}
	var v tenantIdentity
	// Decode appIds separately: a missing appIds must not hide a verified GET
	// identity or prevent the independent scoped read-only diagnostics.
	var identity struct {
		ID   string `json:"tenantId"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &identity) != nil || identity.ID == "" || identity.Name == "" {
		s.Category = "invalid-shape"
		s.Shape = "invalid-identity"
		return s, apps
	}
	s.Shape = "identity"
	nameMatches := identity.Name == name
	for _, other := range otherNames {
		if identity.Name == other {
			nameMatches = true
		}
	}
	if identity.ID != id || !nameMatches {
		s.Category = "foreign"
		return s, apps
	}
	s.Category = "candidate"
	apps.Business = s.Business
	apps.APICode = s.APICode
	apps.Category = "invalid-shape"
	apps.Shape = "missing-or-invalid-appids"
	if json.Unmarshal(raw, &v) == nil && v.AppIDs != nil {
		apps.Shape = "appids"
		apps.Count = len(*v.AppIDs)
		apps.Category = "empty"
		if apps.Count > 0 {
			apps.Category = "nonempty"
		}
	}
	return s, apps
}
func probeTenantRecovery(c *authingapi.Client, name string) tenantProbeResult {
	r := tenantProbeResult{
		List:   tenantProbeStage{Stage: "list", Category: "not-run", Shape: "unavailable", Count: -1, Total: -1},
		GET:    tenantProbeStage{Stage: "get", Category: "not-run", Shape: "unavailable", Count: -1, Total: -1},
		Apps:   tenantProbeStage{Stage: "apps", Category: "not-run", Shape: "unavailable", Count: -1, Total: -1},
		Users:  tenantProbeStage{Stage: "users", Category: "not-run", Shape: "unavailable", Count: -1, Total: -1},
		Admins: tenantProbeStage{Stage: "admins", Category: "not-run", Shape: "unavailable", Count: -1, Total: -1},
		Orgs:   tenantProbeStage{Stage: "organizations", Category: "not-run", Shape: "unavailable", Count: -1, Total: -1},
	}
	if name != incidentTenantName {
		r.List.Category = "guard"
		return r
	}
	var id string
	r.List, id = tenantProbeInventory(c, "list", "", name)
	if r.List.Category != "candidate" {
		return r
	}
	r.GET, r.Apps = tenantProbeIdentity(c, id, name)
	if r.GET.Category != "candidate" {
		return r
	}
	hash := sha256.Sum256([]byte(id))
	r.Fingerprint = hex.EncodeToString(hash[:])
	r.Users, _ = tenantProbeInventory(c, "users", id, "")
	r.Admins, _ = tenantProbeInventory(c, "admins", id, "")
	r.Orgs, _ = tenantProbeInventory(c, "organizations", id, "")
	return r
}

func tenantClosed(value, allowed, fallback string) string {
	for _, label := range strings.Fields(allowed) {
		if value == label {
			return value
		}
	}
	return fallback
}
func formatTenantStage(s tenantProbeStage) string {
	stage := tenantClosed(s.Stage, "list get apps users admins organizations guard", "guard")
	category := tenantClosed(s.Category, "unknown not-run guard transport invalid-envelope http-other http-4xx http-5xx business-other business-4xx business-5xx complete candidate absent empty nonempty foreign duplicate invalid-shape incomplete-inventory page-cap", "unknown")
	shape := tenantClosed(s.Shape, "unavailable count-list identity appids invalid-data missing-or-invalid-count missing-or-invalid-list invalid-item invalid-identity missing-or-invalid-appids", "unavailable")
	return fmt.Sprintf("stage=%s category=%s http_error=%d business=%d api_code=%d shape=%s pages=%d count=%d matches=%d total=%d", stage, category, s.HTTP, s.Business, s.APICode, shape, s.Pages, s.Count, s.Matches, s.Total)
}
func formatTenantRecovery(r tenantProbeResult) string {
	fingerprint := "unavailable"
	if len(r.Fingerprint) == 64 {
		if b, e := hex.DecodeString(r.Fingerprint); e == nil && len(b) == 32 {
			fingerprint = r.Fingerprint
		}
	}
	parts := []string{fmt.Sprintf("tenant-recovery name=%s authority=none id_sha256=%s", incidentTenantName, fingerprint)}
	for _, s := range []tenantProbeStage{r.List, r.GET, r.Apps, r.Users, r.Admins, r.Orgs} {
		parts = append(parts, formatTenantStage(s))
	}
	return strings.Join(parts, "; ")
}

func TestReadOnlySandboxTenantRecovery(t *testing.T) {
	if !*tenantRecoveryLive {
		t.Skip("requires -authing-tenant-recovery-sandbox and READ_ONLY_SANDBOX")
	}
	env := map[string]string{}
	for _, key := range []string{"AUTHING_ACCEPTANCE_CONFIRM", "AUTHING_ACCESS_KEY_ID", "AUTHING_ACCESS_KEY_SECRET", "AUTHING_HOST", "AUTHING_TENANT_RECOVERY_NAME"} {
		env[key] = os.Getenv(key)
	}
	name := env["AUTHING_TENANT_RECOVERY_NAME"]
	if !tenantRecoveryGuard(*tenantRecoveryLive, env, name) {
		t.Fatal("tenant recovery guard rejected input (output suppressed)")
	}
	c, err := authingapi.NewClient(authingapi.Options{AccessKeyID: env["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: env["AUTHING_ACCESS_KEY_SECRET"], Host: env["AUTHING_HOST"]})
	if err != nil {
		t.Fatal("tenant recovery client failed (output suppressed)")
	}
	t.Log(formatTenantRecovery(probeTenantRecovery(c, name)))
}

// Only typed, closed diagnostics survive a trace failure. Other phase errors
// (including Terraform/API response text) remain suppressed. First phase wins.
type tenantDiagnosticError struct{ Diagnostic tenantProbeStage }

func (e tenantDiagnosticError) Error() string { return formatTenantStage(e.Diagnostic) }
func executeTenantTrace(c traceCase) error {
	for _, p := range c.phases {
		if err := p.run(); err != nil {
			result := fmt.Sprintf("%s phase=%s code=%s (output suppressed)", c.name, p.name, c.code)
			var diagnostic tenantDiagnosticError
			if errors.As(err, &diagnostic) {
				result += " " + formatTenantStage(diagnostic.Diagnostic)
			}
			return errors.New(result)
		}
	}
	return nil
}

const incidentTenantName = "hermesacc-97f07719b2e15f0c"

var tenantRecoveryLive = flag.Bool("authing-tenant-recovery-sandbox", false, "opt in to exact-incident read-only tenant diagnosis")

func tenantRecoveryGuard(enabled bool, env map[string]string, name string) bool {
	return enabled && name == incidentTenantName && env["AUTHING_ACCEPTANCE_CONFIRM"] == "READ_ONLY_SANDBOX" && env["AUTHING_ACCESS_KEY_ID"] != "" && env["AUTHING_ACCESS_KEY_SECRET"] != ""
}
