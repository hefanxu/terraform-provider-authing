package acceptance

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestDestructiveLiveApplicationStrategyTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	name, err := newGroupCode()
	if err != nil {
		t.Fatal("random application name generation failed")
	}
	if err := runApplicationStrategyTrace(t.TempDir(), env, name); err != nil {
		t.Fatal(err)
	}
}

func TestMockApplicationStrategyRejectedDrift(t *testing.T) {
	a := &mockApplication{failStrategyDriftSuccess: true}
	server := httptest.NewServer(http.HandlerFunc(a.serve))
	defer server.Close()
	name := "hermesacc-1234567890abcdef"
	err := runApplicationStrategyTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, name)
	if err == nil || !strings.Contains(err.Error(), "phase=remote-drift") || !strings.Contains(err.Error(), "reason=update-unsuccessful") || !strings.Contains(err.Error(), "cleanup=confirmed") {
		t.Fatal("false-success strategy drift did not preserve classified first failure and cleanup")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "mock-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("leaked credential")
		}
	}
	a.Lock()
	defer a.Unlock()
	if a.id != "" || a.deletes != 1 || a.strategy != "DENY_ALL" {
		t.Fatal("failed strategy drift did not clean up the owned app")
	}
}

func TestMockApplicationStrategyTrace(t *testing.T) {
	a := &mockApplication{}
	var writes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/update-application-permission-strategy" {
			// The fixture records the resulting transition after each write.
			defer func() {
				a.Lock()
				writes = append(writes, a.strategy)
				a.Unlock()
			}()
		}
		a.serve(w, r)
	}))
	defer server.Close()
	name := "hermesacc-1234567890abcdef"
	if err := runApplicationStrategyTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret", "AUTHING_HOST": server.URL}, name); err != nil {
		t.Fatal(err)
	}
	a.Lock()
	defer a.Unlock()
	if a.id != "" || a.deletes != 1 || strings.Contains(strings.Join(a.paths, ","), "POST /api/v3/update-application,") {
		t.Fatalf("strategy trace did not isolate strategy-only reconciliation and deletion: deleted=%d present=%t app-update=%t paths=%v", a.deletes, a.id != "", strings.Contains(strings.Join(a.paths, ","), "POST /api/v3/update-application,"), a.paths)
	}
	if got := strings.Join(writes, ","); got != "DENY_ALL,ALLOW_ALL,DENY_ALL" {
		t.Fatalf("unexpected strategy transitions: %s", got)
	}
}
