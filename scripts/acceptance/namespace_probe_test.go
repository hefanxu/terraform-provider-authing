package acceptance

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

// Separate opt-in from the destructive acceptance flag.
var namespaceRecoveryLive = flag.Bool("authing-namespace-recovery-sandbox", false, "opt in to read-only namespace recovery")

// Preserve the existing status-only test seam; the live path uses diagnostics.
func probeNamespaceRecovery(client *authingapi.Client, code string) string {
	return probeNamespaceRecoveryDiagnostic(client, code).Status
}

func namespaceRecoveryGuard(enabled bool, env map[string]string, code string) bool {
	return enabled && env["AUTHING_ACCEPTANCE_CONFIRM"] == "READ_ONLY_SANDBOX" &&
		env["AUTHING_ACCESS_KEY_ID"] != "" && env["AUTHING_ACCESS_KEY_SECRET"] != "" &&
		sandboxCode.MatchString(code)
}

func TestNamespaceRecoveryProbe(t *testing.T) {
	if !*namespaceRecoveryLive {
		t.Skip("requires -authing-namespace-recovery-sandbox and exact read-only confirmation")
	}
	code := os.Getenv("AUTHING_TEST_OBJECT_CODE")
	env := map[string]string{
		"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"),
		"AUTHING_ACCESS_KEY_ID":      os.Getenv("AUTHING_ACCESS_KEY_ID"),
		"AUTHING_ACCESS_KEY_SECRET":  os.Getenv("AUTHING_ACCESS_KEY_SECRET"),
	}
	if !namespaceRecoveryGuard(*namespaceRecoveryLive, env, code) {
		t.Fatal("namespace recovery requires exact confirmation, generated code, and sandbox credentials")
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: env["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: env["AUTHING_ACCESS_KEY_SECRET"], Host: os.Getenv("AUTHING_HOST")})
	if err != nil {
		t.Fatal("namespace recovery client setup failed (output suppressed)")
	}
	t.Log(formatNamespaceRecoveryLog(probeNamespaceRecoveryDiagnostic(client, code), code))
}

func TestNamespaceRecoveryProbeMock(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture *mockNamespace
		want    string
	}{
		{"absent", &mockNamespace{}, "absent"},
		{"owned-empty", &mockNamespace{code: namespaceTestCode, name: namespaceTestCode, description: "hermesacc ownership " + namespaceTestCode}, "owned_empty"},
		{"owned-nonempty", &mockNamespace{code: namespaceTestCode, name: namespaceTestCode, description: "hermesacc ownership " + namespaceTestCode, children: true}, "owned_nonempty"},
		{"foreign", &mockNamespace{code: namespaceTestCode, name: namespaceTestCode, description: "hermesacc ownership " + namespaceTestCode, foreign: true}, "foreign"},
		{"wrong-code", &mockNamespace{code: namespaceTestCode, name: namespaceTestCode, description: "hermesacc ownership " + namespaceTestCode, wrongCode: true}, "foreign"},
		{"incomplete", &mockNamespace{code: namespaceTestCode, name: namespaceTestCode, description: "hermesacc ownership " + namespaceTestCode, incompleteInventory: true}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := namespaceClient(t, tc.fixture)
			defer server.Close()
			got := probeNamespaceRecovery(client, namespaceTestCode)
			if got != tc.want {
				t.Fatalf("status=%s want %s", got, tc.want)
			}
			for _, path := range tc.fixture.paths {
				if strings.HasPrefix(path, "POST ") && path != "POST /api/v3/get-management-token" {
					t.Fatalf("write attempted: %s", path)
				}
			}
		})
	}
}

func TestNamespaceRecoveryProbeGuard(t *testing.T) {
	valid := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}
	if !namespaceRecoveryGuard(true, valid, namespaceTestCode) {
		t.Fatal("valid read-only request rejected")
	}
	for _, tc := range []struct {
		name    string
		enabled bool
		env     map[string]string
		code    string
	}{
		{"disabled", false, valid, namespaceTestCode},
		{"missing-code", true, valid, ""},
		{"invalid-code", true, valid, "default"},
		{"non-exact-code", true, valid, namespaceTestCode + ";"},
		{"wrong-confirmation", true, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "DESTRUCTIVE_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}, namespaceTestCode},
		{"missing-key", true, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_SECRET": "secret"}, namespaceTestCode},
		{"missing-secret", true, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key"}, namespaceTestCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if namespaceRecoveryGuard(tc.enabled, tc.env, tc.code) {
				t.Fatal("unsafe recovery request accepted")
			}
		})
	}
}

func TestNamespaceRecoveryProbeInventoryFailureIsUnknown(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   string
		status int
		body   string
	}{
		{"roles-403", "/api/v3/list-permission-namespace-roles", 403, `{"statusCode":403,"message":"secret-marker"}`},
		{"resources-500", "/api/v3/list-resources", 500, `{"statusCode":500,"message":"secret-marker"}`},
		{"data-resources-malformed", "/api/v3/list-data-resources", 200, `{"statusCode":200,"data":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := &mockNamespace{code: namespaceTestCode, name: namespaceTestCode, description: "hermesacc ownership " + namespaceTestCode, children: true}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tc.path {
					if r.Method != http.MethodGet {
						t.Error("inventory write attempted")
					}
					w.WriteHeader(tc.status)
					w.Write([]byte(tc.body))
					return
				}
				n.serve(w, r)
			}))
			defer server.Close()
			client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			if got := probeNamespaceRecovery(client, namespaceTestCode); got != "unknown" {
				t.Fatalf("status=%s want unknown", got)
			}
		})
	}
}

func TestNamespaceRecoveryProbeDoesNotConflateFailureWithAbsence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"http-403", 403, `{"statusCode":403,"message":"secret-marker"}`},
		{"http-500", 500, `{"statusCode":500,"message":"secret-marker"}`},
		{"business-403", 200, `{"statusCode":403,"message":"secret-marker"}`},
		{"business-500", 200, `{"statusCode":500,"message":"secret-marker"}`},
		{"malformed", 200, `{}`},
		{"empty-200", 200, `{"statusCode":200,"data":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-management-token" {
					w.Write([]byte(`{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`))
					return
				}
				if r.Method != http.MethodGet || r.URL.Path != "/api/v3/get-permission-namespace" || r.URL.Query().Get("code") != namespaceTestCode {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			if got := probeNamespaceRecovery(client, namespaceTestCode); got != "unknown" {
				t.Fatalf("status=%s want unknown", got)
			}
		})
	}
}
