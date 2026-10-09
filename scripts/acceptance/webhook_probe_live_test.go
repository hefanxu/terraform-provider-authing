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

// This manually dispatched probe performs only GET /api/v3/list-webhooks
// (besides authentication); its controlled output never contains response text.
var webhookShapeLive = flag.Bool("authing-webhook-shape-sandbox", false, "opt in to read-only webhook list shape probe")

type webhookShapeStage struct {
	State, Reason, Shape           string
	HTTP, Business, APICode, Pages int
}

func webhookProbeGuard(enabled bool, env map[string]string, code string) bool {
	return enabled && code == incidentWebhookCode && sandboxCode.MatchString(code) && env["AUTHING_ACCEPTANCE_CONFIRM"] == "READ_ONLY_SANDBOX" && env["AUTHING_ACCESS_KEY_ID"] != "" && env["AUTHING_ACCESS_KEY_SECRET"] != ""
}
func webhookShapeFailure(err error, page int) webhookShapeStage {
	stage := webhookShapeStage{State: "unknown", Reason: "transport", Shape: "unavailable", Pages: page}
	var httpErr *authingapi.HTTPStatusError
	if errors.As(err, &httpErr) {
		stage.HTTP = httpErr.StatusCode
		stage.Reason = "http-other"
		if stage.HTTP >= 400 && stage.HTTP < 500 {
			stage.Reason = "http-4xx"
		} else if stage.HTTP >= 500 {
			stage.Reason = "http-5xx"
		}
	} else if errors.Is(err, authingapi.ErrInvalidResponse) {
		stage.Reason = "invalid-envelope"
	}
	return stage
}
func probeWebhookListShape(client *authingapi.Client, code string) webhookShapeStage {
	if code != incidentWebhookCode {
		return webhookShapeStage{State: "unknown", Reason: "guard", Shape: "unavailable"}
	}
	seen, total, matches := 0, -1, 0
	names := map[string]bool{}
	for page := 1; page <= 100; page++ {
		body, err := client.SendHttpRequest("/api/v3/list-webhooks", http.MethodGet, map[string]any{"page": page, "limit": 50})
		if err != nil {
			return webhookShapeFailure(err, page)
		}
		stage := webhookShapeStage{State: "unknown", Reason: "invalid-envelope", Shape: "unavailable", Pages: page}
		var outer struct {
			StatusCode *int            `json:"statusCode"`
			APICode    *int            `json:"apiCode"`
			Data       json.RawMessage `json:"data"`
		}
		if json.Unmarshal(body, &outer) != nil || outer.StatusCode == nil {
			return stage
		}
		stage.Business = *outer.StatusCode
		if outer.APICode != nil {
			stage.APICode = *outer.APICode
		}
		if stage.Business != 200 {
			stage.Reason = "business-other"
			if stage.Business >= 400 && stage.Business < 500 {
				stage.Reason = "business-4xx"
			} else if stage.Business >= 500 {
				stage.Reason = "business-5xx"
			}
			return stage
		}
		var data map[string]json.RawMessage
		if len(outer.Data) == 0 || json.Unmarshal(outer.Data, &data) != nil || data == nil {
			stage.Shape = "invalid-data"
			return stage
		}
		rawCount, ok := data["totalCount"]
		if !ok || string(rawCount) == "null" {
			stage.Shape = "missing-count"
			return stage
		}
		var count int
		if json.Unmarshal(rawCount, &count) != nil || count < 0 {
			stage.Shape = "invalid-data"
			return stage
		}
		rawList, ok := data["list"]
		if !ok {
			stage.Shape = "missing-list"
			return stage
		}
		if string(rawList) == "null" {
			stage.Shape = "null-list"
			return stage
		}
		var entries []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(rawList, &entries) != nil || entries == nil {
			stage.Shape = "invalid-data"
			return stage
		}
		if total >= 0 && count != total || len(entries) > 50 || seen+len(entries) > count || len(entries) == 0 && seen < count {
			stage.Reason = "incomplete"
			stage.Shape = "incomplete"
			return stage
		}
		total = count
		for _, e := range entries {
			if e.Name != "" {
				if names[e.Name] {
					stage.Reason = "duplicate"
					stage.Shape = "duplicate"
					return stage
				}
				names[e.Name] = true
			}
			if e.Name == code {
				matches++
			}
		}
		seen += len(entries)
		if seen == total {
			stage.Reason = "none"
			stage.Shape = "complete"
			stage.State = "no-match"
			if total == 0 {
				stage.State = "zero"
			} else if matches == 1 {
				stage.State = "match"
			}
			return stage
		}
	}
	return webhookShapeStage{State: "unknown", Reason: "incomplete", Shape: "incomplete", Pages: 100}
}
func formatWebhookListShape(s webhookShapeStage) string {
	return fmt.Sprintf("webhook-shape state=%s reason=%s shape=%s http_error=%d business=%d api=%d pages=%d", s.State, s.Reason, s.Shape, s.HTTP, s.Business, s.APICode, s.Pages)
}
func TestReadOnlyWebhookListShape(t *testing.T) {
	if !*webhookShapeLive {
		t.Skip("requires -authing-webhook-shape-sandbox and exact READ_ONLY_SANDBOX confirmation")
	}
	code := os.Getenv("AUTHING_TEST_OBJECT_CODE")
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET")}
	if !webhookProbeGuard(*webhookShapeLive, env, code) {
		t.Fatal("webhook shape probe guard rejected input (output suppressed)")
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: env["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: env["AUTHING_ACCESS_KEY_SECRET"], Host: os.Getenv("AUTHING_HOST")})
	if err != nil {
		t.Fatal("webhook shape client setup failed (output suppressed)")
	}
	t.Log(formatWebhookListShape(probeWebhookListShape(client, code)))
}
