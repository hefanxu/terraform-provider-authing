package acceptance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"terraform-provider-authing/internal/authingapi"
)

// This guard is intentionally narrower than the provider's generic Delete:
// it never revokes the only administrator, never trusts an incomplete page,
// and never infers a target member ID from an admin-list row.
func revokeOwnedTenantAdmin(c *authingapi.Client, tenant, user, pinned string) error {
	if tenant == "" || user == "" || pinned == "" {
		return errors.New("admin identity not pinned")
	}
	status, raw, err := tenantEnvelope(c, "/api/v3/get-tenant-user", http.MethodGet, map[string]string{"tenantId": tenant, "linkUserId": user})
	var member struct {
		Tenant string `json:"tenantId"`
		User   string `json:"linkUserId"`
		ID     string `json:"memberId"`
		Admin  *bool  `json:"isTenantAdmin"`
	}
	if err != nil || status != 200 || json.Unmarshal(raw, &member) != nil || member.Tenant != tenant || member.User != user || member.ID != pinned || member.Admin == nil || !*member.Admin {
		return errors.New("pinned administrator not verified")
	}
	// List all pages, not just the target's page. A sole/critical admin cannot be
	// identified safely by name; require an additional admin before any revoke.
	const limit = 100
	seen := map[string]bool{}
	total := -1
	target := false
	for page := 1; ; page++ {
		status, raw, err = tenantEnvelope(c, "/api/v3/list-tenant-admin", http.MethodPost, map[string]string{"tenantId": tenant, "page": fmt.Sprint(page), "limit": fmt.Sprint(limit)})
		var v struct {
			Total *int `json:"totalCount"`
			List  *[]struct {
				Tenant string `json:"tenantId"`
				ID     string `json:"memberId"`
				User   string `json:"linkUserId"`
			} `json:"list"`
		}
		if err != nil || status != 200 || json.Unmarshal(raw, &v) != nil || v.Total == nil || v.List == nil || *v.Total < 0 {
			return errors.New("admin inventory unknown")
		}
		if total < 0 {
			total = *v.Total
		}
		if *v.Total != total || len(*v.List) != min(limit, total-len(seen)) {
			return errors.New("admin inventory incomplete")
		}
		for _, row := range *v.List {
			if row.Tenant != tenant || row.ID == "" || row.User == "" || seen[row.ID] || row.ID == pinned && row.User != user || row.User == user && row.ID != pinned {
				return errors.New("admin inventory identity mismatch")
			}
			seen[row.ID] = true
			if row.ID == pinned {
				target = true
			}
		}
		if len(seen) == total {
			break
		}
	}
	if !target || total < 2 {
		return errors.New("sole or unlisted administrator cannot be revoked")
	}
	status, raw, err = tenantEnvelope(c, "/api/v3/delete-tenant-admin", http.MethodPost, map[string]string{"tenantId": tenant, "memberId": pinned})
	var result struct {
		Success *bool `json:"success"`
	}
	if err != nil || status != 200 || len(raw) > 0 && (json.Unmarshal(raw, &result) != nil || result.Success != nil && !*result.Success) {
		return errors.New("administrator revoke failed")
	}
	status, raw, err = tenantEnvelope(c, "/api/v3/get-tenant-user", http.MethodGet, map[string]string{"tenantId": tenant, "linkUserId": user})
	var after struct {
		Tenant string `json:"tenantId"`
		User   string `json:"linkUserId"`
		ID     string `json:"memberId"`
		Admin  *bool  `json:"isTenantAdmin"`
	}
	if err != nil || status != 200 || json.Unmarshal(raw, &after) != nil || after.Tenant != tenant || after.User != user || after.ID != pinned || after.Admin == nil || *after.Admin {
		return errors.New("administrator revoke not confirmed on pinned membership")
	}
	return nil
}
