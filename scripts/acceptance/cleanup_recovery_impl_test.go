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

var cleanupKnownFingerprints = map[string][]string{
	"group":          {"154156ffa12ca4cdc4ee92405637c86df8a3e42c3b30e73b1fe328cfb23027be", "f601b67483075a6a7d9680cc5312f975dca12f8034217ee9a1b01684b74e4f26", "58be0915870e79cd39fb7bb1679905791704875e72d7810495ab63d98cbe7b96"},
	"user":           {"189f388675a6a05c85cecec5041c9ba887558a4d2477724665d156c6b79f99ea"},
	"public_account": {"2571bc3a6b07d4550a4d221fd22e07039d4d9927ddda714d604ec191bb1afeb9"},
	"tenant":         {"dc09fe56a1a8400bf17b36a43edfb69eb78428ccee94857bdf3763ab21b2090a"},
	"organization":   {"9fa54c3ab0e7ed835577131ac44a9e0b44d2c48d51436e75f444ba1132b1cc2a"},
}

func cleanupMatchesEvidence(f cleanupFamily, item map[string]json.RawMessage, expected []string) bool {
	if cleanupCandidate(f, item).Code != "" {
		return true
	}
	h := cleanupHash(cleanupString(item, f.ID))
	for _, known := range expected {
		if known == h {
			return true
		}
	}
	return false
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
