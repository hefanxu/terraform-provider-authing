package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestTenantUserLookupExactIdentityAndAllowlistedState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input map[string]string
		query string
	}{
		{"linked user", map[string]string{"tenant_id": "tenant & 1", "link_user_id": "user/1"}, "linkUserId=user%2F1&tenantId=tenant+%26+1"},
		{"member", map[string]string{"tenant_id": "tenant & 1", "member_id": "member/1"}, "memberId=member%2F1&tenantId=tenant+%26+1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			result := lookupRead(t, NewTenantUserDataSource(), tc.input, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/api/v3/get-tenant-user" || r.URL.Query().Encode() != tc.query {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				body, _ := io.ReadAll(r.Body)
				if len(body) != 0 {
					t.Errorf("GET body: %q", body)
				}
				fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"tenant & 1","memberId":"member/1","linkUserId":"user/1","isTenantAdmin":false,"username":"alice","name":"Alice","nickname":"Al","photo":"https://example.test/avatar","password":"PASSWORD_MARKER","salt":"SALT_MARKER"}}`)
			})
			if result.Diagnostics.HasError() {
				t.Fatal(result.Diagnostics)
			}
			var got TenantUserDataSourceModel
			if d := result.State.Get(context.Background(), &got); d.HasError() {
				t.Fatal(d)
			}
			if calls != 1 || got.TenantID.ValueString() != "tenant & 1" || got.MemberID.ValueString() != "member/1" || got.LinkUserID.ValueString() != "user/1" || got.IsTenantAdmin.IsNull() || got.IsTenantAdmin.ValueBool() || got.Username.ValueString() != "alice" || got.Name.ValueString() != "Alice" || got.Nickname.ValueString() != "Al" || got.Photo.ValueString() != "https://example.test/avatar" {
				t.Fatalf("unexpected state: %+v calls=%d", got, calls)
			}
			state := fmt.Sprint(result.State.Raw)
			if strings.Contains(state, "PASSWORD_MARKER") || strings.Contains(state, "SALT_MARKER") || strings.Contains(fmt.Sprint(result.Diagnostics), "PASSWORD_MARKER") {
				t.Fatal("secret leaked")
			}
		})
	}
}

func TestTenantUserLookupRejectsInvalidInputsWithoutNetwork(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input map[string]string
	}{
		{"empty tenant", map[string]string{"tenant_id": "", "member_id": "m"}},
		{"missing identifier", map[string]string{"tenant_id": "t"}},
		{"both identifiers", map[string]string{"tenant_id": "t", "member_id": "m", "link_user_id": "u"}},
		{"empty identifier", map[string]string{"tenant_id": "t", "member_id": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			result := lookupRead(t, NewTenantUserDataSource(), tc.input, func(http.ResponseWriter, *http.Request) { calls++ })
			if !result.Diagnostics.HasError() || calls != 0 || !result.State.Raw.IsNull() {
				t.Fatalf("diagnostics=%v calls=%d state=%v", result.Diagnostics, calls, result.State.Raw)
			}
		})
	}
}

func TestTenantUserLookupRejectsUnsafeResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"wrong tenant", `{"statusCode":200,"data":{"tenantId":"other","memberId":"m","linkUserId":"u","isTenantAdmin":false,"password":"PASSWORD_MARKER"}}`, 200},
		{"wrong member", `{"statusCode":200,"data":{"tenantId":"t","memberId":"other","linkUserId":"u","isTenantAdmin":false,"salt":"SALT_MARKER"}}`, 200},
		{"missing linked identity", `{"statusCode":200,"data":{"tenantId":"t","memberId":"m","isTenantAdmin":false}}`, 200},
		{"missing flag", `{"statusCode":200,"data":{"tenantId":"t","memberId":"m","linkUserId":"u"}}`, 200},
		{"null", `{"statusCode":200,"data":null}`, 200},
		{"missing data", `{"statusCode":200}`, 200},
		{"business 404", `{"statusCode":404,"message":"PASSWORD_MARKER"}`, 200},
		{"HTTP 404", `{"statusCode":404,"message":"PASSWORD_MARKER"}`, 404},
		{"business 500", `{"statusCode":500,"message":"SALT_MARKER"}`, 200},
		{"HTTP 500", `{"statusCode":500,"message":"SALT_MARKER"}`, 500},
		{"malformed", `not json PASSWORD_MARKER`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := lookupRead(t, NewTenantUserDataSource(), map[string]string{"tenant_id": "t", "member_id": "m"}, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) })
			if !result.Diagnostics.HasError() || !result.State.Raw.IsNull() || strings.Contains(fmt.Sprint(result.Diagnostics), "PASSWORD_MARKER") || strings.Contains(fmt.Sprint(result.Diagnostics), "SALT_MARKER") {
				t.Fatalf("unsafe response: %v state=%v", result.Diagnostics, result.State.Raw)
			}
		})
	}
}

func TestTenantUserLookupRegisteredReadOnly(t *testing.T) {
	found := false
	for _, factory := range (&AuthingProvider{}).DataSources(context.Background()) {
		d := factory()
		m := datasource.MetadataResponse{}
		d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "authing"}, &m)
		if m.TypeName == "authing_tenant_user" {
			found = true
		}
	}
	if !found {
		t.Fatal("tenant user data source not registered")
	}
}
