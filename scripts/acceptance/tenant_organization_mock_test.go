package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

type mockTenantOrganization struct {
	sync.Mutex
	tenant                           mockTenant
	code, name, description, variant string
	creates, updates, deletes        int
	events                           []string
}

func (m *mockTenantOrganization) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v3/get-management-token" || r.URL.Path == "/api/v3/list-tenants" || r.URL.Path == "/api/v3/get-tenant" || r.URL.Path == "/api/v3/create-tenant" || r.URL.Path == "/api/v3/update-tenant" || r.URL.Path == "/api/v3/delete-tenant" || r.URL.Path == "/api/v3/list-tenant-users" || r.URL.Path == "/api/v3/list-tenant-admin" || r.URL.Path == "/api/v3/list-organizations" {
		m.tenant.serve(w, r)
		return
	}
	m.Lock()
	defer m.Unlock()
	m.events = append(m.events, r.Method+" "+r.URL.Path)
	reply := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": data}) }
	if m.tenant.id == "" {
		http.Error(w, "missing tenant", 500)
		return
	}
	switch r.URL.Path {
	case "/api/v3/get-organization":
		if r.Method != "GET" || r.URL.Query().Get("tenantId") != m.tenant.id || r.URL.Query().Get("organizationCode") != tenantOrgTestCode {
			http.Error(w, "wrong identity", 500)
			return
		}
		if m.code == "" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		m.respond(w)
	case "/api/v3/create-organization", "/api/v3/update-organization", "/api/v3/delete-organization":
		var v struct {
			Tenant      string          `json:"tenantId"`
			Code        string          `json:"organizationCode"`
			Name        string          `json:"organizationName"`
			Description string          `json:"description"`
			Metadata    *map[string]any `json:"metadata"`
		}
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&v) != nil || v.Tenant != m.tenant.id || v.Code != tenantOrgTestCode {
			http.Error(w, "wrong mutation scope", 500)
			return
		}
		if r.URL.Path == "/api/v3/create-organization" {
			if m.code != "" || v.Name != v.Code || v.Description != "hermesacc ownership "+v.Code || v.Metadata == nil {
				http.Error(w, "unsafe create", 500)
				return
			}
			m.code, m.name, m.description = v.Code, v.Name, v.Description
			m.creates++
			m.tenant.orgs = true
			m.respond(w)
			return
		}
		if m.code == "" {
			http.Error(w, "missing org", 500)
			return
		}
		if r.URL.Path == "/api/v3/update-organization" {
			if v.Description != m.description || v.Name != tenantOrgTestCode+"-updated" && v.Name != tenantOrgTestCode+"-updated-drift" {
				http.Error(w, "unsafe update", 500)
				return
			}
			m.name = v.Name
			m.updates++
			m.respond(w)
			return
		}
		if m.variant != "" || m.tenant.id == "" {
			http.Error(w, "unsafe cascade", 500)
			return
		}
		m.code = ""
		m.deletes++
		m.tenant.orgs = false
		reply(map[string]bool{"success": true})
	case "/api/v3/list-children-departments", "/api/v3/get-all-departments", "/api/v3/list-department-members":
		q := r.URL.Query()
		if r.Method != "GET" || q.Get("tenantId") != m.tenant.id || q.Get("organizationCode") != m.code || q.Get("departmentId") != "root" {
			http.Error(w, "wrong inventory scope", 500)
			return
		}
		if r.URL.Path == "/api/v3/list-department-members" && (q.Get("page") != "1" || q.Get("limit") != "1" || q.Get("includeChildrenDepartments") != "true") {
			http.Error(w, "wrong member scope", 500)
			return
		}
		if m.variant == "incomplete" {
			reply(map[string]any{"totalCount": 1, "list": []any{}})
			return
		}
		occupied := m.variant == "children" && r.URL.Path != "/api/v3/list-department-members" || m.variant == "members" && r.URL.Path == "/api/v3/list-department-members"
		list := []any{}
		if occupied {
			list = append(list, map[string]string{"id": "foreign"})
		}
		reply(map[string]any{"totalCount": len(list), "list": list})
	default:
		http.Error(w, "unexpected", 500)
	}
}
func (m *mockTenantOrganization) respond(w http.ResponseWriter) {
	name := m.name
	if m.variant == "foreign" {
		name = "foreign"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"tenantId": m.tenant.id, "organizationCode": m.code, "organizationName": name, "description": m.description, "hasChildren": m.variant == "children", "membersCount": map[bool]int{true: 1, false: 0}[m.variant == "members"]}})
}
