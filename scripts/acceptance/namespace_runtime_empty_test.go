package acceptance

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

func TestNamespaceResourceInventoryAllowsOnlyCompleteEmptyRuntimeShape(t *testing.T) {
	for _, tc := range []struct {
		name, data, want string
		allow            bool
	}{
		{"complete-empty", `{"totalCount":0,"list":[]}`, "owned_empty", true},
		{"nonempty", `{"totalCount":1,"list":[{"code":"foreign"}]}`, "unknown", false},
		{"missing-total", `{"list":[]}`, "unknown", false},
		{"extra-message", `{"totalCount":0,"list":[],"message":"secret-marker"}`, "unknown", false},
		{"null-list", `{"totalCount":0,"list":null}`, "unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockNamespace{code: namespaceTestCode, name: namespaceTestCode, description: "hermesacc ownership " + namespaceTestCode}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/list-resources" {
					if r.Method != http.MethodGet || r.URL.Query().Get("namespace") != namespaceTestCode {
						t.Error("unexpected resource inventory request")
					}
					_, _ = fmt.Fprintf(w, `{"statusCode":200,"data":%s}`, tc.data)
					return
				}
				mock.serve(w, r)
			}))
			defer server.Close()
			client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key", AccessKeySecret: "secret", Host: server.URL})
			if err != nil {
				t.Fatal("client setup failed")
			}
			got := probeNamespaceRecoveryDiagnostic(client, namespaceTestCode)
			if got.Status != tc.want {
				t.Fatalf("status=%s want %s", got.Status, tc.want)
			}
			err = verifyOwnedNamespace(client, namespaceTestCode, namespaceTestCode, "hermesacc ownership "+namespaceTestCode)
			if (err == nil) != tc.allow {
				t.Fatalf("unsafe inventory acceptance=%v want %v", err == nil, tc.allow)
			}
			if mock.deletes != 0 {
				t.Fatal("inventory validation deleted a namespace")
			}
		})
	}
}
