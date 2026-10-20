package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestTenantCustomFieldLookupIsScopedAndSelectsExactKey(t *testing.T) {
	calls := 0
	result := lookupRead(t, NewTenantCustomFieldDataSource(), map[string]string{"tenant_id": "tenant & one", "target_type": "USER", "key": "school/level"}, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v3/get-custom-fields" || r.URL.Query().Encode() != "targetType=USER&tenantId=tenant+%26+one" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":[{"targetType":"USER","key":"other","dataType":"STRING","label":"Other","isUnique":false,"visibleInAdminConsole":true},{"targetType":"USER","key":"school/level","dataType":"NUMBER","label":"School level","description":"Grade","isUnique":false,"userEditable":false,"visibleInAdminConsole":false,"visibleInUserCenter":true}]}`)
	})
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	var got TenantCustomFieldDataSourceModel
	if d := result.State.Get(context.Background(), &got); d.HasError() {
		t.Fatal(d)
	}
	if calls != 1 || got.TenantID.ValueString() != "tenant & one" || got.Key.ValueString() != "school/level" || got.DataType.ValueString() != "NUMBER" || got.Label.ValueString() != "School level" || got.Description.ValueString() != "Grade" || got.UserEditable.IsNull() || got.UserEditable.ValueBool() || got.VisibleInAdminConsole.ValueBool() || !got.VisibleInUserCenter.ValueBool() {
		t.Fatalf("unexpected state: %+v calls=%d", got, calls)
	}
}

func TestTenantCustomFieldLookupRejectsMissingAndFailedResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"absent field", `{"statusCode":200,"data":[]}`, 200},
		{"different target", `{"statusCode":200,"data":[{"targetType":"ROLE","key":"school","dataType":"STRING","label":"Wrong","isUnique":false,"visibleInAdminConsole":true}]}`, 200},
		{"business 404", `{"statusCode":404,"message":"PRIVATE"}`, 200},
		{"transport 404", `{"statusCode":404,"message":"PRIVATE"}`, 404},
		{"transport failure", `upstream PRIVATE`, 502},
		{"server error", `{"statusCode":500,"message":"PRIVATE"}`, 500},
		{"malformed", `invalid PRIVATE`, 200},
		{"missing data", `{"statusCode":200}`, 200},
		{"missing identity", `{"statusCode":200,"data":[{"key":"school","dataType":"STRING","label":"Wrong"}]}`, 200},
		{"missing required label", `{"statusCode":200,"data":[{"targetType":"USER","key":"school","dataType":"STRING","isUnique":false,"visibleInAdminConsole":true}]}`, 200},
		{"encrypted", `{"statusCode":200,"data":[{"targetType":"USER","key":"school","dataType":"STRING","label":"Secret","isUnique":false,"visibleInAdminConsole":true,"encrypted":true}]}`, 200},
		{"duplicate identity", `{"statusCode":200,"data":[{"targetType":"USER","key":"school","dataType":"STRING","label":"First","isUnique":false,"visibleInAdminConsole":true},{"targetType":"USER","key":"school","dataType":"STRING","label":"Second","isUnique":false,"visibleInAdminConsole":true}]}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := lookupRead(t, NewTenantCustomFieldDataSource(), map[string]string{"tenant_id": "tenant", "target_type": "USER", "key": "school"}, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) })
			if !result.Diagnostics.HasError() || strings.Contains(fmt.Sprint(result.Diagnostics), "PRIVATE") || !result.State.Raw.IsNull() {
				t.Fatalf("expected safe failure: %v state=%v", result.Diagnostics, result.State.Raw)
			}
		})
	}
}

func TestTenantCustomFieldLookupRejectsInvalidScopeWithoutNetwork(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values map[string]string
	}{
		{"tenant empty", map[string]string{"tenant_id": "", "target_type": "USER", "key": "school"}},
		{"group unsupported", map[string]string{"tenant_id": "tenant", "target_type": "GROUP", "key": "school"}},
		{"type invalid", map[string]string{"tenant_id": "tenant", "target_type": "other", "key": "school"}},
		{"key empty", map[string]string{"tenant_id": "tenant", "target_type": "USER", "key": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			result := lookupRead(t, NewTenantCustomFieldDataSource(), tc.values, func(http.ResponseWriter, *http.Request) { calls++ })
			if !result.Diagnostics.HasError() || calls != 0 {
				t.Fatalf("diagnostics=%v calls=%d", result.Diagnostics, calls)
			}
		})
	}
}

func TestTenantCustomFieldLookupRegisteredReadOnly(t *testing.T) {
	found := false
	for _, factory := range (&AuthingProvider{}).DataSources(context.Background()) {
		d := factory()
		m := datasource.MetadataResponse{}
		d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "authing"}, &m)
		if m.TypeName == "authing_tenant_custom_field" {
			found = true
		}
	}
	if !found {
		t.Fatal("tenant custom field data source not registered")
	}
}
