package authingapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTrustedTLSTokenAndRequest(t *testing.T) {
	var tokens, calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v3/get-management-token":
			tokens++
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "" {
				t.Errorf("token request method/auth: %s %q", r.Method, r.Header.Get("Authorization"))
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("token body: %v", err)
			}
			if body["accessKeyId"] != "key" || body["accessKeySecret"] != "secret" {
				t.Errorf("token body mismatch")
			}
			_, _ = w.Write([]byte(`{"statusCode":200,"data":{"access_token":"token-1","expires_in":3600}}`))
		case "/api/v3/get-model":
			calls++
			if r.Header.Get("Authorization") != "Bearer token-1" {
				t.Errorf("authorization missing")
			}
			_, _ = w.Write([]byte(`{"statusCode":200,"data":{"id":"model"}}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewClient(Options{Host: server.URL, AccessKeyID: "key", AccessKeySecret: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		body, err := client.SendHttpRequest("/api/v3/get-model", "GET", map[string]any{"id": "model"})
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(body) {
			t.Fatalf("invalid body: %q", body)
		}
	}
	if tokens != 1 || calls != 2 {
		t.Fatalf("tokens=%d calls=%d", tokens, calls)
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc, customize func(*Options)) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	opts := Options{Host: server.URL, AccessKeyID: "key", AccessKeySecret: "super-secret", HTTPClient: server.Client()}
	if customize != nil {
		customize(&opts)
	}
	client, err := NewClient(opts)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return client, server
}

func tokenResponse(w http.ResponseWriter, token string, lifetime int) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"statusCode":200,"data":{"access_token":%q,"expires_in":%d}}`, token, lifetime)
}

func TestRejectUntrustedTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted request reached server") }))
	defer server.Close()
	client, err := NewClient(Options{Host: server.URL, AccessKeyID: "key", AccessKeySecret: "super-secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SendHttpRequest("/api/v3/get-model", "GET", nil)
	if err == nil {
		t.Fatal("untrusted TLS accepted")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("secret leaked: %s", err)
	}
}

func TestQueryEncodingAndHeaders(t *testing.T) {
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			if r.Header.Get("Authorization") != "" || r.Header.Get("x-authing-app-tenant-id") != "tenant" {
				t.Errorf("token headers: %v", r.Header)
			}
			tokenResponse(w, "token", 3600)
			return
		}
		if r.Method != "GET" || r.Header.Get("Content-Type") != "" {
			t.Errorf("GET method/headers: %s %v", r.Method, r.Header)
		}
		if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("x-authing-userpool-id") != "pool" || r.Header.Get("x-authing-app-tenant-id") != "tenant" {
			t.Errorf("API auth headers: %v", r.Header)
		}
		want := url.Values{"empty": {""}, "off": {"false"}, "zero": {"0"}, "phrase": {"a & b/+"}, "nested": {`{"a":1}`}}
		if r.URL.Query().Encode() != want.Encode() {
			t.Errorf("query = %s, want %s", r.URL.RawQuery, want.Encode())
		}
		if r.ContentLength > 0 {
			t.Errorf("GET body content length %d", r.ContentLength)
		}
		_, _ = w.Write([]byte(`{"statusCode":200,"data":{}}`))
	}, func(o *Options) { o.TenantID = "tenant"; o.UserPoolID = "pool" })
	defer server.Close()
	_, err := client.SendHttpRequest("/api/v3/get-model", "GET", map[string]any{"empty": "", "off": false, "zero": 0, "phrase": "a & b/+", "nested": map[string]int{"a": 1}, "skip": nil})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentTokenCacheAndRefresh(t *testing.T) {
	var tokenCalls atomic.Int32
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			tokenResponse(w, fmt.Sprintf("token-%d", tokenCalls.Add(1)), 1)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer token-") {
			t.Errorf("missing auth")
		}
		_, _ = w.Write([]byte(`{"statusCode":200}`))
	}, nil)
	defer server.Close()
	run := func() {
		t.Helper()
		var wg sync.WaitGroup
		for range 25 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := client.SendHttpRequest("/api/v3/get-model", "GET", nil); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
	}
	run()
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("initial token calls = %d", got)
	}
	time.Sleep(950 * time.Millisecond)
	run()
	if got := tokenCalls.Load(); got != 2 {
		t.Fatalf("refreshed token calls = %d", got)
	}
}

func TestAuthingNotFoundEnvelopePreserved(t *testing.T) {
	for _, httpStatus := range []int{http.StatusNotFound, http.StatusOK} {
		t.Run(fmt.Sprint(httpStatus), func(t *testing.T) {
			client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tokenPath {
					tokenResponse(w, "token", 3600)
					return
				}
				w.WriteHeader(httpStatus)
				_, _ = w.Write([]byte(`{"statusCode":404,"message":"not found"}`))
			}, nil)
			defer server.Close()
			body, err := client.SendHttpRequest("/api/v3/get-model", "GET", nil)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != `{"statusCode":404,"message":"not found"}` {
				t.Fatalf("404 body: %s", body)
			}
		})
	}
}

func TestBadResponsesAndFailuresNeverLeakSecrets(t *testing.T) {
	cases := []struct {
		name    string
		token   bool
		status  int
		body    string
		wantErr string
	}{
		{"token rejected", true, 401, `{"statusCode":401,"message":"super-secret"}`, "token request"},
		{"token invalid JSON", true, 200, `super-secret`, "token request"},
		{"token missing data", true, 200, `{"statusCode":200}`, "token response"},
		{"token expiry overflow", true, 200, `{"statusCode":200,"data":{"access_token":"token","expires_in":9223372036854775807}}`, "token response"},
		{"token failed envelope", true, 200, `{"statusCode":401,"message":"super-secret"}`, "token request"},
		{"API 5xx", false, 503, `{"statusCode":503,"message":"super-secret"}`, "HTTP status 503"},
		{"API 4xx not 404", false, 401, `{"statusCode":401,"message":"super-secret"}`, "HTTP status 401"},
		{"API malformed 404", false, 404, `super-secret`, "invalid authing response"},
		{"API malformed success", false, 200, `super-secret`, "invalid authing response"},
		{"API missing status", false, 200, `{}`, "invalid authing response"},
		{"API status failure", false, 200, `{"statusCode":422,"message":"super-secret"}`, "API status 422"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tokenPath && !tc.token {
					tokenResponse(w, "token", 3600)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}, nil)
			defer server.Close()
			_, err := client.SendHttpRequest("/api/v3/get-model", "GET", nil)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error=%v, want %q", err, tc.wantErr)
			}
			if strings.Contains(err.Error(), "super-secret") {
				t.Fatalf("secret leaked: %v", err)
			}
		})
	}
}

func TestHostAndPathProtection(t *testing.T) {
	hosts := []string{"http://example.com", "https://user:super-secret@example.com", "https://example.com/base", "https://example.com?foo=bar", "https://example.com#frag", "//example.com", "ftp://example.com"}
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			if _, err := NewClient(Options{Host: host, AccessKeyID: "key", AccessKeySecret: "super-secret"}); err == nil || strings.Contains(err.Error(), "super-secret") {
				t.Fatalf("host %q: %v", host, err)
			}
		})
	}
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("invalid path contacted server: %s", r.URL.Path)
	}, nil)
	defer server.Close()
	for _, path := range []string{"https://evil.example/api/v3/get-model", "//evil.example/api/v3/get-model", "/api/v3/../secrets", "/api/v3/%2e%2e", "/api/v3/get-model?accessKeySecret=super-secret", "/api/v3/get-management-token", "/api/v3//get-model", "/api/v2/get-model"} {
		if _, err := client.SendHttpRequest(path, "GET", nil); err == nil {
			t.Errorf("accepted unsafe path %q", path)
		}
	}
}

func TestPostJSONAndRetryAfterTokenFailure(t *testing.T) {
	var attempts atomic.Int32
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			if attempts.Add(1) == 1 {
				w.WriteHeader(503)
				_, _ = w.Write([]byte(`{"statusCode":503}`))
				return
			}
			tokenResponse(w, "recovered", 3600)
			return
		}
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "Bearer recovered" {
			t.Errorf("POST headers: %s %v", r.Method, r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["name"] != "entry" || body["enabled"] != false || body["count"] != float64(0) {
			t.Errorf("POST payload: %v", body)
		}
		_, _ = w.Write([]byte(`{"statusCode":200}`))
	}, nil)
	defer server.Close()
	if _, err := client.SendHttpRequest("/api/v3/create-model", "POST", map[string]any{"name": "entry", "enabled": false, "count": 0}); err == nil {
		t.Fatal("token failure swallowed")
	}
	if _, err := client.SendHttpRequest("/api/v3/create-model", "POST", map[string]any{"name": "entry", "enabled": false, "count": 0}); err != nil {
		t.Fatal(err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("token attempts = %d", got)
	}
}

func TestDefaultHostAndTimeout(t *testing.T) {
	client, err := NewClient(Options{AccessKeyID: "key", AccessKeySecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if client.host != "https://api.authing.cn" || client.httpClient.Timeout <= 0 {
		t.Fatalf("insecure defaults: %q, %v", client.host, client.httpClient.Timeout)
	}
	slow, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			time.Sleep(100 * time.Millisecond)
			tokenResponse(w, "token", 3600)
		}
	}, func(o *Options) { o.Timeout = 10 * time.Millisecond })
	defer server.Close()
	if _, err := slow.SendHttpRequest("/api/v3/get-model", "GET", nil); err == nil {
		t.Fatal("request did not time out")
	}
}

func TestContextCancellationAndRedirect(t *testing.T) {
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			tokenResponse(w, "token", 3600)
			return
		}
		w.Header().Set("Location", "https://elsewhere.example/secret")
		w.WriteHeader(302)
	}, nil)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.SendHttpRequestContext(ctx, "/api/v3/get-model", "GET", nil); err != context.Canceled {
		t.Fatalf("context error: %v", err)
	}
	if _, err := client.SendHttpRequest("/api/v3/get-model", "GET", nil); err == nil || !strings.Contains(err.Error(), "redirect refused") {
		t.Fatalf("redirect error: %v", err)
	}
}
