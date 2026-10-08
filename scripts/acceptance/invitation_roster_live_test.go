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

type rosterFixture struct {
	sync.Mutex
	policy                                                 mockInvitationPolicy
	id, name, policyID                                     string
	deletes                                                int
	paths                                                  []string
	invitees, incomplete, foreign, failDrift, failReadback bool
}

func (m *rosterFixture) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v3/get-management-token", "/api/v3/list-invitation-policies", "/api/v3/create-invitation-policy", "/api/v3/get-invitation-policy", "/api/v3/update-invitation-policy", "/api/v3/delete-invitation-policies-batch":
		if r.URL.Path == "/api/v3/delete-invitation-policies-batch" {
			m.Lock()
			present := m.id != ""
			m.Unlock()
			if present {
				http.Error(w, "roster must be deleted first", 500)
				return
			}
		}
		m.policy.serve(w, r)
		return
	}
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	var body map[string]any
	if r.Method == http.MethodPost {
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad json", 500)
			return
		}
	}
	bad := func() { http.Error(w, "secret-marker", 500) }
	send := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": data}) }
	switch r.URL.Path {
	case "/api/v3/list-invitation-rosters":
		if r.Method != http.MethodPost || body["page"] != float64(1) || body["limit"] != float64(50) || body["keywords"] != invitationTestName || body["withRosterSecret"] != false {
			bad()
			return
		}
		list := []any{}
		if m.id != "" {
			list = append(list, map[string]any{"rosterId": m.id, "name": m.name})
		}
		send(map[string]any{"totalCount": len(list), "list": list})
	case "/api/v3/list-invitation-rosters-by-policy-id":
		if body["policyId"] != m.policy.id || body["page"] != float64(1) || body["limit"] != float64(50) || body["withAssignedPolicy"] != true || body["withRosterSecret"] != false {
			bad()
			return
		}
		list := []any{}
		if m.id != "" && m.policyID == m.policy.id {
			list = append(list, map[string]any{"rosterId": m.id, "name": m.name})
		}
		send(map[string]any{"totalCount": len(list), "list": list})
	case "/api/v3/create-invitation-roster":
		if m.id != "" || body["name"] != invitationTestName || len(body) != 1 {
			bad()
			return
		}
		m.id = "mock-roster-id"
		m.name = invitationTestName
		send(map[string]any{"rosterId": m.id})
	case "/api/v3/get-invitation-roster":
		if m.failReadback {
			m.failReadback = false
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		if r.Method != http.MethodGet || r.URL.Query().Get("rosterId") != m.id || m.id == "" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		if r.URL.Query().Get("withAssignedPolicy") != "true" {
			bad()
			return
		}
		name := m.name
		if m.foreign {
			name = "foreign roster"
		}
		send(map[string]any{"rosterId": m.id, "name": name, "assignedPolicy": map[string]any{"policyId": m.policyID}})
	case "/api/v3/update-invitation-roster":
		if m.failDrift {
			bad()
			return
		}
		if body["rosterId"] != m.id || m.id == "" {
			bad()
			return
		}
		if v, ok := body["name"]; ok {
			m.name = v.(string)
		}
		if v, ok := body["policyId"]; ok {
			if v != m.policy.id {
				bad()
				return
			}
			m.policyID = v.(string)
		}
		send(map[string]any{"rosterId": m.id})
	case "/api/v3/list-invitation-invitees":
		if body["rosterId"] != m.id || body["page"] != float64(1) || body["limit"] != float64(50) || m.id == "" {
			bad()
			return
		}
		if m.incomplete {
			send(map[string]any{"totalCount": 1, "list": []any{}})
			return
		}
		list := []any{}
		if m.invitees {
			list = append(list, map[string]any{"inviteeId": "foreign-invitee", "rosterId": m.id, "name": "foreign", "email": "foreign@example.invalid"})
		}
		send(map[string]any{"totalCount": len(list), "list": list})
	case "/api/v3/delete-invitation-roster":
		if body["id"] != m.id || m.id == "" || m.invitees || m.foreign {
			bad()
			return
		}
		m.id = ""
		m.policyID = ""
		m.deletes++
		send(map[string]any{"success": true})
	default:
		bad()
	}
}
func TestMockInvitationRosterTerraformTrace(t *testing.T) {
	m := &rosterFixture{}
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	defer server.Close()
	err := runInvitationRosterTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, invitationTestName)
	if err != nil {
		t.Fatal(err)
	}
	if m.id != "" || m.policy.id != "" || m.deletes != 1 || m.policy.deletes != 1 {
		t.Fatal("both owned objects not deleted exactly once")
	}
	paths := strings.Join(m.paths, "\n")
	for _, p := range []string{"create-invitation-roster", "update-invitation-roster", "list-invitation-invitees", "delete-invitation-roster"} {
		if !strings.Contains(paths, p) {
			t.Fatalf("missing %s", p)
		}
	}
	if strings.Contains(paths, "send-invitation") || strings.Contains(paths, "generate-invitation") {
		t.Fatal("sent invitation")
	}
}
func TestMockInvitationRosterCleanupRefusesUnsafe(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		foreign, invitees, incomplete bool
	}{
		{"foreign", true, false, false}, {"assigned-invitee", false, true, false}, {"incomplete-inventory", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &rosterFixture{id: "mock-roster-id", name: invitationTestName, policyID: "mock-policy-id", foreign: tc.foreign, invitees: tc.invitees, incomplete: tc.incomplete}
			m.policy.id = "mock-policy-id"
			m.policy.name = invitationTestName
			server := httptest.NewServer(http.HandlerFunc(m.serve))
			defer server.Close()
			client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
			if err != nil {
				t.Fatal("client failed")
			}
			if cleanupInvitationRoster(client, m.id, invitationTestName, m.policyID) == nil {
				t.Fatal("unsafe cleanup accepted")
			}
			if m.deletes != 0 || m.id == "" {
				t.Fatal("deleted unowned or occupied roster")
			}
		})
	}
}
func TestMockInvitationRosterRefusesExisting(t *testing.T) {
	m := &rosterFixture{id: "existing-roster", name: invitationTestName}
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	defer server.Close()
	err := runInvitationRosterTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, invitationTestName)
	if err == nil || m.id != "existing-roster" || m.deletes != 0 || m.policy.id != "" {
		t.Fatal("preflight accepted existing roster")
	}
}
func TestMockInvitationRosterFailureCleanup(t *testing.T) {
	m := &rosterFixture{failDrift: true}
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	defer server.Close()
	err := runInvitationRosterTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, invitationTestName)
	if err == nil {
		t.Fatal("failed drift accepted")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "secret-marker", "mock-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("secret in diagnostic")
		}
	}
	if m.id != "" || m.policy.id != "" || m.deletes != 1 || m.policy.deletes != 1 {
		t.Fatal("owned objects not cleaned in order")
	}
}

func TestMockInvitationRosterFailedCreateReadbackCleanup(t *testing.T) {
	m := &rosterFixture{failReadback: true}
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	defer server.Close()
	err := runInvitationRosterTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, invitationTestName)
	if err == nil || !strings.Contains(err.Error(), "phase=apply-create") {
		t.Fatal("failed create readback was not reported")
	}
	if m.id != "" || m.policy.id != "" || m.deletes != 1 || m.policy.deletes != 1 {
		t.Fatal("orphaned created objects with no Terraform state")
	}
}

func TestMockInvitationRosterAssignedInviteeBlocksFailureCleanup(t *testing.T) {
	m := &rosterFixture{invitees: true, failDrift: true}
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	defer server.Close()
	err := runInvitationRosterTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, invitationTestName)
	if err == nil || !strings.Contains(err.Error(), "phase=cleanup-incomplete") || m.id == "" || m.policy.id == "" || m.deletes != 0 || m.policy.deletes != 0 {
		t.Fatal("cleanup removed a roster with assigned invitees or its policy")
	}
}

func TestMockInvitationRosterInviteePaginationRefusesLateAssignment(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-management-token" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
			return
		}
		if r.URL.Path != "/api/v3/list-invitation-invitees" {
			http.Error(w, "unexpected endpoint", 500)
			return
		}
		var body struct {
			RosterID string `json:"rosterId"`
			Page     int    `json:"page"`
			Limit    int    `json:"limit"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.RosterID != "owned-roster" || body.Limit != 50 {
			http.Error(w, "invalid scope", 500)
			return
		}
		pages++
		list := make([]any, 0)
		if body.Page == 1 {
			for i := 0; i < 50; i++ {
				list = append(list, map[string]any{"inviteeId": fmt.Sprintf("%d", i), "rosterId": "owned-roster"})
			}
		} else if body.Page == 2 {
			list = append(list, map[string]any{"inviteeId": "last", "rosterId": "owned-roster"})
		} else {
			http.Error(w, "unexpected page", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": 51, "list": list}})
	}))
	defer server.Close()
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
	if err != nil {
		t.Fatal("client failed")
	}
	if noInvitationInvitees(client, "owned-roster") == nil || pages != 2 {
		t.Fatal("late assigned invitee or pagination missed")
	}
}

func TestDestructiveLiveInvitationRosterTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	name, err := newGroupCode()
	if err != nil {
		t.Fatal("name generation failed")
	}
	if err = runInvitationRosterTrace(t.TempDir(), env, name); err != nil {
		t.Fatal(err)
	}
}
