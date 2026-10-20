package authingapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Authing/authing-golang-sdk/v3/dto"
)

// These contracts are independent of the SDK transport; paths and verbs come from
// the published Management V3 OpenAPI and the SDK's endpoint declarations.
func TestOperationMappings(t *testing.T) {
	seen := map[string]string{}
	for name, op := range operations {
		if op.path == "" || (op.method != http.MethodGet && op.method != http.MethodPost) {
			t.Errorf("invalid operation %s: %+v", name, op)
		}
		if previous := seen[op.method+" "+op.path]; previous != "" {
			t.Errorf("duplicate endpoint: %s and %s", previous, name)
		}
		seen[op.method+" "+op.path] = name
	}
	if len(operations) != 62 {
		t.Errorf("want 62 endpoints, got %d", len(operations))
	}
	for _, tc := range []struct{ name, method, path string }{
		{"GetUser", "GET", "/api/v3/get-user"},
		{"CreateGroup", "POST", "/api/v3/create-group"},
		{"GetModel", "GET", "/api/v3/metadata/get-model"},
		{"RemoveModel", "POST", "/api/v3/metadata/remove-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := operations[tc.name]
			if got.method != tc.method || got.path != tc.path {
				t.Errorf("got %+v, want %s %s", got, tc.method, tc.path)
			}
		})
	}
}

func TestTypedOperationsTransmitDTOs(t *testing.T) {
	var gotGet, gotCreate bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v3/get-management-token":
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"test-token","expires_in":3600}}`)
		case "/api/v3/get-user":
			gotGet = true
			if r.Method != http.MethodGet || r.URL.Query().Get("userId") != "user-123" {
				t.Errorf("GET payload not serialized to query: %s %s", r.Method, r.URL)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"userId":"user-123"}}`)
		case "/api/v3/create-group":
			gotCreate = true
			var body struct {
				Code string `json:"code"`
			}
			if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil || body.Code != "group-123" {
				t.Errorf("POST DTO missing code: method=%s body=%+v", r.Method, body)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"code":"group-123"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c, err := NewClient(Options{AccessKeyID: "test-id", AccessKeySecret: "test-secret", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.GetUser(&dto.GetUserDto{UserId: "user-123"}); got == nil || got.StatusCode != 200 {
		t.Errorf("GET response: %+v", got)
	}
	if got := c.CreateGroup(&dto.CreateGroupReqDto{Code: "group-123"}); got == nil || got.StatusCode != 200 {
		t.Errorf("POST response: %+v", got)
	}
	if !gotGet || !gotCreate {
		t.Errorf("missing calls: GET=%v POST=%v", gotGet, gotCreate)
	}
}

func TestTypedOperationRejectsBrokenResponses(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"server error", 500, `{"statusCode":500,"message":"failed"}`},
		{"invalid JSON", 200, `{`},
		{"empty JSON", 200, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v3/get-management-token" {
					fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"test-token","expires_in":3600}}`)
					return
				}
				w.WriteHeader(tc.code)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			c, err := NewClient(Options{AccessKeyID: "test-id", AccessKeySecret: "test-secret", Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			if res := c.GetUser(&dto.GetUserDto{}); res != nil {
				t.Errorf("expected nil for %s; got status %d", tc.name, res.StatusCode)
			}
		})
	}
}

func TestTypedOperationsPreserveBusinessStatus(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		status, wantStatus int
		body               string
		invoke             func(*Client) (int, bool)
	}{
		{"get", "GET", "/api/v3/get-user", 200, 404, `{"statusCode":404,"message":"not found"}`, func(c *Client) (int, bool) {
			r := c.GetUser(&dto.GetUserDto{})
			if r == nil {
				return 0, false
			}
			return r.StatusCode, true
		}},
		{"create", "POST", "/api/v3/create-group", 200, 403, `{"statusCode":403,"message":"denied"}`, func(c *Client) (int, bool) {
			r := c.CreateGroup(&dto.CreateGroupReqDto{})
			if r == nil {
				return 0, false
			}
			return r.StatusCode, true
		}},
		{"http404", "GET", "/api/v3/get-user", 404, 404, `{"statusCode":404,"message":"not found"}`, func(c *Client) (int, bool) {
			r := c.GetUser(&dto.GetUserDto{})
			if r == nil {
				return 0, false
			}
			return r.StatusCode, true
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v3/get-management-token" {
					fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"test-token","expires_in":3600}}`)
					return
				}
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("got %s %s; want %s %s", r.Method, r.URL.Path, tc.method, tc.path)
				}
				if r.Header.Get("Authorization") == "" {
					t.Error("missing bearer token")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			c, err := NewClient(Options{AccessKeyID: "test-id", AccessKeySecret: "test-secret", Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			status, ok := tc.invoke(c)
			if !ok || status != tc.wantStatus {
				t.Errorf("typed response lost status: ok=%v status=%d", ok, status)
			}
		})
	}
}
