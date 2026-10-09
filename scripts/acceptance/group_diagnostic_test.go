package acceptance

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMockGroupEmptyDescriptionReadbackDiagnostic(t *testing.T) {
	g := &mockGroup{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-group" {
			g.Lock()
			code, name, kind, description := g.code, g.name, g.kind, g.description
			g.Unlock()
			if code != "" && description == "" {
				fmt.Fprintf(w, `{"statusCode":200,"data":{"code":%q,"name":%q,"type":%q,"description":null}}`, code, name, kind)
				return
			}
		}
		g.serve(w, r)
	}))
	defer server.Close()
	err := runGroupTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, "hermesacc-1234567890abcdef")
	if err == nil || !strings.Contains(err.Error(), "phase=apply-empty-description") || !strings.Contains(err.Error(), "description_readback=null") || !strings.Contains(err.Error(), "group_readback=identity") {
		t.Fatalf("diagnostic missing: %v", err)
	}
	fmt.Println(err)
}
