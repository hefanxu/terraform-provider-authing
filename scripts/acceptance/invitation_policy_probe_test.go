package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

const incidentInvitationPolicyCode = "hermesacc-ece1527c4751401c"

func TestInvitationPolicyProbeGuard(t *testing.T) {
	valid := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}
	if !invitationPolicyProbeGuard(true, valid, incidentInvitationPolicyCode) {
		t.Fatal("valid probe refused")
	}
	for _, tc := range []struct {
		name    string
		enabled bool
		env     map[string]string
		code    string
	}{
		{"disabled", false, valid, incidentInvitationPolicyCode},
		{"different", true, valid, "hermesacc-aaaaaaaaaaaaaaaa"},
		{"suffix", true, valid, incidentInvitationPolicyCode + "x"},
		{"destructive", true, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "DESTRUCTIVE_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}, incidentInvitationPolicyCode},
		{"missing-key", true, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_SECRET": "secret"}, incidentInvitationPolicyCode},
		{"missing-secret", true, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key"}, incidentInvitationPolicyCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if invitationPolicyProbeGuard(tc.enabled, tc.env, tc.code) {
				t.Fatal("unsafe probe accepted")
			}
		})
	}
}

func TestInvitationPolicyProbeList(t *testing.T) {
	const secret = "NEVER-PRINT-SECRET-MARKER"
	row := `{"policyId":"id-one","name":"` + incidentInvitationPolicyCode + `","internal":"` + secret + `"}`
	filler := `{"policyId":"id-two","name":"other"}`
	envelope := func(count int, rows string) string {
		return fmt.Sprintf(`{"statusCode":200,"data":{"totalCount":%d,"list":[%s]}}`, count, rows)
	}
	for _, tc := range []struct {
		name          string
		http          int
		pages         []string
		state, reason string
		business      int
		calls         int
	}{
		{"zero", 200, []string{envelope(0, "")}, "zero", "none", 200, 1},
		{"exact-second-page", 200, []string{envelope(2, filler), envelope(2, row)}, "one", "none", 200, 2},
		{"http-403", 403, []string{`{"statusCode":403,"message":"` + secret + `"}`}, "unknown", "http-4xx", 0, 1},
		{"http-500", 500, []string{`{"statusCode":500,"message":"` + secret + `"}`}, "unknown", "http-5xx", 0, 1},
		{"business-403", 200, []string{`{"statusCode":403,"message":"` + secret + `"}`}, "unknown", "business-4xx", 403, 1},
		{"business-500", 200, []string{`{"statusCode":500,"message":"` + secret + `"}`}, "unknown", "business-5xx", 500, 1},
		{"malformed", 200, []string{`{"statusCode":200,"data":{}}`}, "unknown", "missing-count", 200, 1},
		{"null-list", 200, []string{`{"statusCode":200,"data":{"totalCount":0,"list":null}}`}, "unknown", "null-list", 200, 1},
		{"invalid-count", 200, []string{`{"statusCode":200,"data":{"totalCount":"0","list":[]}}`}, "unknown", "invalid-count", 200, 1},
		{"invalid-list", 200, []string{`{"statusCode":200,"data":{"totalCount":0,"list":{}}}`}, "unknown", "invalid-list", 200, 1},
		{"null-data", 200, []string{`{"statusCode":200,"data":null}`}, "unknown", "invalid-data", 200, 1},
		{"missing-list", 200, []string{`{"statusCode":200,"data":{"totalCount":0}}`}, "unknown", "missing-list", 200, 1},
		{"invalid-envelope", 200, []string{`{"statusCode":"oops","message":"` + secret + `"}`}, "unknown", "invalid-envelope", 0, 1},
		{"duplicate-name", 200, []string{envelope(2, row+","+row)}, "unknown", "duplicate", 200, 1},
		{"duplicate-id", 200, []string{envelope(2, filler), envelope(2, filler)}, "unknown", "duplicate", 200, 2},
		{"incomplete", 200, []string{envelope(2, "")}, "unknown", "incomplete", 200, 1},
		{"changed-count", 200, []string{envelope(2, filler), envelope(3, row)}, "unknown", "incomplete", 200, 2},
		{"over-count", 200, []string{envelope(1, filler+","+row)}, "unknown", "incomplete", 200, 1},
		{"malformed-row", 200, []string{envelope(1, `{"policyId":"id-one","name":null}`)}, "unknown", "invalid-row", 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-management-token" {
					fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
					return
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/v3/list-invitation-policies" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(405)
					return
				}
				var request map[string]any
				if json.NewDecoder(r.Body).Decode(&request) != nil || len(request) != 3 || request["page"] != float64(calls+1) || request["limit"] != float64(50) || request["keywords"] != incidentInvitationPolicyCode {
					t.Error("incorrect list scope or pagination")
				}
				index := calls
				calls++
				if index >= len(tc.pages) {
					t.Error("extra page")
					w.WriteHeader(500)
					return
				}
				w.WriteHeader(tc.http)
				fmt.Fprint(w, tc.pages[index])
			}))
			defer server.Close()
			client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
			if err != nil {
				t.Fatal("client setup failed")
			}
			got := probeInvitationPolicyList(client, incidentInvitationPolicyCode)
			if got.State != tc.state || got.Reason != tc.reason || got.Business != tc.business || got.Pages != tc.calls || calls != tc.calls {
				t.Errorf("probe=%+v calls=%d, want state=%s reason=%s business=%d calls=%d", got, calls, tc.state, tc.reason, tc.business, tc.calls)
			}
			output := formatInvitationPolicyProbe(got)
			for _, forbidden := range []string{secret, "mock-token", "id-one", "id-two", incidentInvitationPolicyCode, "message", "internal"} {
				if strings.Contains(output, forbidden) {
					t.Error("probe output disclosed policy or response")
				}
			}
		})
	}
}

func TestInvitationPolicyCleanupRetainsInitialFailure(t *testing.T) {
	initial := fmt.Errorf("invitation-policy phase=apply-create code=%s (output suppressed)", invitationTestName)
	got := invitationPolicyCleanupFailure(initial, invitationTestName, "mock-policy-id").Error()
	if !strings.Contains(got, "phase=apply-create") || !strings.Contains(got, "cleanup-incomplete") || !strings.Contains(got, "id=mock-policy-id") {
		t.Fatal("initial failure or cleanup context lost")
	}
}

func TestInvitationPolicyTraceRetainsFailureWhenCleanupBlocked(t *testing.T) {
	m := &mockInvitationPolicy{failDrift: true, invalidRoster: true}
	_, server := invitationMockClient(t, m)
	defer server.Close()
	err := runInvitationPolicyTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, invitationTestName)
	if err == nil || !strings.Contains(err.Error(), "phase=remote-drift") || !strings.Contains(err.Error(), "cleanup-incomplete") || m.deletes != 0 {
		t.Fatal("original failure, blocked cleanup, or mutation safety lost")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "secret-marker", "mock-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("secret leaked")
		}
	}
}
