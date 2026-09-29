// Package authingapi provides a TLS-verified Management API transport. SDK DTOs
// may be used by callers, but the SDK's HTTP transport is never used here.
package authingapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const defaultHost = "https://api.authing.cn"
const tokenPath = "/api/v3/get-management-token"
const maxResponseBytes = 8 << 20

// Options configures a Management API client. HTTPClient can supply trusted
// roots for a private installation. The default client verifies TLS certificates;
// callers supplying HTTPClient must likewise keep certificate verification on.
type Options struct {
	AccessKeyID     string
	AccessKeySecret string
	Host            string
	TenantID        string
	UserPoolID      string
	HTTPClient      *http.Client
	Timeout         time.Duration
}

// Client holds credentials and a per-client, concurrency-safe token cache.
type Client struct {
	host           string
	keyID          string
	secret         string
	tenantID       string
	userPoolID     string
	configuredPool bool
	httpClient     *http.Client
	mu             sync.Mutex
	token          string
	tokenPoolID    string
	tokenExpires   time.Time
}

// NewClient validates the host without performing network I/O. Plain HTTP is
// permitted only for loopback development servers; production hosts need HTTPS.
func NewClient(o Options) (*Client, error) {
	if o.AccessKeyID == "" || o.AccessKeySecret == "" {
		return nil, errors.New("authing credentials are required")
	}
	host := o.Host
	if host == "" {
		host = defaultHost
	}
	u, err := url.Parse(host)
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.HasSuffix(u.Host, ":") {
		return nil, errors.New("invalid authing host URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return nil, errors.New("authing host requires HTTPS (except loopback)")
	}
	if strings.ContainsAny(u.Host, "\\\r\n") {
		return nil, errors.New("invalid authing host URL")
	}
	hc := &http.Client{}
	if o.HTTPClient != nil {
		*hc = *o.HTTPClient
	}
	if o.Timeout > 0 {
		hc.Timeout = o.Timeout
	} else if hc.Timeout == 0 {
		hc.Timeout = 10 * time.Second
	}
	// Never send credentials or bearer tokens to a redirect target.
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	pool := o.UserPoolID
	if pool == "" {
		pool = o.AccessKeyID
	}
	return &Client{host: strings.TrimSuffix(u.String(), "/"), keyID: o.AccessKeyID, secret: o.AccessKeySecret, tenantID: o.TenantID, userPoolID: pool, configuredPool: o.UserPoolID != "", httpClient: hc}, nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// SendHttpRequest sends an authenticated request and returns the JSON response.
// A valid 404 Authing envelope is returned intact for callers' not-found logic.
func (c *Client) SendHttpRequest(path, method string, req any) ([]byte, error) {
	return c.SendHttpRequestContext(context.Background(), path, method, req)
}

// SendHttpRequestContext is the cancellable counterpart of SendHttpRequest.
func (c *Client) SendHttpRequestContext(ctx context.Context, path, method string, req any) ([]byte, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}
	if path == tokenPath {
		return nil, errors.New("token endpoint is reserved")
	}
	if method != http.MethodGet && method != http.MethodPost && method != http.MethodPut && method != http.MethodPatch && method != http.MethodDelete {
		return nil, errors.New("unsupported HTTP method")
	}
	token, pool, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}
	return c.send(ctx, path, method, req, token, pool)
}

func validatePath(path string) error {
	if !strings.HasPrefix(path, "/api/v3/") || strings.ContainsAny(path, "?#%\\\r\n") || strings.Contains(path, "//") {
		return errors.New("invalid authing API path")
	}
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return errors.New("invalid authing API path")
		}
	}
	return nil
}

func (c *Client) getToken(ctx context.Context) (string, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExpires) {
		return c.token, c.tokenPoolID, nil
	}
	body, err := c.send(ctx, tokenPath, http.MethodPost, map[string]string{"accessKeyId": c.keyID, "accessKeySecret": c.secret}, "", "")
	if err != nil {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		return "", "", fmt.Errorf("authing token request: %w", err)
	}
	var response struct {
		StatusCode int `json:"statusCode"`
		Data       struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int64  `json:"expires_in"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil || response.StatusCode != http.StatusOK || response.Data.AccessToken == "" || response.Data.ExpiresIn <= 0 || response.Data.ExpiresIn > math.MaxInt64/int64(time.Second) {
		return "", "", errors.New("invalid authing token response")
	}
	lifetime := time.Duration(response.Data.ExpiresIn) * time.Second
	// Keep a small safety margin while allowing short-lived tokens to be reused.
	margin := lifetime / 10
	if margin > 30*time.Second {
		margin = 30 * time.Second
	}
	c.token, c.tokenExpires = response.Data.AccessToken, time.Now().Add(lifetime-margin)
	c.tokenPoolID = c.userPoolID
	if !c.configuredPool {
		if scoped := scopedPoolID(c.token); scoped != "" {
			c.tokenPoolID = scoped
		}
	}
	return c.token, c.tokenPoolID, nil
}

// The token comes from Authing over verified TLS. Decode only the documented
// scoped_userpool_id for request routing; this does not authenticate the JWT.
func scopedPoolID(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(parts[1]) > 8192 {
		return ""
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		ScopedUserPoolID string `json:"scoped_userpool_id"`
	}
	if json.Unmarshal(data, &claims) != nil {
		return ""
	}
	return claims.ScopedUserPoolID
}

func (c *Client) send(ctx context.Context, path, method string, value any, token, poolID string) ([]byte, error) {
	endpoint := c.host + path
	var body io.Reader
	if method == http.MethodGet {
		if value != nil {
			raw, err := json.Marshal(value)
			if err != nil {
				return nil, errors.New("invalid authing query payload")
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil {
				return nil, errors.New("authing query payload must be an object")
			}
			params := url.Values{}
			for key, v := range fields {
				if string(v) == "null" {
					continue
				}
				var text string
				if len(v) > 0 && v[0] == '"' {
					if json.Unmarshal(v, &text) != nil {
						return nil, errors.New("invalid authing query payload")
					}
				} else {
					text = string(v)
				}
				params.Set(key, text)
			}
			if len(params) > 0 {
				endpoint += "?" + params.Encode()
			}
		}
	} else {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, errors.New("invalid authing request payload")
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, errors.New("invalid authing request")
	}
	if method != http.MethodGet {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
		if poolID != "" {
			request.Header.Set("x-authing-userpool-id", poolID)
		}
	}
	if c.tenantID != "" {
		request.Header.Set("x-authing-app-tenant-id", c.tenantID)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("authing HTTP transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return nil, errors.New("authing HTTP redirect refused")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, errors.New("authing response read failed")
	}
	if len(data) > maxResponseBytes {
		return nil, errors.New("authing response too large")
	}
	var envelope struct {
		StatusCode *int `json:"statusCode"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.StatusCode == nil {
		return nil, errors.New("invalid authing response")
	}
	if *envelope.StatusCode == http.StatusNotFound && (response.StatusCode == http.StatusNotFound || response.StatusCode >= 200 && response.StatusCode < 300) {
		return data, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("authing HTTP status %d", response.StatusCode)
	}
	// A non-200 Authing business status is a valid API envelope, not a
	// transport failure. Keep it available for the resource's error handling.
	return data, nil
}
