package acceptance

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

var cleanupInventoryLive = flag.Bool("authing-cleanup-inventory-sandbox", false, "opt in to complete read-only cleanup inventory")
var cleanupGenerated = regexp.MustCompile(`hermesacc-[0-9a-f]{16}`)

func cleanupLiveGuard(enabled bool, confirm, key, secret string) bool {
	return enabled && confirm == "READ_ONLY_SANDBOX" && key != "" && secret != ""
}
func cleanupCandidate(f cleanupFamily, item map[string]json.RawMessage) cleanupEvent {
	id := cleanupString(item, f.ID)
	name := cleanupString(item, f.Name)
	code := cleanupGenerated.FindString(name)
	if code == "" {
		code = cleanupGenerated.FindString(cleanupString(item, "description"))
	}
	if code == "" {
		code = cleanupGenerated.FindString(cleanupString(item, "appDescription"))
	}
	marker := code != "" && (cleanupString(item, "description") == "hermesacc ownership "+code || cleanupString(item, "appDescription") == "hermesacc ownership "+code)
	if f.Family == "user" || f.Family == "public_account" {
		marker = code != "" && name == code && cleanupString(item, "nickname") == code
	}
	if f.Family == "ext_idp" || f.Family == "tenant" {
		marker = code != "" && name == code
	}
	return cleanupEvent{Kind: "candidate", Family: f.Family, Code: code, Hash: cleanupHash(id), State: "discovered-not-verified", Authority: "none", Marker: marker, Count: 1, Total: 1}
}
func cleanupFamilies() []cleanupFamily {
	families := []cleanupFamily{
		{Family: "group", Path: "/api/v3/list-groups", ID: "code", Name: "code", Get: "/api/v3/get-group", GetID: "code", Paged: true},
		{Family: "user", Path: "/api/v3/list-users", ID: "userId", Name: "username", Get: "/api/v3/get-user", GetID: "userId", Paged: true, PageKind: "options"},
		{Family: "public_account", Path: "/api/v3/list-public-accounts", ID: "userId", Name: "username", Get: "/api/v3/get-public-account", GetID: "userId", Paged: true, PageKind: "options"},
		{Family: "application", Path: "/api/v3/list-applications", ID: "appId", Name: "appName", Get: "/api/v3/get-application", GetID: "appId", Paged: true, Extra: map[string]any{"all": true}},
		{Family: "namespace", Path: "/api/v3/list-namespaces", ID: "code", Name: "code", Get: "/api/v3/get-namespace", GetID: "code", Paged: true},
		{Family: "ext_idp", Path: "/api/v3/list-ext-idp", ID: "id", Name: "name", Get: "/api/v3/get-ext-idp", GetID: "id"},
		{Family: "webhook", Path: "/api/v3/list-webhooks", ID: "webhookId", Name: "name", Get: "/api/v3/get-webhook", GetID: "webhookId", Paged: true},
		{Family: "pipeline_function", Path: "/api/v3/list-pipeline-functions", ID: "funcId", Name: "funcName", Get: "/api/v3/get-pipeline-function", GetID: "funcId", Extra: map[string]any{"scene": "PRE_REGISTER"}},
		{Family: "tenant", Path: "/api/v3/list-tenants", ID: "tenantId", Name: "name", Get: "/api/v3/get-tenant", GetID: "tenantId", Paged: true, PageKind: "strings"},
		{Family: "organization", Path: "/api/v3/list-organizations", ID: "organizationCode", Name: "organizationName", Get: "/api/v3/get-organization", GetID: "organizationCode", Paged: true},
	}
	// The complete published pipeline scene enum is searched only for candidates.
	// Even AUTH_FLOW_FUNCTION here cannot establish auth-flow-function absence.
	for _, scene := range []string{"POST_REGISTER", "PRE_AUTHENTICATION", "POST_AUTHENTICATION", "PRE_OIDC_ID_TOKEN_ISSUED", "PRE_OIDC_ACCESS_TOKEN_ISSUED", "PRE_COMPLETE_USER_INFO", "PRE_MFA_VERIFY", "POST_MFA_VERIFY", "AUTH_FLOW_FUNCTION"} {
		families = append(families, cleanupFamily{Family: "pipeline_function", Path: "/api/v3/list-pipeline-functions", ID: "funcId", Name: "funcName", Get: "/api/v3/get-pipeline-function", GetID: "funcId", Extra: map[string]any{"scene": scene}})
	}
	return families
}
func cleanupEmit(e cleanupEvent) {
	raw, _ := json.Marshal(e)
	fmt.Println("CLEANUP_JSON " + string(raw))
}
func cleanupInspect(c *authingapi.Client, f cleanupFamily, item map[string]json.RawMessage, emit func(cleanupEvent)) {
	e := cleanupCandidate(f, item)
	id := cleanupString(item, f.ID)
	raw, business, state := cleanupRead(c, f.Get, "GET", map[string]any{f.GetID: id})
	e.Business = business
	e.State = state
	var exact map[string]json.RawMessage
	if state == "complete" {
		if json.Unmarshal(raw, &exact) != nil || cleanupString(exact, f.ID) != id || cleanupString(exact, f.Name) != cleanupString(item, f.Name) {
			e.State = "identity-mismatch"
		} else {
			e.State = "candidate-exact-identity"
			e.Marker = cleanupCandidate(f, exact).Marker
		}
	}
	emit(e)
	if e.State != "candidate-exact-identity" {
		return
	}
	if f.Family == "group" {
		members, s := cleanupInventory(c, cleanupFamily{Family: "group_members", Path: "/api/v3/list-group-members", ID: "userId", Paged: true}, map[string]any{"code": id}, emit)
		emit(cleanupEvent{Kind: "dependency", Family: "group_members", Code: e.Code, ScopeHash: e.Hash, State: s, Authority: "none", Count: cleanupCount(len(members), s), Total: cleanupCount(len(members), s)})
		if s == "complete" {
			for _, m := range members {
				emit(cleanupEvent{Kind: "relation", Family: "group_member", Code: e.Code, ScopeHash: e.Hash, Hash: cleanupHash(cleanupString(m, "userId")), State: "candidate-not-authority", Authority: "none", Count: 1, Total: 1})
			}
		}
	}
	if f.Family == "user" {
		groups, s := cleanupInventory(c, cleanupFamily{Family: "user_groups", Path: "/api/v3/get-user-groups", ID: "code"}, map[string]any{"userId": id}, emit)
		emit(cleanupEvent{Kind: "dependency", Family: "user_groups", ScopeHash: e.Hash, Code: e.Code, State: s, Authority: "none", Count: cleanupCount(len(groups), s), Total: cleanupCount(len(groups), s)})
	}
	if f.Family == "tenant" {
		var apps *[]string
		appState := "invalid-appids"
		n := -1
		if json.Unmarshal(exact["appIds"], &apps) == nil && apps != nil {
			n = len(*apps)
			appState = "complete"
		}
		emit(cleanupEvent{Kind: "dependency", Family: "tenant_apps", ScopeHash: e.Hash, State: appState, Authority: "none", Count: n, Total: n})
		for _, dep := range []cleanupFamily{
			{Family: "tenant_users", Path: "/api/v3/list-tenant-users", ID: "memberId", Paged: true, PageKind: "options"},
			{Family: "tenant_admins", Path: "/api/v3/list-tenant-admin", ID: "memberId", Paged: true, PageKind: "strings"},
			{Family: "tenant_organizations", Path: "/api/v3/list-organizations", ID: "organizationCode", Name: "organizationName", Get: "/api/v3/get-organization", GetID: "organizationCode", Paged: true},
		} {
			items, s := cleanupInventory(c, dep, map[string]any{"tenantId": id}, emit)
			emit(cleanupEvent{Kind: "dependency", Family: dep.Family, ScopeHash: e.Hash, State: s, Authority: "none", Count: cleanupCount(len(items), s), Total: cleanupCount(len(items), s)})
			if s != "complete" {
				continue
			}
			for _, d := range items {
				depEvent := cleanupCandidate(dep, d)
				depEvent.ScopeHash = e.Hash
				scope := cleanupString(d, "tenantId")
				depEvent.State = "scoped-candidate"
				if scope != "" && scope != id {
					depEvent.State = "foreign-scope"
				}
				emit(depEvent)
				if dep.Family == "tenant_organizations" && depEvent.State != "foreign-scope" {
					orgID := cleanupString(d, dep.ID)
					orgRaw, b, st := cleanupRead(c, dep.Get, "GET", map[string]any{"organizationCode": orgID, "tenantId": id})
					var org map[string]json.RawMessage
					if st == "complete" {
						if json.Unmarshal(orgRaw, &org) != nil || cleanupString(org, dep.ID) != orgID || cleanupString(org, dep.Name) != cleanupString(d, dep.Name) || cleanupString(org, "tenantId") != "" && cleanupString(org, "tenantId") != id {
							st = "identity-mismatch"
						} else {
							st = "candidate-exact-identity"
						}
					}
					depEvent.State = st
					depEvent.Business = b
					depEvent.Kind = "exact"
					emit(depEvent)
					if st == "candidate-exact-identity" {
						root := cleanupString(org, "departmentId")
						if root == "" {
							emit(cleanupEvent{Kind: "dependency", Family: "organization_root", ScopeHash: depEvent.Hash, State: "missing-root", Authority: "none", Count: -1, Total: -1})
							continue
						}
						for _, rootDep := range []cleanupFamily{
							{Family: "organization_children", Path: "/api/v3/list-children-departments", ID: "departmentId"},
							{Family: "organization_members", Path: "/api/v3/list-department-members", ID: "userId", Paged: true},
						} {
							args := map[string]any{"organizationCode": orgID, "departmentId": root, "tenantId": id}
							if rootDep.Family == "organization_members" {
								args["includeChildrenDepartments"] = true
							}
							ds, dsState := cleanupInventory(c, rootDep, args, emit)
							emit(cleanupEvent{Kind: "dependency", Family: rootDep.Family, ScopeHash: depEvent.Hash, State: dsState, Authority: "none", Count: cleanupCount(len(ds), dsState), Total: cleanupCount(len(ds), dsState)})
						}
					}
				}
			}
		}
	}
}
func cleanupCount(count int, state string) int {
	if state != "complete" {
		return -1
	}
	return count
}
func cleanupKnownCodeGETs(c *authingapi.Client, emit func(cleanupEvent)) {
	for _, target := range []struct{ family, code, path, key string }{
		{"group", "hermesacc-69fb9c423513635b", "/api/v3/get-group", "code"},
		{"group", "hermesacc-c834fa2818addda0", "/api/v3/get-group", "code"},
		{"group", "hermesacc-d4ecff84407c6617", "/api/v3/get-group", "code"},
		{"user", "hermesacc-9628c310fcf35671", "/api/v3/get-user", "userId"},
		{"namespace", "hermesacc-a69a5cb1b971aa8e", "/api/v3/get-namespace", "code"},
		{"public_account", "hermesacc-802de9a7c2297afd", "/api/v3/get-public-account", "userId"},
	} {
		arg := map[string]any{target.key: target.code}
		if target.family == "public_account" || target.family == "user" {
			arg["userIdType"] = "username"
		}
		_, business, state := cleanupRead(c, target.path, "GET", arg)
		emit(cleanupEvent{Kind: "known_exact", Family: target.family, Code: target.code, State: state, Authority: "none", Business: business, Count: 0, Total: 0})
	}
}
func TestReadOnlyCleanupInventory(t *testing.T) {
	if !*cleanupInventoryLive {
		t.Skip("requires isolated read-only cleanup inventory dispatch")
	}
	if !cleanupLiveGuard(*cleanupInventoryLive, os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), os.Getenv("AUTHING_ACCESS_KEY_ID"), os.Getenv("AUTHING_ACCESS_KEY_SECRET")) {
		t.Fatal("cleanup inventory guard rejected input")
	}
	c, err := authingapi.NewClient(authingapi.Options{AccessKeyID: os.Getenv("AUTHING_ACCESS_KEY_ID"), AccessKeySecret: os.Getenv("AUTHING_ACCESS_KEY_SECRET"), Host: os.Getenv("AUTHING_HOST")})
	if err != nil {
		t.Fatal("cleanup inventory client unavailable")
	}
	tenantAbsent := false
	for _, f := range cleanupFamilies() {
		emit := cleanupEmit
		if scene, ok := f.Extra["scene"].(string); ok {
			emit = func(e cleanupEvent) {
				if e.ScopeHash == "" {
					e.ScopeHash = cleanupHash("pipeline-scene:" + scene)
				}
				cleanupEmit(e)
			}
		}
		items, state := cleanupInventory(c, f, nil, emit)
		if f.Family == "tenant" && state == "complete" && len(items) == 0 {
			tenantAbsent = true
		}
		matches := 0
		if state == "complete" {
			for _, known := range cleanupKnownFingerprints[f.Family] {
				count := 0
				for _, item := range items {
					if cleanupHash(cleanupString(item, f.ID)) == known {
						count++
					}
				}
				emit(cleanupEvent{Kind: "known_fingerprint", Family: f.Family, Hash: known, State: "complete", Authority: "none", Count: count, Total: len(items)})
			}
			for _, item := range items {
				if cleanupMatchesEvidence(f, item, cleanupKnownFingerprints[f.Family]) {
					matches++
					cleanupInspect(c, f, item, emit)
				}
			}
		}
		emit(cleanupEvent{Kind: "family", Family: f.Family, State: state, Authority: "none", Count: cleanupCount(matches, state), Total: cleanupCount(len(items), state)})
	}
	cleanupKnownCodeGETs(c, cleanupEmit)
	if tenantAbsent {
		cleanupRecoverVanishedScopes(c, cleanupEmit)
	}
	// No published auth-flow-function enumeration contract. A pipeline inventory is
	// intentionally never used as absence proof for this different resource family.
	cleanupEmit(cleanupEvent{Kind: "gap", Family: "auth_flow_function", Code: "hermesacc-4cba9ad3756d8fa0", State: "no-published-enumeration-or-exact-id", Authority: "none", Count: -1, Total: -1})
	if strings.Contains(os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "DESTRUCTIVE") {
		t.Fatal("read-only boundary")
	}
}
