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

type mockPolicyRelation struct {
	sync.Mutex
	policy     mockPolicy
	user       mockUser
	authorized bool
	events     []string
	incomplete bool
	wrongType  bool
}

func (m *mockPolicyRelation) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.events = append(m.events, r.Method+" "+r.URL.Path)
	switch r.URL.Path {
	case "/api/v3/authorize-data-policies":
		var v struct {
			IDs     []string `json:"policyIds"`
			Targets []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"targetList"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil || len(v.IDs) != 1 || v.IDs[0] != m.policy.policyID || len(v.Targets) != 1 || v.Targets[0].ID != m.user.id || v.Targets[0].Type != "USER" {
			http.Error(w, "invalid authorize", 500)
			return
		}
		m.authorized = true
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	case "/api/v3/revoke-data-policy":
		var v struct {
			Policy string `json:"policyId"`
			ID     string `json:"targetIdentifier"`
			Type   string `json:"targetType"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil || v.Policy != m.policy.policyID || v.ID != m.user.id || v.Type != "USER" {
			http.Error(w, "invalid revoke", 500)
			return
		}
		m.authorized = false
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	case "/api/v3/list-data-policy-targets":
		if r.URL.Query().Get("policyId") != m.policy.policyID || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "50" {
			http.Error(w, "scope", 500)
			return
		}
		list := []any{}
		if m.authorized {
			list = append(list, map[string]string{"targetIdentifier": m.user.id, "targetType": "USER"})
		}
		if m.wrongType {
			list = append(list, map[string]string{"targetIdentifier": m.user.id, "targetType": "GROUP"})
		}
		total := len(list)
		if m.incomplete {
			total++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": total, "list": list}})
	case "/api/v3/delete-data-policy", "/api/v3/delete-data-resource", "/api/v3/delete-permission-namespace", "/api/v3/delete-users-batch":
		if m.authorized {
			http.Error(w, "target still attached", 500)
			return
		}
		fallthrough
	default:
		if strings.Contains(r.URL.Path, "user") {
			m.user.serve(w, r)
		} else {
			m.policy.serve(w, r)
		}
	}
}
func TestDataPolicyAssignmentIncompleteInventoryRetainsParents(t *testing.T) {
	m := &mockPolicyRelation{incomplete: true}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	err := runDataPolicyAssignmentTrace(t.TempDir(), mockUserCredentials(s.URL), namespaceTestCode, "hermesacc-1234567890abcdef")
	if err == nil || m.policy.policyDeletes != 0 || m.policy.resourceDeletes != 0 || m.policy.namespaceDeletes != 0 || m.user.deletes != 0 {
		t.Fatal("incomplete target inventory authorized teardown")
	}
}
func TestMockDataPolicyAssignmentTerraformLifecycle(t *testing.T) {
	m := &mockPolicyRelation{}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	if e := runDataPolicyAssignmentTrace(t.TempDir(), mockUserCredentials(s.URL), namespaceTestCode, "hermesacc-1234567890abcdef"); e != nil {
		t.Fatalf("%v events=%v", e, m.events)
	}
	if m.authorized || m.policy.policyDeletes != 1 || m.policy.resourceDeletes != 1 || m.policy.namespaceDeletes != 1 || m.user.deletes != 1 {
		t.Fatal("assignment lifecycle incomplete")
	}
	events := strings.Join(m.events, ",")
	if strings.Count(events, "POST /api/v3/authorize-data-policies") != 2 || strings.Count(events, "POST /api/v3/revoke-data-policy") != 2 || strings.Index(events, "POST /api/v3/delete-data-policy") < strings.LastIndex(events, "POST /api/v3/revoke-data-policy") {
		t.Fatal("assignment ordering incorrect")
	}
}
