package acceptance

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplicationDriftReportsSafeUpdateRejection(t *testing.T) {
	a := &mockApplication{failDriftUpdate: true}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	name := "hermesacc-1234567890abcdef"
	err := runApplicationTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, name)
	if err == nil || !strings.Contains(err.Error(), "phase=remote-drift") || !strings.Contains(err.Error(), "reason=update-rejected") || !strings.Contains(err.Error(), "cleanup=confirmed") {
		t.Fatalf("missing safe remote drift category: %v", err)
	}
	for _, secret := range []string{"secret-marker", "private-uri", "key-marker", "credential-marker"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("remote drift error leaked response or credential")
		}
	}
	if a.id != "" || a.deletes != 1 {
		t.Fatal("failed drift did not clean up owned application")
	}
}
