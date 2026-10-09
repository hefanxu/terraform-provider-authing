package acceptance

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMockGroupUnknownCreateIDNeverDeletes(t *testing.T) {
	g := &mockGroup{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/create-group" {
			g.serve(httptest.NewRecorder(), r)
			fmt.Fprint(w, `{"statusCode":500}`)
			return
		}
		g.serve(w, r)
	}))
	defer server.Close()
	err := runGroupTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, "hermesacc-1234567890abcdef")
	if err == nil || !strings.Contains(err.Error(), "phase=apply-create") || !strings.Contains(err.Error(), "cleanup=unknown") || strings.Contains(err.Error(), "state_id_sha256=") {
		t.Fatalf("wrong diagnostic: %v", err)
	}
	fmt.Println(err)
	g.Lock()
	defer g.Unlock()
	if g.deletes != 0 || g.code == "" {
		t.Fatal("guessed deletion without creating state ID")
	}
}
func TestMockGroupReconcileAndCleanupFailures(t *testing.T) {
	for _, mode := range []string{"reconcile", "cleanup"} {
		t.Run(mode, func(t *testing.T) {
			g := &mockGroup{}
			updates := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/update-group" {
					updates++
					if updates == 6 {
						fmt.Fprint(w, `{"statusCode":500}`)
						return
					}
				}
				if mode == "cleanup" && r.URL.Path == "/api/v3/delete-groups-batch" {
					fmt.Fprint(w, `{"statusCode":500}`)
					return
				}
				g.serve(w, r)
			}))
			defer server.Close()
			err := runGroupTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, "hermesacc-1234567890abcdef")
			cleanup := "confirmed"
			if mode == "cleanup" {
				cleanup = "incomplete"
			}
			if err == nil || !strings.Contains(err.Error(), "phase=apply-reconcile") || !strings.Contains(err.Error(), "cleanup="+cleanup) || !strings.Contains(err.Error(), "state_id_sha256=") {
				t.Fatalf("first phase/fingerprint lost: %v", err)
			}
			fmt.Println(err)
		})
	}
}
