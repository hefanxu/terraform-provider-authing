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

func TestDeviceExclusiveRuleSettingsRead(t *testing.T) {
	result := lookupRead(t, NewDeviceExclusiveRuleSettingsDataSource(), nil, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v3/get-device-exclusive-rule-settings" || r.URL.RawQuery != "" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		if body, _ := io.ReadAll(r.Body); len(body) != 0 {
			t.Errorf("GET body: %q", body)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"rule":"condition:device","deviceRule":{"maxOnlineDevices":2.5},"ipRule":{"maxOnlineIPs":3},"secret":"NOT_FOR_STATE"}}`)
	})
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	var state DeviceExclusiveRuleSettingsDataSourceModel
	if d := result.State.Get(context.Background(), &state); d.HasError() {
		t.Fatal(d)
	}
	if state.Rule.ValueString() != "condition:device" || state.MaxOnlineDevices.IsNull() || state.MaxOnlineDevices.ValueBigFloat().Text('f', 1) != "2.5" || state.MaxOnlineIPs.ValueBigFloat().Text('f', 0) != "3" {
		t.Fatalf("state: %+v", state)
	}
	if strings.Contains(fmt.Sprint(result.State.Raw), "NOT_FOR_STATE") {
		t.Fatal("unselected data persisted")
	}
}

func TestDeviceExclusiveDisabledRuleMayOmitLimits(t *testing.T) {
	result := lookupRead(t, NewDeviceExclusiveRuleSettingsDataSource(), nil, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"statusCode":200,"data":{"rule":"disable"}}`)
	})
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	var state DeviceExclusiveRuleSettingsDataSourceModel
	if d := result.State.Get(context.Background(), &state); d.HasError() {
		t.Fatal(d)
	}
	if state.Rule.ValueString() != "disable" || !state.MaxOnlineDevices.IsNull() || !state.MaxOnlineIPs.IsNull() {
		t.Fatalf("state: %+v", state)
	}
}

func TestDeviceExclusiveScopeSettingsRead(t *testing.T) {
	result := lookupRead(t, NewDeviceExclusiveValidScopeSettingsDataSource(), nil, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v3/get-device-exclusive-valid-scope-settings" || r.URL.RawQuery != "" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		if body, _ := io.ReadAll(r.Body); len(body) != 0 {
			t.Errorf("GET body: %q", body)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":[{"appId":"app-1","createdAt":"2022-07-03T02:20:30.000Z","appName":"NOT_FOR_STATE","appLogo":"NOT_FOR_STATE"},{"appId":"app-2","createdAt":"2022-07-03T02:20:30.000Z","isDefault":true}]}`)
	})
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	var state DeviceExclusiveValidScopeSettingsDataSourceModel
	if d := result.State.Get(context.Background(), &state); d.HasError() {
		t.Fatal(d)
	}
	var ids []string
	if d := state.AppIDs.ElementsAs(context.Background(), &ids, false); d.HasError() {
		t.Fatal(d)
	}
	if len(ids) != 2 || ids[0] != "app-1" || ids[1] != "app-2" {
		t.Fatalf("ids=%v", ids)
	}
	if strings.Contains(fmt.Sprint(result.State.Raw), "NOT_FOR_STATE") {
		t.Fatal("unselected data persisted")
	}
}

func TestDeviceExclusiveEmptyScope(t *testing.T) {
	result := lookupRead(t, NewDeviceExclusiveValidScopeSettingsDataSource(), nil, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"statusCode":200,"data":[]}`) })
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	var state DeviceExclusiveValidScopeSettingsDataSourceModel
	if d := result.State.Get(context.Background(), &state); d.HasError() {
		t.Fatal(d)
	}
	if state.AppIDs.IsNull() || len(state.AppIDs.Elements()) != 0 {
		t.Fatalf("expected empty nonnull list: %v", state.AppIDs)
	}
}

func TestDeviceExclusiveSettingsRejectInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		factory func() datasource.DataSource
		body    string
		code    int
	}{
		{"rule missing data", NewDeviceExclusiveRuleSettingsDataSource, `{"statusCode":200}`, 200},
		{"rule missing discriminator", NewDeviceExclusiveRuleSettingsDataSource, `{"statusCode":200,"data":{}}`, 200},
		{"rule null", NewDeviceExclusiveRuleSettingsDataSource, `{"statusCode":200,"data":{"rule":null}}`, 200},
		{"rule unknown", NewDeviceExclusiveRuleSettingsDataSource, `{"statusCode":200,"data":{"rule":"TOP_SECRET"}}`, 200},
		{"rule missing active limit", NewDeviceExclusiveRuleSettingsDataSource, `{"statusCode":200,"data":{"rule":"condition:ip"}}`, 200},
		{"rule malformed inactive limit", NewDeviceExclusiveRuleSettingsDataSource, `{"statusCode":200,"data":{"rule":"disable","deviceRule":{"maxOnlineDevices":"TOP_SECRET"}}}`, 200},
		{"rule null nested", NewDeviceExclusiveRuleSettingsDataSource, `{"statusCode":200,"data":{"rule":"condition:device","deviceRule":null}}`, 200},
		{"rule business error", NewDeviceExclusiveRuleSettingsDataSource, `{"statusCode":403,"message":"TOP_SECRET","data":{"rule":"disable"}}`, 200},
		{"rule transport error", NewDeviceExclusiveRuleSettingsDataSource, `{"statusCode":500,"message":"TOP_SECRET"}`, 500},
		{"scope missing", NewDeviceExclusiveValidScopeSettingsDataSource, `{"statusCode":200}`, 200},
		{"scope null", NewDeviceExclusiveValidScopeSettingsDataSource, `{"statusCode":200,"data":null}`, 200},
		{"scope object", NewDeviceExclusiveValidScopeSettingsDataSource, `{"statusCode":200,"data":{}}`, 200},
		{"scope missing id", NewDeviceExclusiveValidScopeSettingsDataSource, `{"statusCode":200,"data":[{"createdAt":"2022-07-03T02:20:30Z"}]}`, 200},
		{"scope missing timestamp", NewDeviceExclusiveValidScopeSettingsDataSource, `{"statusCode":200,"data":[{"appId":"a"}]}`, 200},
		{"scope malformed timestamp", NewDeviceExclusiveValidScopeSettingsDataSource, `{"statusCode":200,"data":[{"appId":"a","createdAt":"TOP_SECRET"}]}`, 200},
		{"scope duplicate id", NewDeviceExclusiveValidScopeSettingsDataSource, `{"statusCode":200,"data":[{"appId":"a","createdAt":"2022-07-03T02:20:30Z"},{"appId":"a","createdAt":"2022-07-03T02:20:30Z"}]}`, 200},
		{"scope bad item", NewDeviceExclusiveValidScopeSettingsDataSource, `{"statusCode":200,"data":[null]}`, 200},
		{"scope business error", NewDeviceExclusiveValidScopeSettingsDataSource, `{"statusCode":403,"message":"TOP_SECRET","data":[]}`, 200},
		{"scope invalid JSON", NewDeviceExclusiveValidScopeSettingsDataSource, `TOP_SECRET`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := lookupRead(t, tc.factory(), nil, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.code); fmt.Fprint(w, tc.body) })
			if !result.Diagnostics.HasError() || !result.State.Raw.IsNull() || strings.Contains(fmt.Sprint(result.Diagnostics), "TOP_SECRET") {
				t.Fatalf("expected safe error and null state: %v state=%v", result.Diagnostics, result.State.Raw)
			}
		})
	}
}

func TestDeviceExclusiveSettingsRegisteredDataSourcesOnly(t *testing.T) {
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
	for _, name := range []string{"authing_device_exclusive_rule_settings", "authing_device_exclusive_valid_scope_settings"} {
		if !names[name] || resources[name] {
			t.Errorf("%s must be data source only", name)
		}
	}
}
