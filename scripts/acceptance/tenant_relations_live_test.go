package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const tenantOrgTestCode = "hermesacc-abcdef0123456789"

func TestTenantAdminNeverRevokesSoleOrUnknownAdmin(t *testing.T) {
	for _, variant := range []string{"sole", "incomplete", "foreign", "changed"} {
		t.Run(variant, func(t *testing.T) {
			revokes := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v3/get-management-token":
					fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
				case "/api/v3/get-tenant-user":
					member := "pinned"
					if variant == "changed" {
						member = "replacement"
					}
					fmt.Fprintf(w, `{"statusCode":200,"data":{"tenantId":"tenant-id","linkUserId":"user-id","memberId":%q,"isTenantAdmin":true}}`, member)
				case "/api/v3/list-tenant-admin":
					if variant == "incomplete" {
						fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":2,"list":[]}}`)
						return
					}
					if variant == "foreign" {
						fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":2,"list":[{"tenantId":"other","memberId":"pinned","linkUserId":"user-id"},{"tenantId":"tenant-id","memberId":"guardian","linkUserId":"guardian-user"}]}}`)
						return
					}
					fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":1,"list":[{"tenantId":"tenant-id","memberId":"pinned","linkUserId":"user-id"}]}}`)
				case "/api/v3/delete-tenant-admin":
					revokes++
					fmt.Fprint(w, `{"statusCode":200}`)
				default:
					http.Error(w, "unexpected", 500)
				}
			}))
			defer s.Close()
			if err := revokeOwnedTenantAdmin(mockUserClient(t, s.URL), "tenant-id", "user-id", "pinned"); err == nil || revokes != 0 {
				t.Fatal("unsafe admin revoked")
			}
		})
	}
}
func TestTenantAdminRevokeRequiresGuardianAndPreservesMembership(t *testing.T) {
	admin := true
	revokes := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/get-management-token":
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
		case "/api/v3/get-tenant-user":
			fmt.Fprintf(w, `{"statusCode":200,"data":{"tenantId":"tenant-id","linkUserId":"user-id","memberId":"pinned","isTenantAdmin":%t}}`, admin)
		case "/api/v3/list-tenant-admin":
			fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":2,"list":[{"tenantId":"tenant-id","memberId":"pinned","linkUserId":"user-id"},{"tenantId":"tenant-id","memberId":"guardian","linkUserId":"guardian-user"}]}}`)
		case "/api/v3/delete-tenant-admin":
			var v struct {
				Tenant string `json:"tenantId"`
				Member string `json:"memberId"`
			}
			if json.NewDecoder(r.Body).Decode(&v) != nil || v.Tenant != "tenant-id" || v.Member != "pinned" {
				http.Error(w, "unsafe revoke", 500)
				return
			}
			revokes++
			admin = false
			fmt.Fprint(w, `{"statusCode":200}`)
		default:
			http.Error(w, "unexpected", 500)
		}
	}))
	defer s.Close()
	if err := revokeOwnedTenantAdmin(mockUserClient(t, s.URL), "tenant-id", "user-id", "pinned"); err != nil || revokes != 1 || admin {
		t.Fatalf("guarded revoke failed: %v", err)
	}
}

func TestMockTenantOrganizationTerraformLifecycle(t *testing.T) {
	m := &mockTenantOrganization{}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	if err := runTenantOrganizationTrace(t.TempDir(), postCredentials(s), tenantTestName, tenantOrgTestCode); err != nil {
		t.Fatalf("%v events=%v tenant-paths=%v", err, m.events, m.tenant.paths)
	}
	if m.creates != 1 || m.updates < 3 || m.deletes != 1 || m.tenant.deletes != 1 {
		t.Fatalf("incomplete lifecycle: %+v", m)
	}
}
func TestTenantOrganizationNeverCascadesUnknownInventory(t *testing.T) {
	for _, variant := range []string{"children", "members", "incomplete", "foreign"} {
		t.Run(variant, func(t *testing.T) {
			m := &mockTenantOrganization{tenant: mockTenant{id: "tenant-unique-id", name: tenantTestName}, code: tenantOrgTestCode, name: tenantOrgTestCode, description: "hermesacc ownership " + tenantOrgTestCode, variant: variant}
			s := httptest.NewServer(http.HandlerFunc(m.serve))
			defer s.Close()
			if err := cleanupTenantOrganization(mockUserClient(t, s.URL), m.tenant.id, m.code, m.name, m.description); err == nil || m.deletes != 0 {
				t.Fatal("unsafe organization deleted")
			}
		})
	}
}
func TestTenantOrganizationIncompleteInventoryStopsTeardown(t *testing.T) {
	m := &mockTenantOrganization{variant: "incomplete"}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	err := runTenantOrganizationTrace(t.TempDir(), postCredentials(s), tenantTestName, tenantOrgTestCode)
	if err == nil || !strings.Contains(err.Error(), "phase=verify-created") || !strings.Contains(err.Error(), "cleanup=incomplete") || m.deletes != 0 || m.tenant.deletes != 0 {
		t.Fatalf("incomplete inventory authorized cascade: %v", err)
	}
}
func TestDestructiveLiveTenantOrganizationTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires explicit destructive sandbox opt-in")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	tenant, e := newGroupCode()
	if e != nil {
		t.Fatal("tenant generation failed")
	}
	org, e := newGroupCode()
	if e != nil || org == tenant {
		t.Fatal("organization generation failed")
	}
	if e := runTenantOrganizationTrace(t.TempDir(), env, tenant, org); e != nil {
		t.Fatal(e)
	}
}
