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

// Reject parent deletion while the relation exists; observe both teardown order
// and that revoking a membership never deletes the user.
type mockMembership struct {
	sync.Mutex
	user    mockUser
	group   mockGroup
	member  bool
	events  []string
	badPage bool
}

func (m *mockMembership) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.events = append(m.events, r.Method+" "+r.URL.Path)
	switch r.URL.Path {
	case "/api/v3/list-group-members":
		if r.URL.Query().Get("code") != m.group.code || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "100" {
			http.Error(w, "wrong group inventory scope", 500)
			return
		}
		list := []any{}
		if m.member {
			list = append(list, map[string]string{"userId": m.user.id})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": len(list), "list": list}})
	case "/api/v3/get-user-groups":
		if r.URL.Query().Get("userId") != m.user.id || m.user.id == "" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		list := []any{}
		if m.member {
			list = append(list, map[string]string{"code": m.group.code})
		}
		count := len(list)
		if m.badPage {
			count++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": count, "list": list}})
	case "/api/v3/add-group-members", "/api/v3/remove-group-members":
		var v struct {
			Code    string   `json:"code"`
			UserIds []string `json:"userIds"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil || v.Code != m.group.code || m.group.code == "" || m.user.id == "" || len(v.UserIds) != 1 || v.UserIds[0] != m.user.id {
			http.Error(w, "invalid relation", 500)
			return
		}
		if r.URL.Path == "/api/v3/add-group-members" {
			if m.member {
				http.Error(w, "duplicate", 500)
				return
			}
			m.member = true
		} else {
			if !m.member {
				http.Error(w, "absent", 500)
				return
			}
			m.member = false
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	case "/api/v3/delete-groups-batch", "/api/v3/delete-users-batch":
		if m.member {
			http.Error(w, "parent deleted with membership", 500)
			return
		}
		if r.URL.Path == "/api/v3/delete-groups-batch" {
			m.group.serve(w, r)
		} else {
			m.user.serve(w, r)
		}
	case "/api/v3/get-group", "/api/v3/create-group", "/api/v3/update-group":
		m.group.serve(w, r)
	default:
		m.user.serve(w, r)
	}
}
func TestMockGroupMemberTerraformLifecycle(t *testing.T) {
	m := &mockMembership{}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	err := runGroupMemberTrace(t.TempDir(), mockUserCredentials(s.URL), "hermesacc-1234567890abcdef", "hermesacc-fedcba0987654321")
	if err != nil {
		m.Lock()
		events := append([]string(nil), m.events...)
		m.Unlock()
		t.Fatalf("%v events=%v", err, events)
	}
	m.Lock()
	defer m.Unlock()
	if m.member || m.group.code != "" || m.user.id != "" || m.user.deletes != 1 || m.group.deletes != 1 {
		t.Fatal("relation or parents remain")
	}
	events := strings.Join(m.events, ",")
	for _, endpoint := range []string{"create-user", "create-group", "add-group-members", "get-user-groups", "remove-group-members", "delete-groups-batch", "delete-users-batch"} {
		if !strings.Contains(events, "/api/v3/"+endpoint) {
			t.Fatalf("missing %s", endpoint)
		}
	}
	if strings.Count(events, "POST /api/v3/add-group-members") != 2 {
		t.Fatal("drift not restored exactly once")
	}
	revoke := strings.LastIndex(events, "POST /api/v3/remove-group-members")
	if revoke < 0 || strings.Index(events, "POST /api/v3/delete-groups-batch") < revoke || strings.Index(events, "POST /api/v3/delete-users-batch") < revoke {
		t.Fatal("parent deleted before relation")
	}
}
func TestMockGroupMemberIncompleteListingFailsClosed(t *testing.T) {
	m := &mockMembership{badPage: true}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	err := runGroupMemberTrace(t.TempDir(), mockUserCredentials(s.URL), "hermesacc-1234567890abcdef", "hermesacc-fedcba0987654321")
	if err == nil {
		t.Fatal("incomplete membership inventory accepted")
	}
	for _, secret := range []string{"credential-marker", "key-marker", "mock-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("secret leaked")
		}
	}
}
func TestDestructiveLiveGroupMemberTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	user, err := newGroupCode()
	if err != nil {
		t.Fatal("random username generation failed")
	}
	group, err := newGroupCode()
	if err != nil || user == group {
		t.Fatal("random group code generation failed")
	}
	if err := runGroupMemberTrace(t.TempDir(), env, user, group); err != nil {
		t.Fatal(err)
	}
}
