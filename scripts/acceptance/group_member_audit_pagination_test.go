package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"terraform-provider-authing/internal/authingapi"
)

func TestGroupMemberAuditUserPaginationFailsClosed(t *testing.T) {
	entries := make([]map[string]string, 50)
	for i := range entries {
		entries[i] = map[string]string{"userId": fmt.Sprintf("unrelated-%d", i), "username": "other"}
	}
	for _, tc := range []struct {
		name   string
		second string
		first  string
	}{
		{"duplicate", `{"statusCode":200,"data":{"totalCount":51,"list":[{"userId":"unrelated-0","username":"other"}]}}`, ""},
		{"missing-page", `{"statusCode":200,"data":{"totalCount":51,"list":[]}}`, ""},
		{"changed-total", `{"statusCode":200,"data":{"totalCount":52,"list":[{"userId":"unrelated-50","username":"other"}]}}`, ""},
		{"user-malformed", ``, `{"statusCode":200,"data":{"totalCount":1}}`},
		{"user-business-403", ``, `{"statusCode":403}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v3/get-management-token":
					fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
				case "/api/v3/list-groups":
					if r.Method != "GET" {
						t.Error("group not GET")
					}
					fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":0,"list":[]}}`)
				case "/api/v3/list-users":
					if r.Method != "POST" {
						t.Error("user list not POST")
					}
					page++
					var req struct {
						Keywords string `json:"keywords"`
						Options  struct{ Pagination struct{ Page, Limit int } }
					}
					if json.NewDecoder(r.Body).Decode(&req) != nil || req.Keywords != "hermesacc" || req.Options.Pagination.Page != page || req.Options.Pagination.Limit != 50 {
						t.Error("pagination request wrong")
					}
					if tc.first != "" {
						fmt.Fprint(w, tc.first)
					} else if page == 1 {
						json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": 51, "list": entries}})
					} else {
						fmt.Fprint(w, tc.second)
					}
				default:
					t.Errorf("unexpected write or GET %s %s", r.Method, r.URL.Path)
					w.WriteHeader(405)
				}
			}))
			defer server.Close()
			client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			start := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
			result := probeGroupMemberAudit(client, start, start.Add(time.Hour))
			if result.State != "unknown" || result.Groups != 0 || result.Users != 0 || len(result.Codes) != 0 || len(result.UserHashes) != 0 {
				t.Fatalf("partial inventory reported: %+v", result)
			}
		})
	}
}
