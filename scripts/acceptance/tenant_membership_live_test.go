package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

const membershipTestUser = "hermesacc-fedcba0987654321"

type mockTenantMembership struct {
	sync.Mutex
	tenant        mockTenant
	user          mockUser
	member        bool
	memberID      string
	adds, removes int
	badInventory  bool
	rowCorrupt    string
	events        []string
}

func (m *mockTenantMembership) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.events = append(m.events, r.Method+" "+r.URL.Path)
	reply := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": data}) }
	switch r.URL.Path {
	case "/api/v3/get-tenant-user":
		if r.URL.Query().Get("tenantId") != m.tenant.id || r.URL.Query().Get("linkUserId") != m.user.id || !m.member {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		reply(map[string]any{"tenantId": m.tenant.id, "linkUserId": m.user.id, "memberId": m.memberID, "isTenantAdmin": false})
	case "/api/v3/add-tenant-users", "/api/v3/remove-tenant-users":
		var body struct {
			TenantID    string   `json:"tenantId"`
			LinkUserIDs []string `json:"linkUserIds"`
			MemberIDs   []string `json:"memberIds"`
		}
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || body.TenantID == "" || body.TenantID != m.tenant.id || m.user.id == "" {
			http.Error(w, "unsafe relation", 500)
			return
		}
		if r.URL.Path == "/api/v3/add-tenant-users" {
			if m.member || len(body.LinkUserIDs) != 1 || body.LinkUserIDs[0] != m.user.id {
				http.Error(w, "unsafe add", 500)
				return
			}
			m.adds++
			m.member = true
			m.memberID = fmt.Sprintf("member-%d", m.adds)
		} else {
			if !m.member || len(body.MemberIDs) != 1 || body.MemberIDs[0] != m.memberID {
				http.Error(w, "unsafe detach", 500)
				return
			}
			m.removes++
			m.member = false
		}
		reply(map[string]bool{"success": true})
	case "/api/v3/list-tenant-users":
		var v struct {
			TenantID string `json:"tenantId"`
			Options  struct {
				Pagination struct {
					Page  int `json:"page"`
					Limit int `json:"limit"`
				} `json:"pagination"`
			} `json:"options"`
		}
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&v) != nil || v.TenantID != m.tenant.id || v.Options.Pagination.Page != 1 || v.Options.Pagination.Limit != 1 && v.Options.Pagination.Limit != 100 {
			http.Error(w, "unsafe inventory", 500)
			return
		}
		list := []any{}
		if m.member {
			row := map[string]any{"tenantId": m.tenant.id, "memberId": m.memberID, "linkUserId": m.user.id, "isTenantAdmin": false}
			switch m.rowCorrupt {
			case "foreign-tenant":
				row["tenantId"] = "foreign"
			case "foreign-user":
				row["linkUserId"] = "foreign"
			case "missing-member":
				delete(row, "memberId")
			case "missing-admin":
				delete(row, "isTenantAdmin")
			case "admin":
				row["isTenantAdmin"] = true
			}
			list = append(list, row)
		}
		count := len(list)
		if m.badInventory && v.Options.Pagination.Limit == 100 {
			count++
		}
		reply(map[string]any{"totalCount": count, "list": list})
	case "/api/v3/delete-users-batch":
		if m.member || m.tenant.id != "" {
			http.Error(w, "unsafe user deletion", 500)
			return
		}
		m.user.serve(w, r)
	case "/api/v3/delete-tenant":
		m.tenant.members = m.member
		m.tenant.serve(w, r)
	case "/api/v3/create-user":
		var v struct {
			Username string `json:"username"`
			Nickname string `json:"nickname"`
			Password string `json:"password"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil || m.user.id != "" || v.Username != membershipTestUser || v.Nickname != membershipTestUser || v.Password != "" {
			http.Error(w, "invalid create", 500)
			return
		}
		m.user.id = "mock-user-id"
		m.user.username = v.Username
		m.user.nickname = v.Nickname
		m.user.respond(w)
	case "/api/v3/get-management-token", "/api/v3/get-user", "/api/v3/update-user":
		m.user.serve(w, r)
	default:
		m.tenant.serve(w, r)
	}
}
func TestMockTenantMembershipTerraformLifecycle(t *testing.T) {
	m := &mockTenantMembership{}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	e := runTenantMembershipTrace(t.TempDir(), mockUserCredentials(s.URL), membershipTestUser, tenantTestName)
	if e != nil {
		t.Fatalf("%v events=%v", e, m.events)
	}
	m.Lock()
	defer m.Unlock()
	if m.member || m.user.id != "" || m.tenant.id != "" || m.adds != 2 || m.removes != 2 || m.tenant.deletes != 1 || m.user.deletes != 1 {
		t.Fatalf("incomplete lifecycle adds=%d removes=%d tenant=%d user=%d", m.adds, m.removes, m.tenant.deletes, m.user.deletes)
	}
	events := strings.Join(m.events, ",")
	if strings.LastIndex(events, "POST /api/v3/remove-tenant-users") > strings.Index(events, "POST /api/v3/delete-tenant") || strings.Index(events, "POST /api/v3/delete-tenant") > strings.Index(events, "POST /api/v3/delete-users-batch") {
		t.Fatal("unsafe teardown order")
	}
}
func TestTenantMembershipUnknownInventoryFailsClosed(t *testing.T) {
	m := &mockTenantMembership{badInventory: true}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	e := runTenantMembershipTrace(t.TempDir(), mockUserCredentials(s.URL), membershipTestUser, tenantTestName)
	if e == nil || !strings.Contains(e.Error(), "phase=verify-created") || !strings.Contains(e.Error(), "cleanup=incomplete") || m.removes != 0 || m.tenant.deletes != 0 || m.user.deletes != 0 {
		t.Fatalf("unknown inventory accepted: %v", e)
	}
	for _, secret := range []string{"credential-marker", "key-marker", "mock-token"} {
		if strings.Contains(e.Error(), secret) {
			t.Fatal("credential leaked")
		}
	}
}
func TestTenantMembershipWrongPinnedMemberNeverDetaches(t *testing.T) {
	m := &mockTenantMembership{tenant: mockTenant{id: "tenant-unique-id", name: tenantTestName}, user: mockUser{id: "mock-user-id", username: membershipTestUser, nickname: membershipTestUser}, member: true, memberID: "replacement"}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	if e := detachTenantMember(mockUserClient(t, s.URL), m.tenant.id, m.user.id, "stale"); e == nil || m.removes != 0 {
		t.Fatal("stale member ID detached")
	}
	if e := detachTenantMember(mockUserClient(t, s.URL), m.tenant.id, m.user.id, ""); e == nil || m.removes != 0 {
		t.Fatal("unknown member ID detached")
	}
}

func TestTenantMembershipMalformedInventoryNeverDetaches(t *testing.T) {
	for _, variant := range []string{"foreign-tenant", "foreign-user", "missing-member", "missing-admin", "admin"} {
		t.Run(variant, func(t *testing.T) {
			m := &mockTenantMembership{tenant: mockTenant{id: "tenant-unique-id", name: tenantTestName}, user: mockUser{id: "mock-user-id", username: membershipTestUser, nickname: membershipTestUser}, member: true, memberID: "pinned", rowCorrupt: variant}
			s := httptest.NewServer(http.HandlerFunc(m.serve))
			defer s.Close()
			if e := detachTenantMember(mockUserClient(t, s.URL), m.tenant.id, m.user.id, m.memberID); e == nil || m.removes != 0 {
				t.Fatal("malformed inventory permitted detach")
			}
		})
	}
}
func TestDestructiveLiveTenantMembershipTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires explicit destructive sandbox opt-in")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if e := destructiveGuard(*destructiveLive, env); e != nil {
		t.Fatal(e)
	}
	user, e := newGroupCode()
	if e != nil {
		t.Fatal("user name generation failed")
	}
	tenant, e := newGroupCode()
	if e != nil {
		t.Fatal("tenant name generation failed")
	}
	if e := runTenantMembershipTrace(t.TempDir(), env, user, tenant); e != nil {
		t.Fatal(e)
	}
}
