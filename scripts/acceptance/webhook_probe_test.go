package acceptance

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

const incidentWebhookCode = "hermesacc-630f7fb8a43f0400"

func TestWebhookProbeGuard(t *testing.T) {
	valid := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}
	if !webhookProbeGuard(true, valid, incidentWebhookCode) {
		t.Fatal("valid read-only request rejected")
	}
	for _, tc := range []struct {
		name    string
		enabled bool
		env     map[string]string
		code    string
	}{
		{"disabled", false, valid, incidentWebhookCode}, {"wrong-code", true, valid, webhookTestName}, {"suffix", true, valid, incidentWebhookCode + "x"},
		{"wrong-confirmation", true, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "DESTRUCTIVE_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}, incidentWebhookCode},
		{"no-key", true, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_SECRET": "secret"}, incidentWebhookCode},
		{"no-secret", true, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key"}, incidentWebhookCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if webhookProbeGuard(tc.enabled, tc.env, tc.code) {
				t.Fatal("unsafe probe accepted")
			}
		})
	}
}

func TestWebhookListShapeProbe(t *testing.T) {
	const marker = "SECRET-NEVER-PRINT"
	entry := `{"name":"` + incidentWebhookCode + `","webhookId":"` + marker + `","url":"` + marker + `","events":["` + marker + `"]}`
	for _, tc := range []struct {
		name                     string
		httpCode                 int
		pages                    []string
		state, reason, shape     string
		business, api, pagesRead int
	}{
		{"empty", 200, []string{`{"statusCode":200,"data":{"totalCount":0,"list":[]}}`}, "zero", "none", "complete", 200, 0, 1},
		{"unique", 200, []string{`{"statusCode":200,"data":{"totalCount":1,"list":[` + entry + `]}}`}, "match", "none", "complete", 200, 0, 1},
		{"other-name", 200, []string{`{"statusCode":200,"data":{"totalCount":1,"list":[{"name":"other"}]}}`}, "no-match", "none", "complete", 200, 0, 1},
		{"second-page", 200, []string{`{"statusCode":200,"data":{"totalCount":2,"list":[{"name":"other"}]}}`, `{"statusCode":200,"data":{"totalCount":2,"list":[` + entry + `]}}`}, "match", "none", "complete", 200, 0, 2},
		{"http-403", 403, []string{`{"statusCode":403,"message":"` + marker + `"}`}, "unknown", "http-4xx", "unavailable", 0, 0, 1},
		{"http-500", 500, []string{`{"statusCode":500,"message":"` + marker + `"}`}, "unknown", "http-5xx", "unavailable", 0, 0, 1},
		{"business-403", 200, []string{`{"statusCode":403,"apiCode":1234,"message":"` + marker + `"}`}, "unknown", "business-4xx", "unavailable", 403, 1234, 1},
		{"business-500", 200, []string{`{"statusCode":500,"apiCode":5555,"message":"` + marker + `"}`}, "unknown", "business-5xx", "unavailable", 500, 5555, 1},
		{"malformed", 200, []string{`{"statusCode":200,"data":"` + marker + `"}`}, "unknown", "invalid-envelope", "invalid-data", 200, 0, 1},
		{"missing-count", 200, []string{`{"statusCode":200,"data":{"list":[]}}`}, "unknown", "invalid-envelope", "missing-count", 200, 0, 1},
		{"missing-list", 200, []string{`{"statusCode":200,"data":{"totalCount":0}}`}, "unknown", "invalid-envelope", "missing-list", 200, 0, 1},
		{"null-list", 200, []string{`{"statusCode":200,"data":{"totalCount":0,"list":null}}`}, "unknown", "invalid-envelope", "null-list", 200, 0, 1},
		{"short-page", 200, []string{`{"statusCode":200,"data":{"totalCount":2,"list":[]}}`}, "unknown", "incomplete", "incomplete", 200, 0, 1},
		{"changed-total", 200, []string{`{"statusCode":200,"data":{"totalCount":2,"list":[{"name":"other"}]}}`, `{"statusCode":200,"data":{"totalCount":1,"list":[]}}`}, "unknown", "incomplete", "incomplete", 200, 0, 2},
		{"duplicate-name", 200, []string{`{"statusCode":200,"data":{"totalCount":2,"list":[` + entry + `,` + entry + `]}}`}, "unknown", "duplicate", "duplicate", 200, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-management-token" {
					if r.Method != http.MethodPost {
						t.Error("token method")
					}
					fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
					return
				}
				if r.Method != http.MethodGet || r.URL.Path != "/api/v3/list-webhooks" || r.URL.Query().Get("page") != fmt.Sprint(requests+1) || r.URL.Query().Get("limit") != "100" {
					t.Errorf("unexpected request method/path/page")
					w.WriteHeader(405)
					return
				}
				index := requests
				requests++
				if index >= len(tc.pages) {
					t.Error("extra page")
					w.WriteHeader(500)
					return
				}
				w.WriteHeader(tc.httpCode)
				fmt.Fprint(w, tc.pages[index])
			}))
			defer server.Close()
			client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			got := probeWebhookListShape(client, incidentWebhookCode)
			if got.State != tc.state || got.Reason != tc.reason || got.Shape != tc.shape || got.Business != tc.business || got.APICode != tc.api || got.Pages != tc.pagesRead || requests != tc.pagesRead {
				t.Errorf("unexpected closed diagnostic: %+v requests=%d", got, requests)
			}
			if tc.httpCode != 200 && got.HTTP != tc.httpCode {
				t.Errorf("HTTP status lost: %d", got.HTTP)
			}
			output := formatWebhookListShape(got)
			for _, s := range []string{marker, "mock-token", "webhookId", "url", "events", "message", incidentWebhookCode} {
				if strings.Contains(output, s) {
					t.Fatal("unsafe output")
				}
			}
		})
	}
}
