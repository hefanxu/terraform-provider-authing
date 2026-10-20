package acceptance

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// HCL does not configure nested login/protocol/branding options. The API
// payload must not synthesize zero-valued nested structs from the SDK DTO.
func TestApplicationCreateOmitsUnconfiguredNestedOptions(t *testing.T) {
	a := &mockApplication{rejectUnconfiguredNested: true}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	name := "hermesacc-1234567890abcdef"
	if err := runApplicationTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, name); err != nil {
		t.Fatalf("unconfigured nested fields broke minimal application lifecycle: %v", err)
	}
	if a.deletes != 1 || a.id != "" {
		t.Fatal("minimal application was not cleaned up")
	}
}
