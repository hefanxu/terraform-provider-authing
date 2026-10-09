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

type inviteeFixture struct {
	sync.Mutex
	roster                                                           rosterFixture
	invitee                                                          tracedInvitee
	paths, allPaths                                                  []string
	deletes                                                          int
	failEdit, incomplete, foreign, failReadback, incompleteAfterEdit bool
}

func (m *inviteeFixture) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	m.allPaths = append(m.allPaths, r.URL.Path)
	m.Unlock()
	switch r.URL.Path {
	case "/api/v3/list-invitation-invitees", "/api/v3/create-invitation-invitee", "/api/v3/edit-invitation-invitee", "/api/v3/batch-delete-invitation-invitees":
	default:
		m.roster.serve(w, r)
		return
	}
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	var body map[string]any
	if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil {
		http.Error(w, "bad request", 500)
		return
	}
	send := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": data}) }
	bad := func() { http.Error(w, "secret-marker", 500) }
	if body["rosterId"] != m.roster.id || m.roster.id == "" {
		bad()
		return
	}
	switch r.URL.Path {
	case "/api/v3/list-invitation-invitees":
		if body["limit"] != float64(50) || body["page"] != float64(1) {
			bad()
			return
		}
		if m.incomplete {
			send(map[string]any{"totalCount": 1, "list": []any{}})
			return
		}
		rows := []any{}
		if m.failReadback && m.invitee.ID != "" {
			m.failReadback = false
			send(map[string]any{"totalCount": 0, "list": []any{}})
			return
		}
		if m.invitee.ID != "" {
			rows = append(rows, map[string]any{"inviteeId": m.invitee.ID, "rosterId": m.roster.id, "name": m.invitee.Name, "email": m.invitee.Email, "phone": m.invitee.Phone})
		}
		if m.foreign {
			rows = append(rows, map[string]any{"inviteeId": "foreign-id", "rosterId": m.roster.id, "name": "foreign", "email": "foreign@example.invalid"})
		}
		send(map[string]any{"totalCount": len(rows), "list": rows})
	case "/api/v3/create-invitation-invitee":
		if m.invitee.ID != "" || body["name"] != invitationTestName || body["email"] != invitationTestName+"@example.invalid" || len(body) != 3 {
			bad()
			return
		}
		m.invitee = tracedInvitee{"mock-invitee-id", m.roster.id, invitationTestName, invitationTestName + "@example.invalid", ""}
		send(map[string]any{"inviteeId": m.invitee.ID, "rosterId": m.roster.id})
	case "/api/v3/edit-invitation-invitee":
		if m.failEdit {
			if m.incompleteAfterEdit {
				m.incomplete = true
			}
			bad()
			return
		}
		if body["inviteeId"] != m.invitee.ID || m.invitee.ID == "" || body["email"] != m.invitee.Email || body["phone"] != "" || body["name"] != invitationTestName && body["name"] != invitationTestName+"-drift" {
			bad()
			return
		}
		m.invitee.Name = body["name"].(string)
		send(map[string]any{"success": true})
	case "/api/v3/batch-delete-invitation-invitees":
		ids, ok := body["inviteeIds"].([]any)
		if !ok || len(ids) != 1 || ids[0] != m.invitee.ID || m.invitee.ID == "" || m.foreign || m.incomplete {
			bad()
			return
		}
		m.invitee = tracedInvitee{}
		m.deletes++
		send(map[string]any{"success": true})
	}
}
func inviteeMockClient(t *testing.T, m *inviteeFixture) (*authingapi.Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
	if err != nil {
		t.Fatal("client creation failed")
	}
	return client, server
}
func TestMockInvitationInviteeTerraformTrace(t *testing.T) {
	m := &inviteeFixture{}
	_, server := inviteeMockClient(t, m)
	defer server.Close()
	err := runInvitationInviteeTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, invitationTestName)
	if err != nil {
		t.Fatal(err)
	}
	if m.invitee.ID != "" || m.roster.id != "" || m.roster.policy.id != "" || m.deletes != 1 || m.roster.deletes != 1 || m.roster.policy.deletes != 1 {
		t.Fatal("owned objects not deleted exactly once")
	}
	paths := strings.Join(m.paths, "\n")
	for _, path := range []string{"create-invitation-invitee", "edit-invitation-invitee", "batch-delete-invitation-invitees"} {
		if !strings.Contains(paths, path) {
			t.Fatalf("missing %s", path)
		}
	}
	all := strings.Join(m.allPaths, "\n")
	if strings.Contains(all, "send-invitation") || strings.Contains(all, "generate-invitation") {
		t.Fatal("sending endpoint called")
	}
	a := strings.Index(all, "/api/v3/batch-delete-invitation-invitees")
	b := strings.Index(all, "/api/v3/delete-invitation-roster")
	c := strings.Index(all, "/api/v3/delete-invitation-policies-batch")
	if a < 0 || b <= a || c <= b {
		t.Fatal("destroy did not follow invitee -> roster -> policy")
	}
}
func TestMockInvitationInviteeCleanupRefusesForeignAndIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name                string
		foreign, incomplete bool
	}{{"foreign", true, false}, {"incomplete", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			m := &inviteeFixture{foreign: tc.foreign, incomplete: tc.incomplete}
			m.roster.id = "mock-roster-id"
			m.roster.name = invitationTestName
			m.roster.policyID = "mock-policy-id"
			m.roster.policy.id = "mock-policy-id"
			m.roster.policy.name = invitationTestName
			m.invitee = tracedInvitee{"mock-invitee-id", m.roster.id, invitationTestName, invitationTestName + "@example.invalid", ""}
			client, server := inviteeMockClient(t, m)
			defer server.Close()
			if cleanupInvitationInvitee(client, m.roster.id, m.invitee.ID, invitationTestName, m.invitee.Email) == nil || m.deletes != 0 {
				t.Fatal("unsafe invitee cleanup accepted")
			}
			if cleanupInvitationRoster(client, m.roster.id, invitationTestName, m.roster.policyID) == nil || m.roster.deletes != 0 {
				t.Fatal("unsafe roster cleanup accepted")
			}
		})
	}
}
func TestMockInvitationInviteeFailedApplyCleanup(t *testing.T) {
	m := &inviteeFixture{failEdit: true}
	_, server := inviteeMockClient(t, m)
	defer server.Close()
	err := runInvitationInviteeTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, invitationTestName)
	if err == nil || !strings.Contains(err.Error(), "phase=apply-name-update") {
		t.Fatalf("first phase lost: %v", err)
	}
	for _, marker := range []string{"key-marker", "credential-marker", "secret-marker"} {
		if strings.Contains(err.Error(), marker) {
			t.Fatal("sensitive output leaked")
		}
	}
	if m.invitee.ID != "" || m.roster.id != "" || m.roster.policy.id != "" {
		t.Fatal("failed apply left owned objects")
	}
}
func TestMockInvitationInviteeFailedReadbackCleanup(t *testing.T) {
	m := &inviteeFixture{failReadback: true}
	_, server := inviteeMockClient(t, m)
	defer server.Close()
	err := runInvitationInviteeTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, invitationTestName)
	if err == nil || !strings.Contains(err.Error(), "phase=apply-create") || m.deletes != 1 || m.roster.deletes != 1 || m.roster.policy.deletes != 1 {
		t.Fatalf("failed readback did not safely recover orphan: %v", err)
	}
}
func TestMockInvitationInviteeIncompleteFailureCleanup(t *testing.T) {
	m := &inviteeFixture{failEdit: true, incompleteAfterEdit: true}
	_, server := inviteeMockClient(t, m)
	defer server.Close()
	err := runInvitationInviteeTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, invitationTestName)
	if err == nil || !strings.Contains(err.Error(), "phase=apply-name-update") || !strings.Contains(err.Error(), "cleanup=incomplete") || m.deletes != 0 || m.roster.deletes != 0 || m.roster.policy.deletes != 0 {
		t.Fatalf("incomplete inventory permitted deletion: %v", err)
	}
}

func TestMockInvitationInviteePaginationFailsClosed(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-management-token" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
			return
		}
		if r.URL.Path != "/api/v3/list-invitation-invitees" {
			http.Error(w, "unexpected", 500)
			return
		}
		var body struct {
			Roster string `json:"rosterId"`
			Page   int    `json:"page"`
			Limit  int    `json:"limit"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Roster != "owned-roster" || body.Limit != 50 {
			http.Error(w, "scope", 500)
			return
		}
		pages++
		list := []any{}
		if body.Page == 1 {
			for i := 0; i < 50; i++ {
				list = append(list, map[string]any{"inviteeId": fmt.Sprint(i), "rosterId": "owned-roster", "name": "foreign", "email": "foreign@example.invalid"})
			}
		} else if body.Page == 2 {
			list = append(list, map[string]any{"inviteeId": "owned", "rosterId": "owned-roster", "name": invitationTestName, "email": invitationTestName + "@example.invalid"})
		} else {
			http.Error(w, "page", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": 51, "list": list}})
	}))
	defer server.Close()
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := invitationInvitees(client, "owned-roster")
	if err != nil || len(rows) != 51 || pages != 2 {
		t.Fatalf("pagination incomplete: pages=%d err=%v", pages, err)
	}
	if cleanupInvitationInvitee(client, "owned-roster", "owned", invitationTestName, invitationTestName+"@example.invalid") == nil {
		t.Fatal("foreign PII deletion allowed")
	}
}
func TestDestructiveLiveInvitationInviteeTrace(t *testing.T) {
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
	if err := runInvitationInviteeTrace(t.TempDir(), env, name); err != nil {
		t.Fatal(err)
	}
}
