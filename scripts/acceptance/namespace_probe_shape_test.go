package acceptance

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

func TestNamespaceResourceShapeDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, body, shape string
	}{
		{"missing-nested-status", `{"statusCode":200,"data":{"totalCount":0,"list":[],"message":"secret-marker"}}`, "missing-nested-status"},
		{"missing-nested-status-empty-page", `{"statusCode":200,"data":{"totalCount":0,"list":[]}}`, "missing-nested-status-empty-page"},
		{"data-not-object", `{"statusCode":200,"data":[]}`, "data-not-object"},
		{"data-null", `{"statusCode":200,"data":null}`, "data-not-object"},
		{"whitespace-before-object", `{"statusCode":200,"data": {"statusCode":200,"totalCount":0,"list":null}}`, "list-null"},
		{"missing-total", `{"statusCode":200,"data":{"statusCode":200,"list":[]}}`, "missing-total"},
		{"total-invalid", `{"statusCode":200,"data":{"statusCode":200,"totalCount":"secret-marker","list":[]}}`, "total-invalid"},
		{"missing-list", `{"statusCode":200,"data":{"statusCode":200,"totalCount":0}}`, "missing-list"},
		{"list-null", `{"statusCode":200,"data":{"statusCode":200,"totalCount":0,"list":null}}`, "list-null"},
		{"list-not-array", `{"statusCode":200,"data":{"statusCode":200,"totalCount":0,"list":{"secret-marker":true}}}`, "list-not-array"},
		{"nested-status-invalid", `{"statusCode":200,"data":{"statusCode":"secret-marker","totalCount":0,"list":[]}}`, "nested-status-invalid"},
		{"client-invalid-envelope", `secret-marker`, "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := &mockNamespace{code: namespaceTestCode, name: namespaceTestCode, description: "hermesacc ownership " + namespaceTestCode}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/list-resources" {
					if r.Method != http.MethodGet {
						t.Error("recovery attempted a write")
					}
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
			if got.Status != "unknown" || got.Stage != recoveryResources || got.Cause != recoveryInvalidEnvelope || string(got.Shape) != tc.shape {
				t.Fatalf("diagnostic labels=%s/%s/%s/%s", got.Status, got.Stage, got.Cause, got.Shape)
			}
			log := formatNamespaceRecoveryLog(got, namespaceTestCode)
			if log != "namespace recovery status=unknown stage=resources cause=invalid-envelope shape="+tc.shape+" code="+namespaceTestCode || strings.Contains(log, "secret-marker") {
				t.Fatalf("unsafe or incorrect log: %s", log)
			}
		})
	}
}

func TestNamespaceResourceShapeMalformedJSONNeverExposesInput(t *testing.T) {
	if shape := classifyResourceShape([]byte(`{"secret-marker":`)); shape != resourceShapeMalformedJSON {
		t.Fatalf("unexpected malformed JSON label: %s", shape)
	}
}

func TestNamespaceResourceShapeIsOnlyLoggedForResourceInvalidEnvelope(t *testing.T) {
	for _, d := range []namespaceRecoveryDiagnostic{
		{Status: "unknown", Stage: recoveryResources, Cause: recoveryBusiness4xx, Shape: "secret-marker"},
		{Status: "unknown", Stage: recoveryRoles, Cause: recoveryInvalidEnvelope, Shape: "secret-marker"},
		{Status: "unknown", Stage: recoveryResources, Cause: recoveryInvalidEnvelope, Shape: "secret-marker"},
	} {
		log := formatNamespaceRecoveryLog(d, namespaceTestCode)
		if strings.Contains(log, "secret-marker") {
			t.Fatalf("untrusted shape escaped: %s", log)
		}
		if d.Stage == recoveryResources && d.Cause == recoveryInvalidEnvelope && !strings.Contains(log, "shape=unavailable") {
			t.Fatalf("missing safe fallback shape: %s", log)
		}
		if (d.Stage != recoveryResources || d.Cause != recoveryInvalidEnvelope) && strings.Contains(log, "shape=") {
			t.Fatalf("shape logged at other stage/cause: %s", log)
		}
	}
}
