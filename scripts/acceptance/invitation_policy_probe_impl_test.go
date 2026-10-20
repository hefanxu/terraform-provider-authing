package acceptance

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

// The list operation is POST-shaped but read-only. No mutation path is called.
var invitationPolicyProbeLive = flag.Bool("authing-invitation-policy-probe-sandbox", false, "opt in to read-only invitation policy list diagnosis")

type invitationPolicyProbeResult struct {
	State                 string // zero, one, unknown
	Reason                string // closed diagnostic category
	HTTP, Business, Pages int
}

func invitationPolicyProbeGuard(enabled bool, env map[string]string, code string) bool {
	return enabled && code == incidentInvitationPolicyCode && sandboxCode.MatchString(code) &&
		env["AUTHING_ACCEPTANCE_CONFIRM"] == "READ_ONLY_SANDBOX" &&
		env["AUTHING_ACCESS_KEY_ID"] != "" && env["AUTHING_ACCESS_KEY_SECRET"] != ""
}

func invitationPolicyProbeFailure(err error, page int) invitationPolicyProbeResult {
	result := invitationPolicyProbeResult{State: "unknown", Reason: "transport", Pages: page}
	var httpErr *authingapi.HTTPStatusError
	if errors.As(err, &httpErr) {
		result.HTTP = httpErr.StatusCode
		result.Reason = "http-other"
		if result.HTTP >= 400 && result.HTTP < 500 {
			result.Reason = "http-4xx"
		}
		if result.HTTP >= 500 {
			result.Reason = "http-5xx"
		}
	} else if errors.Is(err, authingapi.ErrInvalidResponse) {
		result.Reason = "invalid-envelope"
	}
	return result
}

// This diagnostic intentionally does not call findInvitationPolicy: that
// fail-closed acceptance preflight must remain unchanged while we investigate.
func probeInvitationPolicyList(client *authingapi.Client, code string) invitationPolicyProbeResult {
	if code != incidentInvitationPolicyCode {
		return invitationPolicyProbeResult{State: "unknown", Reason: "guard"}
	}
	seen, total, matches := 0, -1, 0
	ids := map[string]bool{}
	for page := 1; page <= 100; page++ {
		body, err := client.SendHttpRequest("/api/v3/list-invitation-policies", http.MethodPost, map[string]any{"page": page, "limit": 50, "keywords": code})
		if err != nil {
			return invitationPolicyProbeFailure(err, page)
		}
		var envelope struct {
			StatusCode *int            `json:"statusCode"`
			Data       json.RawMessage `json:"data"`
		}
		if json.Unmarshal(body, &envelope) != nil || envelope.StatusCode == nil {
			return invitationPolicyProbeResult{State: "unknown", Reason: "invalid-envelope", Pages: page}
		}
		result := invitationPolicyProbeResult{State: "unknown", Reason: "none", Business: *envelope.StatusCode, Pages: page}
		if result.Business != 200 {
			result.Reason = "business-other"
			if result.Business >= 400 && result.Business < 500 {
				result.Reason = "business-4xx"
			}
			if result.Business >= 500 {
				result.Reason = "business-5xx"
			}
			return result
		}
		var fields map[string]json.RawMessage
		if len(envelope.Data) == 0 || json.Unmarshal(envelope.Data, &fields) != nil || fields == nil {
			result.Reason = "invalid-data"
			return result
		}
		countRaw, ok := fields["totalCount"]
		if !ok || string(countRaw) == "null" {
			result.Reason = "missing-count"
			return result
		}
		var count int
		if json.Unmarshal(countRaw, &count) != nil || count < 0 {
			result.Reason = "invalid-count"
			return result
		}
		listRaw, ok := fields["list"]
		if !ok {
			result.Reason = "missing-list"
			return result
		}
		if string(listRaw) == "null" {
			result.Reason = "null-list"
			return result
		}
		var list []json.RawMessage
		if json.Unmarshal(listRaw, &list) != nil || list == nil {
			result.Reason = "invalid-list"
			return result
		}
		if total >= 0 && count != total {
			result.Reason = "incomplete"
			return result
		}
		total = count
		if len(list) > 50 || seen+len(list) > total || len(list) == 0 && seen < total {
			result.Reason = "incomplete"
			return result
		}
		for _, raw := range list {
			var row struct {
				ID   *string `json:"policyId"`
				Name *string `json:"name"`
			}
			if json.Unmarshal(raw, &row) != nil || row.ID == nil || row.Name == nil || *row.ID == "" || *row.Name == "" {
				result.Reason = "invalid-row"
				return result
			}
			if ids[*row.ID] {
				result.Reason = "duplicate"
				return result
			}
			ids[*row.ID] = true
			if *row.Name == code {
				matches++
				if matches > 1 {
					result.Reason = "duplicate"
					return result
				}
			}
		}
		seen += len(list)
		if seen == total {
			if matches == 0 {
				result.State = "zero"
			} else {
				result.State = "one"
			}
			return result
		}
	}
	return invitationPolicyProbeResult{State: "unknown", Reason: "incomplete", Pages: 100}
}

func formatInvitationPolicyProbe(result invitationPolicyProbeResult) string {
	// Clamp to an allowlist even if a caller constructs an arbitrary result.
	state, reason := result.State, result.Reason
	if state != "zero" && state != "one" && state != "unknown" {
		state = "unknown"
	}
	switch reason {
	case "none", "guard", "transport", "http-other", "http-4xx", "http-5xx", "invalid-envelope", "invalid-data", "business-other", "business-4xx", "business-5xx", "missing-count", "invalid-count", "missing-list", "null-list", "invalid-list", "invalid-row", "duplicate", "incomplete":
	default:
		reason = "invalid-envelope"
	}
	return fmt.Sprintf("invitation-policy-probe state=%s reason=%s http_error=%d business=%d pages=%d", state, reason, result.HTTP, result.Business, result.Pages)
}

func TestReadOnlyInvitationPolicyProbe(t *testing.T) {
	if !*invitationPolicyProbeLive {
		t.Skip("requires -authing-invitation-policy-probe-sandbox and exact READ_ONLY_SANDBOX confirmation")
	}
	code := os.Getenv("AUTHING_TEST_OBJECT_CODE")
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET")}
	if !invitationPolicyProbeGuard(*invitationPolicyProbeLive, env, code) {
		t.Fatal("invitation policy probe guard rejected input (output suppressed)")
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: env["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: env["AUTHING_ACCESS_KEY_SECRET"], Host: os.Getenv("AUTHING_HOST")})
	if err != nil {
		t.Fatal("invitation policy probe client failed (output suppressed)")
	}
	t.Log(formatInvitationPolicyProbe(probeInvitationPolicyList(client, code)))
}

// Preserve the original safe phase along with a separate cleanup diagnostic.
func invitationPolicyCleanupFailure(initial error, name, id string) error {
	label := fmt.Sprintf("invitation-policy phase=cleanup-incomplete code=%s", name)
	if id != "" {
		label += " id=" + id
	}
	label += " (output suppressed)"
	if initial != nil {
		return fmt.Errorf("%v; %s", initial, label)
	}
	return errors.New(label)
}
