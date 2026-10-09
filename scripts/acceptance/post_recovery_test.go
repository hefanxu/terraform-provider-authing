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

const incidentPostCode = "hermesacc-52d2839776773ef4"

func TestPostRecoveryProbeGuardMock(t *testing.T) {
	valid := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}
	if !postRecoveryGuard(true, valid, incidentPostCode) {
		t.Fatal("valid guarded probe rejected")
	}
	for _, tc := range []struct {
		name    string
		enabled bool
		env     map[string]string
		code    string
	}{
		{"disabled", false, valid, incidentPostCode}, {"wrong-code", true, valid, "hermesacc-aaaaaaaaaaaaaaaa"}, {"suffix", true, valid, incidentPostCode + "x"},
		{"no-code", true, valid, ""}, {"no-confirm", true, map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}, incidentPostCode},
		{"destructive", true, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "DESTRUCTIVE_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}, incidentPostCode},
		{"no-key", true, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_SECRET": "secret"}, incidentPostCode},
		{"no-secret", true, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key"}, incidentPostCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if postRecoveryGuard(tc.enabled, tc.env, tc.code) {
				t.Fatal("unsafe probe accepted")
			}
		})
	}
}

func TestPostRecoveryProbeMock(t *testing.T) {
	const secret = "NEVER-PRINT-SECRET-MARKER"
	for _, tc := range []struct {
		name                          string
		getHTTP                       int
		getBody                       string
		pages                         []string
		listHTTP                      int
		wantGET, wantList, wantReason string
	}{
		{"missing", 404, `{"statusCode":404,"apiCode":4041,"message":"` + secret + `"}`, []string{`{"statusCode":200,"data":{"totalCount":0,"list":[]}}`}, 200, "not-found", "zero", "none"},
		{"owned", 200, `{"statusCode":200,"data":{"code":"` + incidentPostCode + `","name":"` + incidentPostCode + `","description":"hermesacc ownership ` + incidentPostCode + `","private":"` + secret + `"}}`, []string{`{"statusCode":200,"data":{"totalCount":1,"list":[{"code":"` + incidentPostCode + `","name":"` + incidentPostCode + `","description":"hermesacc ownership ` + incidentPostCode + `"}]}}`}, 200, "owned", "owned", "none"},
		{"foreign", 200, `{"statusCode":200,"data":{"code":"` + incidentPostCode + `","name":"other","description":"` + secret + `"}}`, []string{`{"statusCode":200,"data":{"totalCount":1,"list":[{"code":"` + incidentPostCode + `","name":"other","description":"` + secret + `"}]}}`}, 200, "foreign", "foreign", "none"},
		{"scope-mismatch", 404, `{"statusCode":404}`, []string{`{"statusCode":200,"data":{"totalCount":2,"list":[{"code":"other"}]}}`}, 200, "not-found", "unknown", "scope-mismatch"},
		{"business-403", 200, `{"statusCode":403,"apiCode":1001,"message":"` + secret + `"}`, []string{`{"statusCode":200,"data":{"totalCount":0,"list":[]}}`}, 200, "unknown", "zero", "business-4xx"},
		{"business-500", 200, `{"statusCode":500,"message":"` + secret + `"}`, []string{`{"statusCode":200,"data":{"totalCount":0,"list":[]}}`}, 200, "unknown", "zero", "business-5xx"},
		{"list-business-500", 404, `{"statusCode":404}`, []string{`{"statusCode":500,"message":"` + secret + `"}`}, 200, "not-found", "unknown", "business-5xx"},
		{"http-403", 403, `{"statusCode":403,"message":"` + secret + `"}`, []string{`{"statusCode":200,"data":{"totalCount":0,"list":[]}}`}, 200, "unknown", "zero", "http-4xx"},
		{"http-500", 500, `{"statusCode":500,"message":"` + secret + `"}`, []string{`{"statusCode":200,"data":{"totalCount":0,"list":[]}}`}, 200, "unknown", "zero", "http-5xx"},
		{"malformed", 200, `{"statusCode":200,"data":{}}`, []string{`{"statusCode":200,"data":{"totalCount":0,"list":[]}}`}, 200, "unknown", "zero", "invalid-envelope"},
		{"list-malformed", 404, `{"statusCode":404}`, []string{`{"statusCode":200,"data":{}}`}, 200, "not-found", "unknown", "invalid-envelope"},
		{"list-403", 404, `{"statusCode":404}`, []string{`{"statusCode":403,"message":"` + secret + `"}`}, 200, "not-found", "unknown", "business-4xx"},
		{"list-http-500", 404, `{"statusCode":404}`, []string{`{"statusCode":500,"message":"` + secret + `"}`}, 500, "not-found", "unknown", "http-5xx"},
		{"incomplete", 404, `{"statusCode":404}`, []string{`{"statusCode":200,"data":{"totalCount":2,"list":[]}}`}, 200, "not-found", "unknown", "incomplete"},
		{"duplicate", 404, `{"statusCode":404}`, []string{`{"statusCode":200,"data":{"totalCount":2,"list":[{"code":"` + incidentPostCode + `"},{"code":"` + incidentPostCode + `"}]}}`}, 200, "not-found", "unknown", "duplicate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-management-token" {
					if r.Method != http.MethodPost {
						t.Error("token method")
					}
					fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
					return
				}
				switch r.URL.Path {
				case "/api/v3/get-post":
					if r.Method != http.MethodGet || r.URL.Query().Get("code") != incidentPostCode {
						t.Error("wrong GET identity")
					}
					w.WriteHeader(tc.getHTTP)
					fmt.Fprint(w, tc.getBody)
				case "/api/v3/list-post":
					if r.Method != http.MethodPost {
						t.Error("batch list method")
						w.WriteHeader(405)
						return
					}
					var req struct {
						PostCodes []string `json:"postCodes"`
						Page      int      `json:"page"`
						Limit     int      `json:"limit"`
					}
					if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.PostCodes) != 1 || req.PostCodes[0] != incidentPostCode || req.Page != page+1 || req.Limit != 50 {
						t.Error("list must be exact-code scoped and paginated")
					}
					i := page
					page++
					if i >= len(tc.pages) {
						t.Error("extra page")
						w.WriteHeader(500)
						return
					}
					w.WriteHeader(tc.listHTTP)
					fmt.Fprint(w, tc.pages[i])
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			got := probePostRecovery(client, incidentPostCode)
			if got.GET.State != tc.wantGET || got.List.State != tc.wantList {
				t.Errorf("states get=%s list=%s want=%s/%s", got.GET.State, got.List.State, tc.wantGET, tc.wantList)
			}
			reason := got.GET.Reason
			if tc.wantGET != "unknown" {
				reason = got.List.Reason
			}
			if reason != tc.wantReason {
				t.Errorf("reason=%s want=%s", reason, tc.wantReason)
			}
			if tc.name == "business-403" && (got.GET.Business != 403 || got.GET.APICode != 1001) {
				t.Error("business status/API code suppressed")
			}
			if tc.name == "missing" && (got.GET.Business != 404 || got.GET.APICode != 4041) {
				t.Error("404 diagnostics missing")
			}
			if tc.name == "http-403" && got.GET.HTTP != 403 {
				t.Error("HTTP 403 suppressed")
			}
			output := formatPostRecovery(got)
			if strings.Contains(output, secret) || strings.Contains(output, "mock-token") || strings.Contains(output, "message") || strings.Contains(output, "private") {
				t.Errorf("secret in output")
			}
		})
	}
}
