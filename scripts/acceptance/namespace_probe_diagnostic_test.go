package acceptance

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

func TestNamespaceRecoveryTypedDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, stage, cause string
		httpStatus                     int
	}{
		{"get-403", "/api/v3/get-permission-namespace", `{"statusCode":403,"message":"secret-marker"}`, "get", "business-4xx", 403},
		{"get-500", "/api/v3/get-permission-namespace", `{"statusCode":500,"message":"secret-marker"}`, "get", "business-5xx", 200},
		{"get-malformed", "/api/v3/get-permission-namespace", `{"statusCode":200,"data":{}}`, "get", "invalid-envelope", 200},
		{"get-invalid-http", "/api/v3/get-permission-namespace", `secret-marker`, "get", "invalid-envelope", 200},
		{"roles-403", "/api/v3/list-permission-namespace-roles", `{"statusCode":403,"message":"secret-marker"}`, "roles", "business-4xx", 200},
		{"roles-500", "/api/v3/list-permission-namespace-roles", `{"statusCode":500,"message":"secret-marker"}`, "roles", "business-5xx", 500},
		{"roles-malformed", "/api/v3/list-permission-namespace-roles", `{"statusCode":200,"data":{}}`, "roles", "invalid-envelope", 200},
		{"roles-incomplete", "/api/v3/list-permission-namespace-roles", `{"statusCode":200,"data":{"totalCount":1,"list":[]}}`, "roles", "incomplete-inventory", 200},
		{"resources-403", "/api/v3/list-resources", `{"statusCode":403,"message":"secret-marker"}`, "resources", "business-4xx", 403},
		{"resources-500", "/api/v3/list-resources", `{"statusCode":500,"message":"secret-marker"}`, "resources", "business-5xx", 200},
		{"resources-nested-403", "/api/v3/list-resources", `{"statusCode":200,"data":{"statusCode":403,"message":"secret-marker"}}`, "resources", "business-4xx", 200},
		{"resources-malformed", "/api/v3/list-resources", `{"statusCode":200,"data":{"totalCount":0,"list":[]}}`, "resources", "invalid-envelope", 200},
		{"resources-incomplete", "/api/v3/list-resources", `{"statusCode":200,"data":{"statusCode":200,"totalCount":1,"list":[]}}`, "resources", "incomplete-inventory", 200},
		{"resources-nested-500", "/api/v3/list-resources", `{"statusCode":200,"data":{"statusCode":500,"message":"secret-marker"}}`, "resources", "business-5xx", 200},
		{"data-resources-403", "/api/v3/list-data-resources", `{"statusCode":403,"message":"secret-marker"}`, "data-resources", "business-4xx", 403},
		{"data-resources-500", "/api/v3/list-data-resources", `{"statusCode":500,"message":"secret-marker"}`, "data-resources", "business-5xx", 200},
		{"data-resources-malformed", "/api/v3/list-data-resources", `{"statusCode":200,"data":{}}`, "data-resources", "invalid-envelope", 200},
		{"data-resources-incomplete", "/api/v3/list-data-resources", `{"statusCode":200,"data":{"totalCount":1,"list":[]}}`, "data-resources", "incomplete-inventory", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := &mockNamespace{code: namespaceTestCode, name: namespaceTestCode, description: "hermesacc ownership " + namespaceTestCode}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tc.path {
					if r.Method != http.MethodGet {
						t.Error("recovery attempted a write")
					}
					w.WriteHeader(tc.httpStatus)
					_, _ = w.Write([]byte(tc.body))
					return
				}
				n.serve(w, r)
			}))
			defer server.Close()
			client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			got := probeNamespaceRecoveryDiagnostic(client, namespaceTestCode)
			if got.Status != "unknown" || got.Stage != namespaceRecoveryStage(tc.stage) || got.Cause != namespaceRecoveryCause(tc.cause) {
				t.Fatalf("diagnostic=%+v want unknown/%s/%s", got, tc.stage, tc.cause)
			}
			log := formatNamespaceRecoveryLog(got, namespaceTestCode)
			if log != "namespace recovery status=unknown stage="+tc.stage+" cause="+tc.cause+" code="+namespaceTestCode || strings.Contains(log, "secret-marker") {
				t.Fatalf("unsafe or incorrect log: %s", log)
			}
		})
	}
}

func TestNamespaceRecoveryDiagnosticLogRejectsUntrustedFields(t *testing.T) {
	log := formatNamespaceRecoveryLog(namespaceRecoveryDiagnostic{Status: "secret-marker", Stage: "secret-marker", Cause: "secret-marker"}, "secret-marker")
	if strings.Contains(log, "secret-marker") || log != "namespace recovery status=unknown stage=get cause=invalid-envelope code=invalid" {
		t.Fatalf("unsafe log: %s", log)
	}
}

func TestNamespaceRecoveryTransportDiagnostic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-management-token" {
			_, _ = w.Write([]byte(`{"statusCode":200,"data":{"access_token":"token","expires_in":3600}}`))
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("no hijacker")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer server.Close()
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	got := probeNamespaceRecoveryDiagnostic(client, namespaceTestCode)
	if got.Status != "unknown" || got.Stage != "get" || got.Cause != "transport" {
		t.Fatalf("diagnostic=%+v", got)
	}
}
