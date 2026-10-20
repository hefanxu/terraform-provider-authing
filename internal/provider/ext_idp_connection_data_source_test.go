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

func TestExtIdpConnectionLookup(t *testing.T) {
	calls := 0
	result := lookupRead(t, NewExtIdpConnectionDataSource(), map[string]string{"ext_idp_id": "parent &/1", "connection_id": "conn-2"}, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v3/list-ext-idp-conns" || r.URL.Query().Encode() != "id=parent+%26%2F1" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Errorf("GET body: %q", body)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":[{"id":"conn-1","extIdpId":"parent &/1","fields":{"client_secret":"SECRET_FROM_OTHER"}},{"id":"conn-2","extIdpId":"parent &/1","type":"oidc","identifier":"ok","displayName":"Login","logo":"https://example.com/logo","loginOnly":false,"fields":{"client_secret":"SECRET_FROM_MATCH"}}]}`)
	})
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	var got ExtIdpConnectionDataSourceModel
	if d := result.State.Get(context.Background(), &got); d.HasError() {
		t.Fatal(d)
	}
	if calls != 1 || got.ExtIdpID.ValueString() != "parent &/1" || got.ConnectionID.ValueString() != "conn-2" || got.Type.ValueString() != "oidc" || got.Identifier.ValueString() != "ok" || got.DisplayName.ValueString() != "Login" || got.Logo.ValueString() != "https://example.com/logo" || got.LoginOnly.IsNull() || got.LoginOnly.ValueBool() {
		t.Fatalf("incorrect lookup: %+v calls=%d", got, calls)
	}
	for _, secret := range []string{"SECRET_FROM_MATCH", "SECRET_FROM_OTHER", "client_secret"} {
		if strings.Contains(fmt.Sprint(result.State.Raw, result.Diagnostics), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
}

func TestExtIdpConnectionLookupRejectsUnmatchedAndIncomplete(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"missing connection", `{"statusCode":200,"data":[{"id":"different","extIdpId":"parent"}]}`},
		{"wrong parent", `{"statusCode":200,"data":[{"id":"conn","extIdpId":"other","fields":{"client_secret":"TOP_SECRET"}}]}`},
		{"missing parent", `{"statusCode":200,"data":[{"id":"conn","fields":{"client_secret":"TOP_SECRET"}}]}`},
		{"duplicate id", `{"statusCode":200,"data":[{"id":"conn","extIdpId":"parent"},{"id":"conn","extIdpId":"parent"}]}`},
		{"missing data", `{"statusCode":200}`},
		{"object data", `{"statusCode":200,"data":{"id":"conn","extIdpId":"parent"}}`},
		{"business error", `{"statusCode":403,"message":"TOP_SECRET"}`},
		{"malformed", `not json TOP_SECRET`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := lookupRead(t, NewExtIdpConnectionDataSource(), map[string]string{"ext_idp_id": "parent", "connection_id": "conn"}, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) })
			if !result.Diagnostics.HasError() || !result.State.Raw.IsNull() || strings.Contains(fmt.Sprint(result.Diagnostics), "TOP_SECRET") {
				t.Fatalf("unsafe result: %v %v", result.Diagnostics, result.State.Raw)
			}
		})
	}
}

func TestExtIdpConnectionLookupRejectsHTTPErrorWithoutLeakingBody(t *testing.T) {
	result := lookupRead(t, NewExtIdpConnectionDataSource(), map[string]string{"ext_idp_id": "parent", "connection_id": "conn"}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"statusCode":500,"message":"TOP_SECRET"}`)
	})
	if !result.Diagnostics.HasError() || !result.State.Raw.IsNull() || strings.Contains(fmt.Sprint(result.Diagnostics), "TOP_SECRET") {
		t.Fatalf("unsafe result: %v %v", result.Diagnostics, result.State.Raw)
	}
}

func TestExtIdpConnectionLookupRejectsEmptyIDs(t *testing.T) {
	for _, values := range []map[string]string{{"ext_idp_id": "", "connection_id": "conn"}, {"ext_idp_id": "parent", "connection_id": ""}} {
		calls := 0
		result := lookupRead(t, NewExtIdpConnectionDataSource(), values, func(w http.ResponseWriter, r *http.Request) { calls++ })
		if !result.Diagnostics.HasError() || calls != 0 {
			t.Fatalf("expected validation before network, calls=%d diags=%v", calls, result.Diagnostics)
		}
	}
}

func TestExtIdpConnectionLookupRegistration(t *testing.T) {
	for _, factory := range (&AuthingProvider{}).DataSources(context.Background()) {
		m := datasource.MetadataResponse{}
		factory().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "authing"}, &m)
		if m.TypeName == "authing_ext_idp_connection" {
			return
		}
	}
	t.Fatal("missing authing_ext_idp_connection")
}
