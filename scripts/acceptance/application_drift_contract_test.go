package acceptance

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Some live installations answer success:false when an update does not
// preserve the current application identity and essential settings.
func TestApplicationRemoteDriftPreservesStableConfiguration(t *testing.T) {
	a := &mockApplication{requireUpdateIdentity: true}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	name := "hermesacc-1234567890abcdef"
	if err := runApplicationTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, name); err != nil {
		t.Fatalf("out-of-band name drift did not preserve stable app settings: %v", err)
	}
	if a.id != "" || a.deletes != 1 {
		t.Fatal("test application remained after drift fixture")
	}
}
