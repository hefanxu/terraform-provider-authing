package acceptance

import (
	"encoding/json"
	"terraform-provider-authing/internal/authingapi"
)

// A scoped audit log can recover a diagnostic ID only by an already recorded
// full SHA-256. It never upgrades a discovery fingerprint to creating-state or
// deletion authority. Unrequested values, including PII/configuration, are not
// retained or emitted. JSON-encoded nested operation parameters are supported.
func cleanupRecoverHashes(raw json.RawMessage, want map[string]bool, found map[string]string, depth int) {
	if depth > 10 || len(raw) > 1<<20 {
		return
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return
	}
	switch v := value.(type) {
	case string:
		h := cleanupHash(v)
		if cleanupFingerprint.MatchString(h) && want[h] {
			found[h] = v
		}
		if len(v) > 0 && (v[0] == '{' || v[0] == '[') {
			cleanupRecoverHashes(json.RawMessage(v), want, found, depth+1)
		}
	case map[string]any:
		for _, child := range v {
			b, _ := json.Marshal(child)
			cleanupRecoverHashes(b, want, found, depth+1)
		}
	case []any:
		for _, child := range v {
			b, _ := json.Marshal(child)
			cleanupRecoverHashes(b, want, found, depth+1)
		}
	}
}
func cleanupRecoverVanishedScopes(c *authingapi.Client, emit func(cleanupEvent)) {
	tenantHash := "dc09fe56a1a8400bf17b36a43edfb69eb78428ccee94857bdf3763ab21b2090a"
	orgHash := "9fa54c3ab0e7ed835577131ac44a9e0b44d2c48d51436e75f444ba1132b1cc2a"
	want := map[string]bool{tenantHash: true, orgHash: true}
	found := map[string]string{}
	for _, resource := range []string{"tenant", "org"} {
		f := cleanupFamily{Family: "audit_" + resource, Path: "/api/v3/get-admin-audit-logs", ID: "requestId", Paged: true, PageKind: "pagination", Extra: map[string]any{"resourceType": resource}}
		logs, state := cleanupInventory(c, f, nil, emit)
		emit(cleanupEvent{Kind: "family", Family: f.Family, State: state, Authority: "none", Count: cleanupCount(len(logs), state), Total: cleanupCount(len(logs), state)})
		if state != "complete" {
			continue
		}
		for _, event := range logs {
			for _, field := range []string{"operationParam", "originValue", "targetValue"} {
				cleanupRecoverHashes(event[field], want, found, 0)
			}
		}
	}
	tenantID, orgID := found[tenantHash], found[orgHash]
	if tenantID != "" {
		_, business, state := cleanupRead(c, "/api/v3/get-tenant", "GET", map[string]any{"tenantId": tenantID})
		emit(cleanupEvent{Kind: "recovered_exact", Family: "tenant", Code: incidentTenantName, Hash: tenantHash, State: state, Authority: "none", Business: business, Count: -1, Total: -1})
	} else {
		emit(cleanupEvent{Kind: "gap", Family: "tenant", Code: incidentTenantName, Hash: tenantHash, State: "exact-id-not-recovered", Authority: "none", Count: -1, Total: -1})
	}
	if tenantID != "" && orgID != "" {
		_, business, state := cleanupRead(c, "/api/v3/get-organization", "GET", map[string]any{"organizationCode": orgID, "tenantId": tenantID})
		emit(cleanupEvent{Kind: "recovered_exact", Family: "tenant_organizations", Hash: orgHash, ScopeHash: tenantHash, State: state, Authority: "none", Business: business, Count: -1, Total: -1})
	} else {
		emit(cleanupEvent{Kind: "gap", Family: "tenant_organizations", Hash: orgHash, ScopeHash: tenantHash, State: "exact-id-not-recovered", Authority: "none", Count: -1, Total: -1})
	}
}
