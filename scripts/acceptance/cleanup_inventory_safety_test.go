package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

func cleanupMock(t *testing.T, fn func(*http.Request) any) *authingapi.Client {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v3/get-management-token" {
			json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"access_token": "mock-token", "expires_in": 3600}})
			return
		}
		json.NewEncoder(w).Encode(fn(r))
	}))
	t.Cleanup(s.Close)
	c, e := authingapi.NewClient(authingapi.Options{AccessKeyID: "mock-key", AccessKeySecret: "mock-secret", Host: s.URL})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestCleanupInventoryExhaustsPagesAndFailsClosed(t *testing.T) {
	for _, mode := range []string{"ok", "changed-total", "duplicate", "missing-count", "short-page", "late403"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			c := cleanupMock(t, func(r *http.Request) any {
				calls++
				if r.Method != "GET" || r.URL.Query().Get("limit") != "50" || r.URL.Query().Has("keywords") {
					t.Error("not complete unfiltered contract-valid inventory")
				}
				p, _ := strconv.Atoi(r.URL.Query().Get("page"))
				n := 51
				items := []any{}
				start, end := 0, 50
				if p == 2 {
					start, end = 50, 51
				}
				for i := start; i < end; i++ {
					items = append(items, map[string]any{"code": fmt.Sprint(i)})
				}
				d := map[string]any{"totalCount": n, "list": items}
				if p == 2 {
					switch mode {
					case "changed-total":
						d["totalCount"] = 52
					case "duplicate":
						d["list"] = []any{map[string]any{"code": "0"}}
					case "missing-count":
						delete(d, "totalCount")
					case "short-page":
						d["list"] = []any{}
					case "late403":
						return map[string]any{"statusCode": 403}
					}
				}
				return map[string]any{"statusCode": 200, "data": d}
			})
			items, state := cleanupInventory(c, cleanupFamily{Family: "group", Path: "/api/v3/list-groups", ID: "code", Paged: true}, nil, nil)
			if calls != 2 {
				t.Fatalf("did not exhaust pages: %d", calls)
			}
			if mode == "ok" {
				if state != "complete" || len(items) != 51 {
					t.Fatal("missing complete inventory")
				}
			} else if state == "complete" || len(items) != 0 {
				t.Fatal("partial inventory trusted")
			}
		})
	}
}
func TestCleanupRequestRefusesMutations(t *testing.T) {
	calls := 0
	c := cleanupMock(t, func(r *http.Request) any { calls++; return map[string]any{"statusCode": 200} })
	for _, p := range []string{"/api/v3/create-user", "/api/v3/update-group", "/api/v3/delete-tenant", "/api/v3/delete-group", "/api/v3/guess-list-auth-flow-functions"} {
		_, _, state := cleanupRead(c, p, "POST", nil)
		if state != "guard" {
			t.Fatal("mutation or guessed endpoint allowed")
		}
	}
	if calls != 0 {
		t.Fatal("unsafe network request")
	}
}
func TestCleanupDeleteRequiresOriginalStateAndEmptyDependencies(t *testing.T) {
	for _, tc := range []struct {
		authority, observed string
		marker, deps        bool
		ok                  bool
	}{
		{"", "", true, true, false}, {"candidate", "candidate", true, true, false},
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", true, true, false},
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false, true, false},
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true, false, false},
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true, true, true},
	} {
		if cleanupPinnedGuard(tc.authority, tc.observed, tc.marker, tc.deps) != tc.ok {
			t.Fatal("unsafe deletion authority")
		}
	}
}
