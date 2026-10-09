package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type mockDepartmentRelation struct {
	sync.Mutex
	dept      mockDepartment
	user      mockUser
	member    bool
	events    []string
	truncated bool
}

func (m *mockDepartmentRelation) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.events = append(m.events, r.Method+" "+r.URL.Path)
	switch r.URL.Path {
	case "/api/v3/add-department-members", "/api/v3/remove-department-members":
		var v struct {
			Org   string   `json:"organizationCode"`
			Dept  string   `json:"departmentId"`
			Users []string `json:"userIds"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil || v.Org != departmentTestCode || v.Dept != m.dept.id || len(v.Users) != 1 || v.Users[0] != m.user.id || m.user.id == "" {
			http.Error(w, "invalid relation", 500)
			return
		}
		m.member = r.URL.Path == "/api/v3/add-department-members"
		m.dept.members = m.member
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	case "/api/v3/list-department-members":
		if r.URL.Query().Get("departmentId") == "root" {
			m.dept.org.serve(w, r)
			return
		}
		if r.URL.Query().Get("organizationCode") != departmentTestCode || r.URL.Query().Get("departmentId") != m.dept.id || r.URL.Query().Get("page") != "1" {
			http.Error(w, "scope", 500)
			return
		}
		entries := []any{}
		if m.member {
			entries = append(entries, map[string]string{"userId": m.user.id})
		}
		count := len(entries)
		if m.truncated {
			count++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": count, "list": entries}})
	case "/api/v3/delete-users-batch", "/api/v3/delete-department", "/api/v3/delete-organization":
		if m.member {
			http.Error(w, "relation attached", 500)
			return
		}
		fallthrough
	default:
		if strings.Contains(r.URL.Path, "user") {
			m.user.serve(w, r)
		} else {
			m.dept.serve(w, r)
		}
	}
}
func TestDepartmentMemberScopeRejectsTruncatedMatch(t *testing.T) {
	m := &mockDepartmentRelation{member: true, truncated: true}
	m.dept.id = "hermesacc-fedcba9876543210"
	m.user.id = "mock-user-id"
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	c := mockUserClient(t, s.URL)
	if _, err := directDepartmentMember(c, departmentTestCode, m.dept.id, m.user.id); err == nil {
		t.Fatal("accepted match from incomplete department page")
	}
}
func TestDepartmentMemberIncompleteInventoryRetainsParents(t *testing.T) {
	m := &mockDepartmentRelation{truncated: true}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	err := runDepartmentMemberTrace(t.TempDir(), mockUserCredentials(s.URL), departmentTestCode, departmentTestCode, "hermesacc-1234567890abcdef")
	if err == nil || m.dept.deletes != 0 || m.dept.org.deletes != 0 || m.user.deletes != 0 {
		t.Fatal("incomplete membership authorized parent teardown")
	}
}
func TestMockDepartmentMemberTerraformLifecycle(t *testing.T) {
	m := &mockDepartmentRelation{}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	if err := runDepartmentMemberTrace(t.TempDir(), mockUserCredentials(s.URL), departmentTestCode, departmentTestCode, "hermesacc-1234567890abcdef"); err != nil {
		t.Fatalf("%v events=%v", err, m.events)
	}
	if m.member || m.user.id != "" || m.user.deletes != 1 || m.dept.id != "" || m.dept.org.code != "" || m.dept.deletes != 1 || m.dept.org.deletes != 1 {
		t.Fatal("incomplete relation teardown")
	}
	events := strings.Join(m.events, ",")
	if strings.Count(events, "POST /api/v3/add-department-members") != 2 || strings.Count(events, "POST /api/v3/remove-department-members") != 2 || strings.Index(events, "POST /api/v3/delete-department") < strings.LastIndex(events, "POST /api/v3/remove-department-members") {
		t.Fatal("relation lifecycle order incorrect")
	}
}
