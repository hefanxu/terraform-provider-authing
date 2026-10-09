package acceptance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

func tenantResultStage(r tenantProbeResult, path string) tenantProbeStage {
	switch path {
	case "/api/v3/list-tenants":
		return r.List
	case "/api/v3/get-tenant":
		return r.GET
	case "/api/v3/list-tenant-users":
		return r.Users
	case "/api/v3/list-tenant-admin":
		return r.Admins
	default:
		return r.Orgs
	}
}
func TestTenantRecoveryStatusFailures(t *testing.T) {
	for _, path := range []string{"/api/v3/list-tenants", "/api/v3/get-tenant", "/api/v3/list-tenant-users", "/api/v3/list-tenant-admin", "/api/v3/list-organizations"} {
		for _, tc := range []struct {
			name, category      string
			http, business, api int
		}{
			{"http403", "http-4xx", 403, 0, 0}, {"http503", "http-5xx", 503, 0, 0}, {"business403", "business-4xx", 200, 403, 4031}, {"business503", "business-5xx", 200, 503, 5031},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				c, calls := recoveryClient(t, func(p string, page int, e map[string]any) (int, any) {
					if p == path && (p == "/api/v3/get-tenant" || page == 2) {
						e["statusCode"] = tc.business
						if tc.http != 200 {
							e["statusCode"] = tc.http
						}
						e["apiCode"] = tc.api
						return tc.http, e
					}
					return 200, e
				})
				r := probeTenantRecovery(c, incidentTenantName)
				s := tenantResultStage(r, path)
				if s.Category != tc.category || s.Business != tc.business || s.APICode != tc.api || tc.http != 200 && s.HTTP != tc.http || tc.http == 200 && s.HTTP != 0 {
					t.Fatalf("wrong numeric classification: %+v", s)
				}
				if path == "/api/v3/list-tenants" && (r.GET.Category != "not-run" || len(*calls) != 3) {
					t.Fatal("early candidate used after failed page")
				}
				if path == "/api/v3/get-tenant" && r.Users.Category != "not-run" {
					t.Fatal("failed GET used as scope")
				}
				if path == "/api/v3/list-tenant-users" && r.Orgs.Category != "nonempty" {
					t.Fatal("independent scope diagnostic suppressed")
				}
				output := formatTenantRecovery(r)
				for _, value := range []string{"raw-id-marker", "body-marker", "pii-marker"} {
					if strings.Contains(output, value) {
						t.Fatal("response exposed")
					}
				}
			})
		}
	}
}
func TestTenantRecoveryMalformedInventories(t *testing.T) {
	for _, path := range []string{"/api/v3/list-tenants", "/api/v3/list-tenant-users", "/api/v3/list-tenant-admin", "/api/v3/list-organizations"} {
		for _, tc := range []struct {
			name, category, shape string
			mutate                func(map[string]any)
		}{
			{"missing-count", "invalid-shape", "missing-or-invalid-count", func(d map[string]any) { delete(d, "totalCount") }},
			{"null-count", "invalid-shape", "missing-or-invalid-count", func(d map[string]any) { d["totalCount"] = nil }},
			{"negative-count", "invalid-shape", "missing-or-invalid-count", func(d map[string]any) { d["totalCount"] = -1 }},
			{"missing-list", "invalid-shape", "missing-or-invalid-list", func(d map[string]any) { delete(d, "list") }},
			{"null-list", "invalid-shape", "missing-or-invalid-list", func(d map[string]any) { d["list"] = nil }},
			{"object-list", "invalid-shape", "missing-or-invalid-list", func(d map[string]any) { d["list"] = map[string]any{} }},
			{"changed-total", "incomplete-inventory", "count-list", func(d map[string]any) { d["totalCount"] = 52 }},
			{"empty-positive", "incomplete-inventory", "count-list", func(d map[string]any) { d["list"] = []any{} }},
			{"invalid-item", "invalid-shape", "invalid-item", func(d map[string]any) { d["list"] = []any{"pii-marker"} }},
			{"missing-identity", "invalid-shape", "invalid-identity", func(d map[string]any) { d["list"] = []any{map[string]any{}} }},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				c, _ := recoveryClient(t, func(p string, page int, e map[string]any) (int, any) {
					if p == path && page == 2 {
						tc.mutate(e["data"].(map[string]any))
					}
					return 200, e
				})
				r := probeTenantRecovery(c, incidentTenantName)
				s := tenantResultStage(r, path)
				if s.Category != tc.category || s.Shape != tc.shape || s.Pages != 2 {
					t.Fatalf("unsafe inventory: %+v", s)
				}
				if path == "/api/v3/list-tenants" && r.GET.Category != "not-run" {
					t.Fatal("incomplete inventory authorized GET")
				}
			})
		}
	}
}
func TestTenantRecoveryDuplicateAndForeignIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, path, category string
		mutate               func(map[string]any)
	}{
		{"duplicate-candidate", "/api/v3/list-tenants", "duplicate", func(e map[string]any) {
			e["data"].(map[string]any)["list"].([]any)[0].(map[string]any)["name"] = incidentTenantName
		}},
		{"duplicate-id", "/api/v3/list-tenants", "duplicate", func(e map[string]any) {
			e["data"].(map[string]any)["list"].([]any)[0].(map[string]any)["tenantId"] = "raw-id-marker"
		}},
		{"foreign-get-name", "/api/v3/get-tenant", "foreign", func(e map[string]any) { e["data"].(map[string]any)["name"] = "pii-marker" }},
		{"foreign-get-id", "/api/v3/get-tenant", "foreign", func(e map[string]any) { e["data"].(map[string]any)["tenantId"] = "foreign-id" }},
		{"foreign-user", "/api/v3/list-tenant-users", "foreign", func(e map[string]any) {
			e["data"].(map[string]any)["list"].([]any)[0].(map[string]any)["tenantId"] = "foreign-id"
		}},
		{"foreign-admin", "/api/v3/list-tenant-admin", "foreign", func(e map[string]any) {
			e["data"].(map[string]any)["list"].([]any)[0].(map[string]any)["tenantId"] = "foreign-id"
		}},
		{"foreign-org", "/api/v3/list-organizations", "foreign", func(e map[string]any) {
			e["data"].(map[string]any)["list"].([]any)[0].(map[string]any)["tenantId"] = "foreign-id"
		}},
		{"duplicate-user", "/api/v3/list-tenant-users", "duplicate", func(e map[string]any) {
			e["data"].(map[string]any)["list"].([]any)[0].(map[string]any)["memberId"] = "member-0"
		}},
		{"duplicate-org", "/api/v3/list-organizations", "duplicate", func(e map[string]any) {
			e["data"].(map[string]any)["list"].([]any)[0].(map[string]any)["organizationCode"] = "org-0"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := recoveryClient(t, func(p string, page int, e map[string]any) (int, any) {
				if p == tc.path && (page == 2 || p == "/api/v3/get-tenant") {
					tc.mutate(e)
				}
				return 200, e
			})
			r := probeTenantRecovery(c, incidentTenantName)
			s := tenantResultStage(r, tc.path)
			if s.Category != tc.category {
				t.Fatalf("unsafe identity accepted: %+v", s)
			}
			if tc.path == "/api/v3/list-tenants" && r.GET.Category != "not-run" || tc.path == "/api/v3/get-tenant" && r.Users.Category != "not-run" {
				t.Fatal("ambiguous identity used")
			}
		})
	}
}
func TestTenantRecoveryAppShapeDoesNotSuppressScopeDiagnostics(t *testing.T) {
	for _, value := range []any{nil, "bad", []string{"private-app"}} {
		c, _ := recoveryClient(t, func(p string, page int, e map[string]any) (int, any) {
			if p == "/api/v3/get-tenant" {
				e["data"].(map[string]any)["appIds"] = value
			}
			return 200, e
		})
		r := probeTenantRecovery(c, incidentTenantName)
		if r.GET.Category != "candidate" || r.Apps.Category == "empty" || r.Users.Category != "nonempty" || r.Admins.Category != "nonempty" || r.Orgs.Category != "nonempty" {
			t.Fatalf("missing diagnostics: %+v", r)
		}
	}
	c, _ := recoveryClient(t, func(p string, page int, e map[string]any) (int, any) {
		if p == "/api/v3/get-tenant" {
			delete(e["data"].(map[string]any), "appIds")
		}
		return 200, e
	})
	if r := probeTenantRecovery(c, incidentTenantName); r.Apps.Shape != "missing-or-invalid-appids" || r.Users.Category != "nonempty" {
		t.Fatalf("missing appIds accepted: %+v", r)
	}
}
func TestTenantRecoveryClosedOutputAndCoreGuard(t *testing.T) {
	c, calls := recoveryClient(t, nil)
	r := probeTenantRecovery(c, tenantTestName)
	if len(*calls) != 0 || r.List.Category != "guard" {
		t.Fatal("core guard made request")
	}
	r.List = tenantProbeStage{Stage: "pii-marker", Category: "pii-marker", Shape: "pii-marker"}
	r.Fingerprint = "pii-marker"
	if strings.Contains(formatTenantRecovery(r), "pii-marker") {
		t.Fatal("unclosed output")
	}
	for _, body := range []any{nil, map[string]any{}, map[string]any{"statusCode": 200, "apiCode": "pii-marker"}} {
		c, _ := recoveryClient(t, func(p string, page int, e map[string]any) (int, any) { return 200, body })
		if r := probeTenantRecovery(c, incidentTenantName); r.List.Category != "invalid-envelope" || r.GET.Category != "not-run" {
			t.Fatalf("invalid envelope trusted: %+v", r)
		}
	}
}
func TestTenantPinnedStageDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		path                   string
		data                   any
		stage, category, shape string
	}{
		{"/api/v3/get-tenant", map[string]any{"tenantId": "pinned-id", "name": tenantTestName}, "apps", "invalid-shape", "missing-or-invalid-appids"},
		{"/api/v3/list-tenant-users", map[string]any{"list": []any{}}, "users", "invalid-shape", "missing-or-invalid-count"},
		{"/api/v3/list-tenant-admin", map[string]any{"totalCount": 0}, "admins", "invalid-shape", "missing-or-invalid-list"},
		{"/api/v3/list-organizations", map[string]any{"totalCount": 1, "list": []any{}}, "organizations", "incomplete-inventory", "count-list"},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := any(map[string]any{"totalCount": 0, "list": []any{}})
				if r.URL.Path == "/api/v3/get-management-token" {
					data = map[string]any{"access_token": "token-marker", "expires_in": 3600}
				} else if r.URL.Path == "/api/v3/get-tenant" {
					data = map[string]any{"tenantId": "pinned-id", "name": tenantTestName, "appIds": []any{}}
				}
				if r.URL.Path == tc.path {
					data = tc.data
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": data})
			}))
			defer s.Close()
			c, e := authingapi.NewClient(authingapi.Options{Host: s.URL, AccessKeyID: "key", AccessKeySecret: "secret"})
			if e != nil {
				t.Fatal(e)
			}
			err := verifyEmptyOwnedTenant(c, "pinned-id", tenantTestName)
			var d tenantDiagnosticError
			if !errors.As(err, &d) || d.Diagnostic.Stage != tc.stage || d.Diagnostic.Category != tc.category || d.Diagnostic.Shape != tc.shape {
				t.Fatalf("wrong pinned diagnostic: %v", err)
			}
		})
	}
}

func TestTenantInventoryRefusesMissingScope(t *testing.T) {
	for _, kind := range []string{"users", "admins", "organizations"} {
		c, calls := recoveryClient(t, nil)
		stage, _ := tenantProbeInventory(c, kind, "", "")
		if stage.Category != "guard" || len(*calls) != 0 {
			t.Fatal("unscoped inventory requested")
		}
	}
}

func TestTenantRecoveryAbsentAndEmptyScopes(t *testing.T) {
	c, calls := recoveryClient(t, func(path string, page int, e map[string]any) (int, any) {
		e["data"] = map[string]any{"totalCount": 0, "list": []any{}}
		return 200, e
	})
	r := probeTenantRecovery(c, incidentTenantName)
	if r.List.Category != "absent" || r.List.Count != 0 || r.List.Pages != 1 || r.GET.Category != "not-run" || len(*calls) != 2 {
		t.Fatalf("absence overclaimed: %+v", r)
	}
	c, _ = recoveryClient(t, func(path string, page int, e map[string]any) (int, any) {
		if path != "/api/v3/list-tenants" && path != "/api/v3/get-tenant" {
			e["data"] = map[string]any{"totalCount": 0, "list": []any{}}
		}
		return 200, e
	})
	r = probeTenantRecovery(c, incidentTenantName)
	for _, s := range []tenantProbeStage{r.Users, r.Admins, r.Orgs} {
		if s.Category != "empty" || s.Count != 0 || s.Total != 0 || s.Pages != 1 {
			t.Fatalf("bad empty scope: %+v", s)
		}
	}
	if !strings.Contains(formatTenantRecovery(r), "authority=none") {
		t.Fatal("emptiness became deletion authority")
	}
}
func TestTenantRecoveryPageCapFailsClosed(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch r.URL.Path {
		case "/api/v3/get-management-token":
			data = map[string]any{"access_token": "token", "expires_in": 3600}
		case "/api/v3/list-tenants":
			pages++
			if r.Method != "GET" || r.URL.Query().Get("limit") != "50" {
				t.Error("bad page request")
			}
			list := []any{}
			for i := 0; i < 50; i++ {
				list = append(list, map[string]any{"tenantId": fmt.Sprint(pages, "-", i), "name": "pii-marker"})
			}
			if pages == 1 {
				list[0] = map[string]any{"tenantId": "raw-id-marker", "name": incidentTenantName}
			}
			data = map[string]any{"totalCount": 5001, "list": list}
		default:
			t.Error("cap did not stop lookup")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": data})
	}))
	defer server.Close()
	c, err := authingapi.NewClient(authingapi.Options{Host: server.URL, AccessKeyID: "key", AccessKeySecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	r := probeTenantRecovery(c, incidentTenantName)
	if pages != 100 || r.List.Category != "page-cap" || r.List.Count != 5000 || r.List.Total != 5001 || r.List.Business != 200 || r.GET.Category != "not-run" {
		t.Fatalf("cap evidence incorrect: %+v", r)
	}
}

func TestTenantRecoveryPreservesDeclaredCounts(t *testing.T) {
	c, _ := recoveryClient(t, nil)
	r := probeTenantRecovery(c, incidentTenantName)
	if r.List.Total != 51 || r.Users.Total != 51 || r.Orgs.Total != 51 {
		t.Fatal("declared totals lost")
	}
}
