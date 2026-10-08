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

const incidentApplicationCode = "hermesacc-c99546b76631e842"
const secondIncidentApplicationCode = "hermesacc-05aa25aef574a094"

func TestApplicationRecoveryProbe(t *testing.T) {
	code := incidentApplicationCode
	owned := map[string]any{"appId": "owned-id", "appName": code, "appIdentifier": code, "appDescription": "hermesacc ownership " + code, "appType": "web"}
	foreign := map[string]any{"appId": "foreign-id", "appName": code, "appIdentifier": code, "appDescription": "other", "appType": "web"}
	cases := []struct {
		name       string
		pages      [][]map[string]any
		get        map[string]any
		want       string
		incomplete bool
	}{
		{name: "absent", pages: [][]map[string]any{{}}, want: "absent"},
		{name: "owned", pages: [][]map[string]any{{owned}}, get: owned, want: "owned"},
		{name: "foreign", pages: [][]map[string]any{{foreign}}, get: foreign, want: "ambiguous"},
		{name: "ambiguous", pages: [][]map[string]any{{owned, foreign}}, get: owned, want: "ambiguous"},
		{name: "incomplete pagination", pages: [][]map[string]any{{{"appId": "other", "appName": "other"}}, {}}, incomplete: true, want: "ambiguous"},
		{name: "second page owned", pages: [][]map[string]any{{{"appId": "other", "appName": "other"}}, {owned}}, get: owned, want: "owned"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v3/get-management-token":
					fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
				case "/api/v3/list-applications":
					if r.Method != "GET" || r.URL.Query().Get("keywords") != "" || r.URL.Query().Get("isSelfBuiltApp") != "" {
						t.Errorf("filtered or non-read-only list")
						return
					}
					page := r.URL.Query().Get("page")
					var n int
					fmt.Sscan(page, &n)
					calls++
					if n < 1 || n > len(tc.pages) {
						t.Errorf("unexpected page %s", page)
						return
					}
					total := 0
					for _, p := range tc.pages {
						total += len(p)
					}
					if tc.incomplete {
						total++
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"list": tc.pages[n-1], "totalCount": total}})
				case "/api/v3/get-application":
					if r.Method != "GET" || tc.get == nil || r.URL.Query().Get("appId") != tc.get["appId"] {
						t.Errorf("unexpected GET id")
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": tc.get})
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
			}))
			defer server.Close()
			client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			got := probeApplication(client, code)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			if calls != len(tc.pages) && tc.name != "ambiguous" {
				t.Fatalf("read %d pages, expected %d", calls, len(tc.pages))
			}
		})
	}
}

func TestApplicationRecoveryGuard(t *testing.T) {
	for _, tc := range []struct {
		code, confirm string
		allowed       bool
	}{
		{incidentApplicationCode, "READ_ONLY_SANDBOX", true},
		{secondIncidentApplicationCode, "READ_ONLY_SANDBOX", true},
		{incidentApplicationCode, "DESTRUCTIVE_SANDBOX", false},
		{"hermesacc-1234567890abcdef", "READ_ONLY_SANDBOX", false},
		{incidentApplicationCode + "x", "READ_ONLY_SANDBOX", false},
	} {
		if (applicationRecoveryGuard(tc.code, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": tc.confirm, "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}) == nil) != tc.allowed {
			t.Errorf("guard mismatch for %q %q", tc.code, tc.confirm)
		}
	}
}

func TestApplicationDiscoveryUnfiltered(t *testing.T) {
	code := incidentApplicationCode
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/get-management-token":
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
		case "/api/v3/list-applications":
			if r.URL.Query().Get("keywords") != "" || r.URL.Query().Get("isSelfBuiltApp") != "" {
				fmt.Fprint(w, `{"statusCode":200,"data":{"list":[],"totalCount":0}}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"list": []map[string]string{{"appId": "owned", "appName": code, "appIdentifier": code}}, "totalCount": 1}})
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	id, err := findApplication(client, code)
	if err != nil || id != "owned" {
		t.Fatalf("discovery missed owned application: id=%q err=%v", id, err)
	}
}

func TestMockApplicationFailurePreservesPhase(t *testing.T) {
	a := &mockApplication{failStrategy: true}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	name := "hermesacc-1234567890abcdef"
	err := runApplicationTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, name)
	if err == nil || !strings.Contains(err.Error(), "phase=apply-create") || !strings.Contains(err.Error(), "cleanup=incomplete") {
		t.Fatalf("expected original phase and incomplete cleanup: %v", err)
	}
	a.Lock()
	defer a.Unlock()
	if a.id == "" || a.deletes != 0 {
		t.Fatal("deleted application without state-backed ID")
	}
}
