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

// Batch list-post is POST-shaped but is a lookup, not a mutation. No CRUD path
// is reachable from this probe. A filtered zero is NOT independent proof of
// absence; the GET status is reported separately rather than papered over.
var postRecoveryLive = flag.Bool("authing-post-recovery-sandbox", false, "opt in to exact-identity read-only post lookup")

type postProbeStage struct {
	State    string
	Reason   string
	HTTP     int // Non-2xx transport status, when the client exposes it.
	Business int
	APICode  int
	Pages    int
}
type postProbeResult struct{ GET, List postProbeStage }

func postRecoveryGuard(enabled bool, env map[string]string, code string) bool {
	return enabled && code == incidentPostCode && sandboxCode.MatchString(code) &&
		env["AUTHING_ACCEPTANCE_CONFIRM"] == "READ_ONLY_SANDBOX" &&
		env["AUTHING_ACCESS_KEY_ID"] != "" && env["AUTHING_ACCESS_KEY_SECRET"] != ""
}

func postProbeFailure(err error) postProbeStage {
	var httpErr *authingapi.HTTPStatusError
	if errors.As(err, &httpErr) {
		reason := "http-other"
		if httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 {
			reason = "http-4xx"
		}
		if httpErr.StatusCode >= 500 {
			reason = "http-5xx"
		}
		return postProbeStage{State: "unknown", Reason: reason, HTTP: httpErr.StatusCode}
	}
	if errors.Is(err, authingapi.ErrInvalidResponse) {
		return postProbeStage{State: "unknown", Reason: "invalid-envelope"}
	}
	return postProbeStage{State: "unknown", Reason: "transport"}
}

func postBusinessFailure(stage postProbeStage) postProbeStage {
	stage.State = "unknown"
	stage.Reason = "business-other"
	if stage.Business >= 400 && stage.Business < 500 {
		stage.Reason = "business-4xx"
	}
	if stage.Business >= 500 {
		stage.Reason = "business-5xx"
	}
	return stage
}

type postLookupEnvelope struct {
	StatusCode *int            `json:"statusCode"`
	APICode    *int            `json:"apiCode"`
	Data       json.RawMessage `json:"data"`
}

func postEnvelope(body []byte) (postLookupEnvelope, bool) {
	var e postLookupEnvelope
	if json.Unmarshal(body, &e) != nil || e.StatusCode == nil {
		return e, false
	}
	return e, true
}
func postNumbers(e postLookupEnvelope) postProbeStage {
	s := postProbeStage{Reason: "none", Business: *e.StatusCode}
	if e.APICode != nil {
		s.APICode = *e.APICode
	}
	return s
}
func postIdentity(code, name, description string) bool {
	return code == incidentPostCode && (name == code || name == code+"-updated") && description == "hermesacc ownership "+code
}
func probePostGet(client *authingapi.Client, code string) postProbeStage {
	body, err := client.SendHttpRequest("/api/v3/get-post", http.MethodGet, map[string]string{"code": code})
	if err != nil {
		return postProbeFailure(err)
	}
	e, valid := postEnvelope(body)
	if !valid {
		return postProbeStage{State: "unknown", Reason: "invalid-envelope"}
	}
	stage := postNumbers(e)
	if stage.Business == 404 {
		stage.State = "not-found"
		return stage
	}
	if stage.Business != 200 {
		return postBusinessFailure(stage)
	}
	var data struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if len(e.Data) == 0 || string(e.Data) == "null" || json.Unmarshal(e.Data, &data) != nil || data.Code == "" || data.Name == "" {
		stage.State = "unknown"
		stage.Reason = "invalid-envelope"
		return stage
	}
	stage.State = "foreign"
	if postIdentity(data.Code, data.Name, data.Description) {
		stage.State = "owned"
	}
	return stage
}
func probePostList(client *authingapi.Client, code string) postProbeStage {
	seen, matches, total := 0, 0, -1
	var matchOwned bool
	for page := 1; page <= 100; page++ {
		// OpenAPI ListPostBatchDto: exact postCodes plus 1-based page and max limit 50.
		body, err := client.SendHttpRequest("/api/v3/list-post", http.MethodPost, map[string]any{"postCodes": []string{code}, "page": page, "limit": 50})
		if err != nil {
			s := postProbeFailure(err)
			s.Pages = page
			return s
		}
		e, valid := postEnvelope(body)
		if !valid {
			return postProbeStage{State: "unknown", Reason: "invalid-envelope", Pages: page}
		}
		stage := postNumbers(e)
		stage.Pages = page
		if stage.Business != 200 {
			return postBusinessFailure(stage)
		}
		var data struct {
			TotalCount *int `json:"totalCount"`
			List       []struct {
				Code        string `json:"code"`
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"list"`
		}
		if len(e.Data) == 0 || json.Unmarshal(e.Data, &data) != nil || data.TotalCount == nil || data.List == nil || *data.TotalCount < 0 || len(data.List) > 50 {
			stage.State = "unknown"
			stage.Reason = "invalid-envelope"
			return stage
		}
		if total >= 0 && total != *data.TotalCount {
			stage.State = "unknown"
			stage.Reason = "incomplete"
			return stage
		}
		total = *data.TotalCount
		if seen+len(data.List) > total || (len(data.List) == 0 && seen < total) {
			stage.State = "unknown"
			stage.Reason = "incomplete"
			return stage
		}
		for _, entry := range data.List {
			if entry.Code == "" || entry.Code != code {
				stage.State = "unknown"
				stage.Reason = "scope-mismatch"
				return stage
			}
			matches++
			if matches > 1 {
				stage.State = "unknown"
				stage.Reason = "duplicate"
				return stage
			}
			matchOwned = postIdentity(entry.Code, entry.Name, entry.Description)
		}
		seen += len(data.List)
		if seen == total {
			if matches == 0 {
				stage.State = "zero"
			} else if matchOwned {
				stage.State = "owned"
			} else {
				stage.State = "foreign"
			}
			return stage
		}
	}
	return postProbeStage{State: "unknown", Reason: "incomplete", Pages: 100}
}
func probePostRecovery(client *authingapi.Client, code string) postProbeResult {
	if code != incidentPostCode {
		return postProbeResult{GET: postProbeStage{State: "unknown", Reason: "guard"}, List: postProbeStage{State: "unknown", Reason: "guard"}}
	}
	return postProbeResult{GET: probePostGet(client, code), List: probePostList(client, code)}
}
func formatPostRecovery(result postProbeResult) string {
	// Only closed labels and parsed integers: never include raw bodies, messages,
	// descriptions, response errors, tokens, or arbitrary service-provided text.
	return fmt.Sprintf("post-recovery get=%s get_reason=%s get_http_error=%d get_business=%d get_api=%d list=%s list_reason=%s list_http_error=%d list_business=%d list_api=%d list_pages=%d",
		result.GET.State, result.GET.Reason, result.GET.HTTP, result.GET.Business, result.GET.APICode,
		result.List.State, result.List.Reason, result.List.HTTP, result.List.Business, result.List.APICode, result.List.Pages)
}
func TestReadOnlyPostRecovery(t *testing.T) {
	if !*postRecoveryLive {
		t.Skip("requires -authing-post-recovery-sandbox and exact READ_ONLY_SANDBOX confirmation")
	}
	code := os.Getenv("AUTHING_TEST_OBJECT_CODE")
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET")}
	if !postRecoveryGuard(*postRecoveryLive, env, code) {
		t.Fatal("post recovery guard rejected input (output suppressed)")
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: env["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: env["AUTHING_ACCESS_KEY_SECRET"], Host: os.Getenv("AUTHING_HOST")})
	if err != nil {
		t.Fatal("post recovery client setup failed (output suppressed)")
	}
	t.Log(formatPostRecovery(probePostRecovery(client, code)))
}
