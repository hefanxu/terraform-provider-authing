package acceptance

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

func TestNamespaceCleanupRefusesAnyGlobalPolicyOrUnknownInventory(t *testing.T) {
	for _, tc := range []struct {
		name, response string
	}{
		{"nonempty", `{"statusCode":200,"data":{"totalCount":1,"list":[{"policyId":"foreign"}]}}`},
		{"missing-total", `{"statusCode":200,"data":{"list":[]}}`},
		{"server-error", `{"statusCode":503,"message":"secret-marker"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockNamespace{code: namespaceTestCode, name: namespaceTestCode, description: "hermesacc ownership " + namespaceTestCode}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/list-data-policies" {
					if r.Method != http.MethodGet || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "1" {
						t.Error("policy inventory was not scoped to one complete first page")
					}
					_, _ = w.Write([]byte(tc.response))
					return
				}
				mock.serve(w, r)
			}))
			defer server.Close()
			client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
			if err != nil {
				t.Fatal("client setup failed")
			}
			if cleanupNamespace(client, namespaceTestCode, namespaceTestCode, "hermesacc ownership "+namespaceTestCode) == nil {
				t.Fatal("unsafe namespace deletion was accepted")
			}
			if mock.deletes != 0 || mock.code != namespaceTestCode {
				t.Fatal("namespace changed despite nonempty or unknown policy inventory")
			}
		})
	}
}

func TestNamespaceRecoveryReportsAnyGlobalPolicyAsNonempty(t *testing.T) {
	mock := &mockNamespace{code: namespaceTestCode, name: namespaceTestCode, description: "hermesacc ownership " + namespaceTestCode}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/list-data-policies" {
			_, _ = w.Write([]byte(`{"statusCode":200,"data":{"totalCount":1,"list":[{"policyId":"foreign"}]}}`))
			return
		}
		mock.serve(w, r)
	}))
	defer server.Close()
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
	if err != nil {
		t.Fatal("client setup failed")
	}
	if got := probeNamespaceRecoveryDiagnostic(client, namespaceTestCode); got.Status != "owned_nonempty" {
		t.Fatalf("global policy should block empty inventory: %s", got.Status)
	}
}
