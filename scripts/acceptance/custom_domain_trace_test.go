package acceptance

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type mockCustomDomain struct {
	sync.Mutex
	domain, old, next string
	calls             []string
	creates, removes  int
}

func (m *mockCustomDomain) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.calls = append(m.calls, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	reply := func(v any) { _ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": v}) }
	if r.URL.RawQuery != "" {
		http.Error(w, "query forbidden", 500)
		return
	}
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		if r.Method != http.MethodPost {
			http.Error(w, "method", 500)
			return
		}
		reply(map[string]any{"access_token": "mock-token", "expires_in": 3600})
	case "/api/v3/get-custom-domain":
		if r.Method != http.MethodGet {
			http.Error(w, "method", 500)
			return
		}
		if m.domain == "" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		reply(map[string]any{"customDomain": m.domain, "dnsTxtName": "_verify." + m.domain, "dnsTxtValue": "mock-public-value", "dnsVerified": false, "cname": "edge.invalid", "httpsVerified": false, "httpsPrivateKey": "PRIVATE-KEY-MUST-NEVER-LEAK", "httpsCertificate": "CERT-MUST-NEVER-LEAK"})
	case "/api/v3/create-custom-domain":
		b, _ := io.ReadAll(r.Body)
		var v map[string]any
		if r.Method != http.MethodPost || json.Unmarshal(b, &v) != nil || len(v) != 1 || m.domain != "" || (v["customDomain"] != m.old && v["customDomain"] != m.next) {
			http.Error(w, "unsafe create", 500)
			return
		}
		m.domain = v["customDomain"].(string)
		m.creates++
		reply(map[string]string{"customDomain": m.domain})
	case "/api/v3/remove-custom-domain":
		b, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || len(b) != 0 || m.domain == "" {
			http.Error(w, "unsafe removal", 500)
			return
		}
		m.domain = ""
		m.removes++
		reply(map[string]bool{"success": true})
	default:
		http.Error(w, "unexpected API", 500)
	}
}
func TestMockCustomDomainReplacementTrace(t *testing.T) {
	a, err := newGroupCode()
	if err != nil {
		t.Fatal("failed to generate mock-only domain")
	}
	b, err := newGroupCode()
	if err != nil {
		t.Fatal("failed to generate replacement domain")
	}
	m := &mockCustomDomain{old: a + ".invalid", next: b + ".invalid"}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	err = runCustomDomainMockTrace(t.TempDir(), s.URL, m.old, m.next)
	if err != nil {
		t.Fatal(err)
	}
	m.Lock()
	defer m.Unlock()
	if m.domain != "" || m.creates != 2 || m.removes != 2 {
		t.Fatalf("replacement lifecycle counts: create=%d remove=%d remaining=%q", m.creates, m.removes, m.domain)
	}
	for _, call := range m.calls {
		if strings.Contains(call, "update-custom-domain") || strings.Contains(call, "verify-custom-domain") {
			t.Fatalf("unexpected mutation: %s", call)
		}
	}
}
func TestMockCustomDomainTraceRejectsNonMockTargets(t *testing.T) {
	const mockDomainA = "hermesacc-1234567890abcdef.invalid"
	const mockDomainB = "hermesacc-fedcba0987654321.invalid"
	for _, tc := range []struct{ host, old, next string }{
		{"https://api.authing.cn", mockDomainA, mockDomainB},
		{"http://127.0.0.1:12345", "sso.example.com", mockDomainB},
		{"http://127.0.0.1:12345", mockDomainA, mockDomainA},
	} {
		if runCustomDomainMockTrace(t.TempDir(), tc.host, tc.old, tc.next) == nil {
			t.Fatal("unsafe trace accepted")
		}
	}
}
func TestLiveCustomDomainRequiresVerifiedOwnership(t *testing.T) {
	t.Skip("BLOCKED: no separately owner-controlled disposable DNS domain or independently verified ownership; never invoke the Authing API from this test")
}
