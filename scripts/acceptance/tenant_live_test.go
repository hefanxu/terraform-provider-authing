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

	"terraform-provider-authing/internal/authingapi"
)

const tenantTestName = "hermesacc-1234567890abcdef"

type mockTenant struct {
	sync.Mutex
	id, name                                                                              string
	creates, updates, deletes                                                             int
	members, admins, orgs, apps, broken, partial, foreign, failUpdate, failCreateReadback bool
	paths                                                                                 []string
}

func (m *mockTenant) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	reply := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": data}) }
	empty := func(occupied bool) {
		if m.broken {
			reply(map[string]any{})
			return
		}
		list := []any{}
		if occupied {
			list = append(list, map[string]string{"id": "foreign"})
		}
		reply(map[string]any{"totalCount": len(list), "list": list})
	}
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		reply(map[string]any{"access_token": "mock-token", "expires_in": 3600})
	case "/api/v3/list-tenants":
		if r.Method != "GET" || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "100" {
			http.Error(w, "bad preflight", 500)
			return
		}
		if m.broken {
			reply(map[string]any{})
			return
		}
		list := []any{}
		if m.id != "" {
			list = append(list, map[string]string{"tenantId": m.id, "name": m.name})
		}
		if m.partial {
			reply(map[string]any{"totalCount": 2, "list": list})
			return
		}
		reply(map[string]any{"totalCount": len(list), "list": list})
	case "/api/v3/get-tenant":
		if r.Method != "GET" || m.id == "" || r.URL.Query().Get("tenantId") != m.id {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		name := m.name
		if m.foreign {
			name = "foreign"
		}
		ids := []string{}
		if m.apps {
			ids = append(ids, "foreign-app")
		}
		reply(map[string]any{"tenantId": m.id, "name": name, "appIds": ids})
	case "/api/v3/create-tenant", "/api/v3/update-tenant", "/api/v3/delete-tenant":
		var v struct {
			ID     string    `json:"tenantId"`
			Name   string    `json:"name"`
			AppIDs *[]string `json:"appIds"`
		}
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&v) != nil {
			http.Error(w, "bad body", 500)
			return
		}
		switch r.URL.Path {
		case "/api/v3/create-tenant":
			if m.id != "" || v.Name != tenantTestName || v.AppIDs == nil || len(*v.AppIDs) != 0 {
				http.Error(w, "unsafe create", 500)
				return
			}
			m.id = "tenant-unique-id"
			m.name = v.Name
			m.creates++
			if m.failCreateReadback {
				fmt.Fprint(w, `{"statusCode":200,"data":{}}`)
				return
			}
			reply(map[string]string{"tenantId": m.id})
		case "/api/v3/update-tenant":
			if m.failUpdate {
				http.Error(w, "secret-marker", 500)
				return
			}
			if v.ID != m.id || m.id == "" || v.AppIDs == nil || len(*v.AppIDs) != 0 || v.Name != tenantTestName+"-updated" && v.Name != tenantTestName+"-updated-drift" {
				http.Error(w, "unsafe update", 500)
				return
			}
			m.name = v.Name
			m.updates++
			reply(map[string]bool{"success": true})
		case "/api/v3/delete-tenant":
			if v.ID != m.id || m.foreign || m.apps || m.members || m.admins || m.orgs || m.broken {
				http.Error(w, "unsafe delete", 500)
				return
			}
			m.id = ""
			m.deletes++
			reply(map[string]bool{"success": true})
		}
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
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&v) != nil || v.TenantID != m.id || v.Options.Pagination.Page != 1 || v.Options.Pagination.Limit != 1 {
			http.Error(w, "unsafe member scope", 500)
			return
		}
		empty(m.members)
	case "/api/v3/list-tenant-admin":
		var v struct{ TenantID, Page, Limit string }
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&v) != nil || v.TenantID != m.id || v.Page != "1" || v.Limit != "1" {
			http.Error(w, "unsafe admin scope", 500)
			return
		}
		empty(m.admins)
	case "/api/v3/list-organizations":
		if r.Method != "GET" || r.URL.Query().Get("tenantId") != m.id || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "1" {
			http.Error(w, "unsafe org scope", 500)
			return
		}
		empty(m.orgs)
	default:
		http.Error(w, "unexpected endpoint", 500)
	}
}
func mockTenantClient(t *testing.T, m *mockTenant) (*authingapi.Client, *httptest.Server) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	c, e := authingapi.NewClient(authingapi.Options{AccessKeyID: "key-marker", AccessKeySecret: "credential-marker", Host: s.URL})
	if e != nil {
		t.Fatal(e)
	}
	return c, s
}
func TestMockTenantTerraformLifecycle(t *testing.T) {
	m := &mockTenant{}
	_, s := mockTenantClient(t, m)
	defer s.Close()
	if e := runTenantTrace(t.TempDir(), postCredentials(s), tenantTestName); e != nil {
		t.Fatalf("%v; paths=%v", e, m.paths)
	}
	m.Lock()
	defer m.Unlock()
	if m.id != "" || m.creates != 1 || m.updates < 3 || m.deletes != 1 {
		t.Fatalf("incomplete lifecycle: creates=%d updates=%d deletes=%d", m.creates, m.updates, m.deletes)
	}
}
func TestTenantDeletionRefusesUnsafeInventory(t *testing.T) {
	for _, field := range []string{"members", "admins", "orgs", "apps", "broken", "foreign"} {
		t.Run(field, func(t *testing.T) {
			m := &mockTenant{id: "tenant-unique-id", name: tenantTestName}
			switch field {
			case "members":
				m.members = true
			case "admins":
				m.admins = true
			case "orgs":
				m.orgs = true
			case "apps":
				m.apps = true
			case "broken":
				m.broken = true
			case "foreign":
				m.foreign = true
			}
			c, s := mockTenantClient(t, m)
			defer s.Close()
			if cleanupTenant(c, m.id, tenantTestName) == nil || m.deletes != 0 {
				t.Fatal("unsafe tenant deleted")
			}
		})
	}
}
func TestTenantPreflightNeverDeletesExisting(t *testing.T) {
	m := &mockTenant{id: "preexisting", name: tenantTestName}
	_, s := mockTenantClient(t, m)
	defer s.Close()
	if runTenantTrace(t.TempDir(), postCredentials(s), tenantTestName) == nil || m.deletes != 0 || m.creates != 0 {
		t.Fatal("preexisting tenant modified")
	}
	for _, name := range []string{"default", "hermesacc-123", "hermesacc-1234567890abcdef;"} {
		if runTenantTrace(t.TempDir(), postCredentials(s), name) == nil {
			t.Fatal("invalid generated name accepted")
		}
	}
}
func TestTenantFailedUpdateCleansPinnedIdentity(t *testing.T) {
	m := &mockTenant{failUpdate: true}
	_, s := mockTenantClient(t, m)
	defer s.Close()
	e := runTenantTrace(t.TempDir(), postCredentials(s), tenantTestName)
	if e == nil || !strings.Contains(e.Error(), "phase=apply-name-update") || m.deletes != 1 || m.id != "" {
		t.Fatalf("failure/cleanup invalid: %v deletes=%d", e, m.deletes)
	}
	for _, secret := range []string{"key-marker", "credential-marker", "secret-marker", "mock-token"} {
		if strings.Contains(e.Error(), secret) {
			t.Fatal("response or credential leaked")
		}
	}
}
func TestTenantFailedUpdateRefusesUnsafeCleanup(t *testing.T) {
	m := &mockTenant{failUpdate: true, admins: true}
	_, s := mockTenantClient(t, m)
	defer s.Close()
	e := runTenantTrace(t.TempDir(), postCredentials(s), tenantTestName)
	if e == nil || !strings.Contains(e.Error(), "phase=verify-created") || !strings.Contains(e.Error(), "cleanup=incomplete") || m.deletes != 0 {
		t.Fatalf("unsafe cleanup: %v deletes=%d", e, m.deletes)
	}
}
func TestTenantFailedCreateWithoutStateNeverDeletes(t *testing.T) {
	m := &mockTenant{failCreateReadback: true}
	_, s := mockTenantClient(t, m)
	defer s.Close()
	e := runTenantTrace(t.TempDir(), postCredentials(s), tenantTestName)
	if e == nil || !strings.Contains(e.Error(), "phase=apply-create") || !strings.Contains(e.Error(), "cleanup=unknown") || m.creates != 1 || m.deletes != 0 {
		t.Fatalf("unknown create identity was deleted or failure hidden: %v", e)
	}
	if strings.Contains(e.Error(), "state_id_sha256=") {
		t.Fatal("discovery identity masqueraded as creating-state evidence")
	}
}
func TestTenantIncompletePreflightFailsClosed(t *testing.T) {
	for _, m := range []*mockTenant{{broken: true}, {partial: true, id: "other-id", name: "other-name"}} {
		_, s := mockTenantClient(t, m)
		e := runTenantTrace(t.TempDir(), postCredentials(s), tenantTestName)
		s.Close()
		if e == nil || !strings.Contains(e.Error(), "phase=preflight") || m.creates != 0 || m.deletes != 0 {
			t.Fatalf("incomplete preflight accepted: %v", e)
		}
	}
}

func TestDestructiveLiveTenantTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires explicit destructive sandbox opt-in")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if e := destructiveGuard(*destructiveLive, env); e != nil {
		t.Fatal(e)
	}
	name, e := newGroupCode()
	if e != nil {
		t.Fatal("tenant name generation failed")
	}
	if e = runTenantTrace(t.TempDir(), env, name); e != nil {
		t.Fatal(e)
	}
}
