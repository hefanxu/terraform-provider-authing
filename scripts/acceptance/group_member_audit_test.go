package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"terraform-provider-authing/internal/authingapi"
)

const auditCode = "hermesacc-1234567890abcdef"
const auditUser = "hermesacc-fedcba0987654321"

func TestGroupMemberAuditGuard(t *testing.T) {
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}
	start, end := "2026-10-09T12:00:00Z", "2026-10-09T13:00:00Z"
	if _, _, ok := groupMemberAuditGuard(true, env, start, end); !ok {
		t.Fatal("guard rejected window")
	}
	for _, tc := range []struct {
		enabled    bool
		start, end string
	}{{false, start, end}, {true, "", end}, {true, start, "bad"}, {true, end, start}} {
		if _, _, ok := groupMemberAuditGuard(tc.enabled, env, tc.start, tc.end); ok {
			t.Fatal("unsafe guard")
		}
	}
	env["AUTHING_ACCEPTANCE_CONFIRM"] = "DESTRUCTIVE_SANDBOX"
	if _, _, ok := groupMemberAuditGuard(true, env, start, end); ok {
		t.Fatal("destructive confirmation accepted")
	}
}

func TestGroupMemberAuditReadOnlyMock(t *testing.T) {
	const secret = "PRIVATE-API-RESPONSE"
	for _, tc := range []struct {
		name, groupList, userList, groupGet, userGet string
		status                                       int
		want                                         string
	}{
		{"candidates", `{"statusCode":200,"data":{"totalCount":1,"list":[{"code":"` + auditCode + `","name":"` + auditCode + `","description":"hermesacc ownership ` + auditCode + `","type":"static"}]}}`, `{"statusCode":200,"data":{"totalCount":1,"list":[{"userId":"opaque-id","username":"` + auditUser + `","createdAt":"2026-10-09T12:30:00Z"}]}}`, `{"statusCode":200,"data":{"code":"` + auditCode + `","name":"` + auditCode + `","description":"hermesacc ownership ` + auditCode + `","type":"static"}}`, `{"statusCode":200,"data":{"userId":"opaque-id","username":"` + auditUser + `","nickname":"` + auditUser + `","createdAt":"2026-10-09T12:30:00Z"}}`, 200, "candidates"},
		{"malformed", `{"statusCode":200,"data":{}}`, `{"statusCode":200,"data":{"totalCount":0,"list":[]}}`, "", "", 200, "unknown"},
		{"duplicate", `{"statusCode":200,"data":{"totalCount":2,"list":[{"code":"` + auditCode + `"},{"code":"` + auditCode + `"}]}}`, `{"statusCode":200,"data":{"totalCount":0,"list":[]}}`, "", "", 200, "unknown"},
		{"incomplete", `{"statusCode":200,"data":{"totalCount":2,"list":[]}}`, `{"statusCode":200,"data":{"totalCount":0,"list":[]}}`, "", "", 200, "unknown"},
		{"business-403", `{"statusCode":403,"message":"` + secret + `"}`, `{"statusCode":200,"data":{"totalCount":0,"list":[]}}`, "", "", 200, "unknown"},
		{"http-500", `{"statusCode":500,"message":"` + secret + `"}`, `{"statusCode":200,"data":{"totalCount":0,"list":[]}}`, "", "", 500, "unknown"},
		{"out-of-window", `{"statusCode":200,"data":{"totalCount":0,"list":[]}}`, `{"statusCode":200,"data":{"totalCount":1,"list":[{"userId":"opaque-id","username":"` + auditUser + `","createdAt":"2026-10-09T11:00:00Z"}]}}`, "", `{"statusCode":200,"data":{"userId":"opaque-id","username":"` + auditUser + `","nickname":"` + auditUser + `","createdAt":"2026-10-09T11:00:00Z"}}`, 200, "zero"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-management-token" {
					fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
					return
				}
				switch r.URL.Path {
				case "/api/v3/list-groups":
					if r.Method != "GET" || r.URL.Query().Get("keywords") != "hermesacc" || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "50" {
						t.Error("group scope")
					}
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.groupList)
				case "/api/v3/list-users":
					if r.Method != "POST" {
						t.Error("users list must POST")
						return
					}
					var v struct {
						Keywords string `json:"keywords"`
						Options  struct {
							Pagination struct {
								Page  int `json:"page"`
								Limit int `json:"limit"`
							} `json:"pagination"`
							FuzzySearchOn []string `json:"fuzzySearchOn"`
						} `json:"options"`
					}
					if json.NewDecoder(r.Body).Decode(&v) != nil || v.Keywords != "hermesacc" || v.Options.Pagination.Page != 1 || v.Options.Pagination.Limit != 50 || len(v.Options.FuzzySearchOn) != 1 || v.Options.FuzzySearchOn[0] != "username" {
						t.Error("user scope")
					}
					fmt.Fprint(w, tc.userList)
				case "/api/v3/get-group":
					if r.Method != "GET" || r.URL.Query().Get("code") != auditCode {
						t.Error("group exact GET")
					}
					fmt.Fprint(w, tc.groupGet)
				case "/api/v3/get-user":
					if r.Method != "GET" || r.URL.Query().Get("userId") != "opaque-id" {
						t.Error("user exact GET")
					}
					fmt.Fprint(w, tc.userGet)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(405)
				}
			}))
			defer server.Close()
			client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			start, _ := time.Parse(time.RFC3339, "2026-10-09T12:00:00Z")
			end, _ := time.Parse(time.RFC3339, "2026-10-09T13:00:00Z")
			result := probeGroupMemberAudit(client, start, end)
			if result.State != tc.want {
				t.Errorf("state=%s want %s", result.State, tc.want)
			}
			text := formatGroupMemberAudit(result)
			for _, bad := range []string{secret, "opaque-id", "mock-token"} {
				if strings.Contains(text, bad) {
					t.Errorf("leaked %s", bad)
				}
			}
			if tc.name == "candidates" && (result.Groups != 1 || result.Users != 1 || !strings.Contains(text, auditCode) || !strings.Contains(text, "user_hash=")) {
				t.Errorf("missing candidate evidence: %s", text)
			}
		})
	}
}
