package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestGlobalSecuritySettingsRead(t *testing.T) {
	calls := 0
	result := lookupRead(t, NewGlobalSecuritySettingsDataSource(), nil, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v3/get-security-settings" || r.URL.RawQuery != "" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		if b, _ := io.ReadAll(r.Body); len(b) != 0 {
			t.Errorf("unexpected GET body: %q", b)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"registerDisabled":false,"loginRequireEmailVerified":true,"allowedOrigins":"NOT_FOR_STATE","cookieSettings":{"secret":"NOT_FOR_STATE"}}}`)
	})
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	var state GlobalSecuritySettingsDataSourceModel
	if d := result.State.Get(context.Background(), &state); d.HasError() {
		t.Fatal(d)
	}
	if calls != 1 || state.RegisterDisabled.IsNull() || state.RegisterDisabled.ValueBool() || !state.LoginRequireEmailVerified.ValueBool() {
		t.Fatalf("unexpected state: %+v calls=%d", state, calls)
	}
	if strings.Contains(fmt.Sprint(result.State.Raw), "NOT_FOR_STATE") {
		t.Fatal("unexpected fields persisted")
	}
}

func TestGlobalMFASettingsRead(t *testing.T) {
	calls := 0
	result := lookupRead(t, NewGlobalMFASettingsDataSource(), nil, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v3/get-global-mfa-settings" || r.URL.RawQuery != "" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		if b, _ := io.ReadAll(r.Body); len(b) != 0 {
			t.Errorf("unexpected GET body: %q", b)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"enabledFactors":["OTP","SMS"],"privateKey":"NOT_FOR_STATE"}}`)
	})
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	var state GlobalMFASettingsDataSourceModel
	if d := result.State.Get(context.Background(), &state); d.HasError() {
		t.Fatal(d)
	}
	if calls != 1 || state.EnabledFactors.IsNull() {
		t.Fatalf("unexpected state: %+v calls=%d", state, calls)
	}
	var factors []string
	if d := state.EnabledFactors.ElementsAs(context.Background(), &factors, false); d.HasError() {
		t.Fatal(d)
	}
	if len(factors) != 2 || factors[0] != "OTP" || factors[1] != "SMS" {
		t.Fatalf("factors=%v", factors)
	}
	if strings.Contains(fmt.Sprint(result.State.Raw), "NOT_FOR_STATE") {
		t.Fatal("unexpected fields persisted")
	}
}

func TestGlobalMFASettingsEmptyFactors(t *testing.T) {
	result := lookupRead(t, NewGlobalMFASettingsDataSource(), nil, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"statusCode":200,"data":{"enabledFactors":[]}}`)
	})
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	var state GlobalMFASettingsDataSourceModel
	if d := result.State.Get(context.Background(), &state); d.HasError() {
		t.Fatal(d)
	}
	if state.EnabledFactors.IsNull() || len(state.EnabledFactors.Elements()) != 0 {
		t.Fatalf("expected non-null empty list: %v", state.EnabledFactors)
	}
}

func TestGlobalSettingsRejectFailuresAndIncompleteResponses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		factory func() datasource.DataSource
		body    string
		code    int
	}{
		{"security missing data", NewGlobalSecuritySettingsDataSource, `{"statusCode":200}`, 200},
		{"security missing flag", NewGlobalSecuritySettingsDataSource, `{"statusCode":200,"data":{"registerDisabled":false}}`, 200},
		{"security null flag", NewGlobalSecuritySettingsDataSource, `{"statusCode":200,"data":{"registerDisabled":false,"loginRequireEmailVerified":null}}`, 200},
		{"security spaced null flag", NewGlobalSecuritySettingsDataSource, `{"statusCode":200,"data":{"registerDisabled":false,"loginRequireEmailVerified": null }}`, 200},
		{"security wrong type", NewGlobalSecuritySettingsDataSource, `{"statusCode":200,"data":{"registerDisabled":"false","loginRequireEmailVerified":true}}`, 200},
		{"security business error", NewGlobalSecuritySettingsDataSource, `{"statusCode":403,"message":"TOP_SECRET"}`, 200},
		{"security HTTP error", NewGlobalSecuritySettingsDataSource, `{"statusCode":500,"message":"TOP_SECRET"}`, 500},
		{"mfa missing data", NewGlobalMFASettingsDataSource, `{"statusCode":200,"data":null}`, 200},
		{"mfa missing factors", NewGlobalMFASettingsDataSource, `{"statusCode":200,"data":{}}`, 200},
		{"mfa null factors", NewGlobalMFASettingsDataSource, `{"statusCode":200,"data":{"enabledFactors":null}}`, 200},
		{"mfa unknown factor", NewGlobalMFASettingsDataSource, `{"statusCode":200,"data":{"enabledFactors":["SECRET"]}}`, 200},
		{"mfa wrong type", NewGlobalMFASettingsDataSource, `{"statusCode":200,"data":{"enabledFactors":"SMS"}}`, 200},
		{"mfa business error", NewGlobalMFASettingsDataSource, `{"statusCode":404,"message":"TOP_SECRET"}`, 200},
		{"mfa malformed", NewGlobalMFASettingsDataSource, `not json TOP_SECRET`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := lookupRead(t, tc.factory(), nil, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.code); fmt.Fprint(w, tc.body) })
			if !result.Diagnostics.HasError() || !result.State.Raw.IsNull() || strings.Contains(fmt.Sprint(result.Diagnostics), "TOP_SECRET") {
				t.Fatalf("expected safe error and no state: %v state=%v", result.Diagnostics, result.State.Raw)
			}
		})
	}
}

func TestGlobalSettingsRegisteredAsDataSourcesOnly(t *testing.T) {
	names := map[string]bool{}
	for _, factory := range (&AuthingProvider{}).DataSources(context.Background()) {
		meta := datasource.MetadataResponse{}
		factory().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "authing"}, &meta)
		names[meta.TypeName] = true
	}
	resources := map[string]bool{}
	for _, factory := range (&AuthingProvider{}).Resources(context.Background()) {
		meta := resource.MetadataResponse{}
		factory().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "authing"}, &meta)
		resources[meta.TypeName] = true
	}
	for _, name := range []string{"authing_global_security_settings", "authing_global_mfa_settings"} {
		if !names[name] || resources[name] {
			t.Errorf("%s must be a data source only", name)
		}
	}
}
