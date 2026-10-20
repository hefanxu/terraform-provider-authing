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

const departmentTestCode = organizationTestCode

type mockDepartment struct {
	sync.Mutex
	org                                    mockOrganization
	id, name, description                  string
	creates, updates, deletes              int
	children, members, incomplete, foreign bool
	unsafeAfterCreate                      bool
	mismatchOpenID                         bool
	paths                                  []string
}

func (m *mockDepartment) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.URL.Path, "department") || r.URL.Query().Get("departmentId") == "root" && (r.URL.Path == "/api/v3/list-department-members" || r.URL.Path == "/api/v3/list-children-departments" || r.URL.Path == "/api/v3/get-all-departments") {
		m.org.serve(w, r)
		return
	}
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	query := r.URL.Query()
	if query.Get("organizationCode") != "" && query.Get("organizationCode") != departmentTestCode {
		http.Error(w, "wrong scope", 500)
		return
	}
	switch r.URL.Path {
	case "/api/v3/create-department":
		var v struct {
			OrganizationCode string         `json:"organizationCode"`
			OpenID           string         `json:"openDepartmentId"`
			IDType           string         `json:"departmentIdType"`
			Name             string         `json:"name"`
			Parent           string         `json:"parentDepartmentId"`
			Description      string         `json:"description"`
			Metadata         map[string]any `json:"metadata"`
		}
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&v) != nil || m.id != "" || v.OrganizationCode != departmentTestCode || v.OpenID != departmentTestCode || v.IDType != "" || v.Name != departmentTestCode || v.Parent != "root" || v.Description != "hermesacc ownership "+departmentTestCode || v.Metadata == nil {
			http.Error(w, "invalid create mapping", 500)
			return
		}
		m.id, m.name, m.description = "hermesacc-fedcba9876543210", v.Name, v.Description
		if m.unsafeAfterCreate {
			m.children = true
		}
		m.creates++
		m.org.Lock()
		m.org.children = true
		m.org.Unlock()
		m.respond(w)
	case "/api/v3/get-department":
		if r.Method != "GET" || m.id == "" || query.Get("departmentId") != m.id {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		m.respond(w)
	case "/api/v3/update-department":
		var v struct {
			OrganizationCode string `json:"organizationCode"`
			ID               string `json:"departmentId"`
			Name             string `json:"name"`
			Parent           string `json:"parentDepartmentId"`
		}
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&v) != nil || v.OrganizationCode != departmentTestCode || v.ID != m.id || v.Parent != "root" {
			http.Error(w, "invalid update", 500)
			return
		}
		m.name = v.Name
		m.updates++
		m.respond(w)
	case "/api/v3/list-children-departments", "/api/v3/get-all-departments":
		if query.Get("departmentId") != m.id {
			http.Error(w, "wrong child scope", 500)
			return
		}
		m.inventory(w, m.children)
	case "/api/v3/list-department-members":
		if query.Get("departmentId") != m.id || query.Get("page") != "1" || query.Get("limit") != "1" || query.Get("includeChildrenDepartments") != "true" {
			http.Error(w, "wrong member scope", 500)
			return
		}
		m.inventory(w, m.members)
	case "/api/v3/delete-department":
		var v struct {
			OrganizationCode string `json:"organizationCode"`
			ID               string `json:"departmentId"`
		}
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&v) != nil || v.OrganizationCode != departmentTestCode || v.ID != m.id || m.children || m.members || m.foreign || m.incomplete {
			http.Error(w, "unsafe delete", 500)
			return
		}
		m.id = ""
		m.deletes++
		m.org.Lock()
		m.org.children = false
		m.org.Unlock()
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	default:
		http.Error(w, "unexpected", 404)
	}
}
func (m *mockDepartment) respond(w http.ResponseWriter) {
	name := m.name
	if m.foreign {
		name = "foreign"
	}
	openID := departmentTestCode
	if m.mismatchOpenID {
		openID = "foreign"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"departmentId": m.id, "openDepartmentId": openID, "organizationCode": departmentTestCode, "name": name, "parentDepartmentId": "root", "description": m.description, "hasChildren": m.children, "membersCount": 0}})
}
func (m *mockDepartment) inventory(w http.ResponseWriter, occupied bool) {
	if m.incomplete {
		fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":1,"list":[]}}`)
		return
	}
	list := []any{}
	if occupied {
		list = append(list, map[string]string{"departmentId": "foreign"})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": len(list), "list": list}})
}
func TestMockDepartmentTerraformTrace(t *testing.T) {
	m := &mockDepartment{}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	err := runDepartmentTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": s.URL}, departmentTestCode)
	if err != nil {
		t.Fatalf("%v; department paths=%v; parent paths=%v", err, m.paths, m.org.paths)
	}
	if m.creates != 1 || m.deletes != 1 || m.updates < 3 || m.id != "" || m.org.code != "" || m.org.deletes != 1 {
		t.Fatal("lifecycle did not update in place and remove department before organization")
	}
}
func TestDepartmentCleanupRejectsUnsafeInventory(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		children, members, incomplete, foreign bool
	}{{name: "children", children: true}, {name: "members", members: true}, {name: "incomplete", incomplete: true}, {name: "foreign", foreign: true}, {name: "missing-marker"}} {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockDepartment{id: departmentTestCode, name: departmentTestCode, description: "hermesacc ownership " + departmentTestCode, children: tc.children, members: tc.members, incomplete: tc.incomplete, foreign: tc.foreign}
			if tc.name == "missing-marker" {
				m.description = ""
			}
			s := httptest.NewServer(http.HandlerFunc(m.serve))
			defer s.Close()
			if cleanupDepartment(s.URL, departmentTestCode, departmentTestCode, "hermesacc ownership "+departmentTestCode) == nil || m.deletes != 0 {
				t.Fatal("unsafe department deletion accepted")
			}
		})
	}
}
func TestDepartmentTraceRefusesOccupiedChildBeforeDestroy(t *testing.T) {
	m := &mockDepartment{unsafeAfterCreate: true}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	err := runDepartmentTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": s.URL}, departmentTestCode)
	if err == nil || !strings.Contains(err.Error(), "phase=verify-safe-before-destroy") || !strings.Contains(err.Error(), "cleanup=incomplete") || m.deletes != 0 || m.org.deletes != 0 {
		t.Fatalf("unsafe tree removed or failure lost: %v", err)
	}
}
func TestDepartmentTraceRefusesExistingOrganization(t *testing.T) {
	m := &mockDepartment{}
	m.org.code = departmentTestCode
	m.org.name = departmentTestCode
	m.org.description = "hermesacc ownership " + departmentTestCode
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	if err := runDepartmentTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": s.URL}, departmentTestCode); err == nil || m.deletes != 0 || m.org.deletes != 0 {
		t.Fatal("preexisting parent accepted or deleted")
	}
}
func TestDepartmentTraceFailedCreateWithoutStateNeverDeletes(t *testing.T) {
	m := &mockDepartment{mismatchOpenID: true}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	err := runDepartmentTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": s.URL}, departmentTestCode)
	if err == nil || !strings.Contains(err.Error(), "phase=apply-create") || !strings.Contains(err.Error(), "cleanup=incomplete") || m.deletes != 0 || m.org.deletes != 0 || m.creates != 1 {
		t.Fatalf("unowned create was cleaned or first failure lost: %v", err)
	}
}
func TestDestructiveLiveDepartmentTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	code, err := newGroupCode()
	if err != nil {
		t.Fatal("code generation failed")
	}
	// The department and organization are distinct generated identities.
	dept, err := newGroupCode()
	if err != nil || code == dept {
		t.Fatal("department ID generation failed")
	}
	if err := runDepartmentTrace(t.TempDir(), env, code, dept); err != nil {
		t.Fatal(err)
	}
}
