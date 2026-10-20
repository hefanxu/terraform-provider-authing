package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func lookupRead(t *testing.T, service datasource.DataSource, values map[string]string, handler func(http.ResponseWriter, *http.Request)) datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	s := datasource.SchemaResponse{}
	service.Schema(ctx, datasource.SchemaRequest{}, &s)
	objectType := s.Schema.Type().TerraformType(ctx).(tftypes.Object)
	attributes := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, typ := range objectType.AttributeTypes {
		if value, ok := values[name]; ok {
			attributes[name] = tftypes.NewValue(typ, value)
		} else {
			attributes[name] = tftypes.NewValue(typ, nil)
		}
	}
	config := tfsdk.Config{Schema: s.Schema, Raw: tftypes.NewValue(objectType, attributes)}
	client := objectTestClient(t, handler)
	configured := datasource.ConfigureResponse{}
	service.(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{ProviderData: client}, &configured)
	if configured.Diagnostics.HasError() {
		t.Fatal(configured.Diagnostics)
	}
	result := datasource.ReadResponse{State: tfsdk.State{Schema: s.Schema}}
	service.Read(ctx, datasource.ReadRequest{Config: config}, &result)
	return result
}

func TestDeviceStatusRead(t *testing.T) {
	calls := 0
	result := lookupRead(t, NewDeviceStatusDataSource(), map[string]string{"device_id": "terminal 1"}, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/api/v3/device-status" || r.URL.RawQuery != "" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body) != 1 || body["id"] != "terminal 1" {
			t.Errorf("unexpected body: %v", body)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"status":"suspended","diffTime":12.5}}`)
	})
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	var got DeviceStatusDataSourceModel
	if d := result.State.Get(context.Background(), &got); d.HasError() {
		t.Fatal(d)
	}
	if calls != 1 || got.DeviceID.ValueString() != "terminal 1" || got.Status.ValueString() != "suspended" || got.DiffTime.IsNull() || got.DiffTime.ValueBigFloat().Text('f', 1) != "12.5" {
		t.Fatalf("unexpected state: %+v, calls=%d", got, calls)
	}
}

func TestDeviceStatusMissingOptionalDiffTimeClearsState(t *testing.T) {
	result := lookupRead(t, NewDeviceStatusDataSource(), map[string]string{"device_id": "d1"}, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"statusCode":200,"data":{"status":"activated"}}`)
	})
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	var got DeviceStatusDataSourceModel
	if d := result.State.Get(context.Background(), &got); d.HasError() {
		t.Fatal(d)
	}
	if !got.DiffTime.IsNull() || got.Status.ValueString() != "activated" {
		t.Fatalf("unexpected state: %+v", got)
	}
}

func TestDeviceStatusNullableDiffTime(t *testing.T) {
	for _, tc := range []struct {
		name, diff, want string
		isNull           bool
	}{
		{"explicit null", `null`, "", true},
		{"zero", `0`, "0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := lookupRead(t, NewDeviceStatusDataSource(), map[string]string{"device_id": "d1"}, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"statusCode":200,"data":{"status":"deactivated","diffTime":%s}}`, tc.diff)
			})
			if result.Diagnostics.HasError() {
				t.Fatal(result.Diagnostics)
			}
			var got DeviceStatusDataSourceModel
			if d := result.State.Get(context.Background(), &got); d.HasError() {
				t.Fatal(d)
			}
			if got.DiffTime.IsNull() != tc.isNull || (!tc.isNull && got.DiffTime.ValueBigFloat().Text('f', 0) != tc.want) {
				t.Fatalf("unexpected diff_time: %v", got.DiffTime)
			}
		})
	}
}

func TestApplicationSubjectAuthRead(t *testing.T) {
	calls := 0
	result := lookupRead(t, NewApplicationSubjectAuthDataSource(), map[string]string{"target_id": "subject & 1", "target_type": "USER", "app_id": "app/1"}, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v3/get-subject-auth-detail" || r.URL.Query().Encode() != "appId=app%2F1&targetId=subject+%26+1&targetType=USER" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		raw, _ := io.ReadAll(r.Body)
		if len(raw) != 0 {
			t.Errorf("GET body: %s", raw)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"appId":"app/1","appName":"Application","reqTargetId":"subject & 1","reqTargetName":"Alice","reqTargetType":"USER","targetType":"ROLE","targetName":"Admin","authType":"SUBJECT"}}`)
	})
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	var got ApplicationSubjectAuthDataSourceModel
	if d := result.State.Get(context.Background(), &got); d.HasError() {
		t.Fatal(d)
	}
	if calls != 1 || got.AppID.ValueString() != "app/1" || got.AppName.ValueString() != "Application" || got.ReqTargetID.ValueString() != "subject & 1" || got.ReqTargetName.ValueString() != "Alice" || got.ReqTargetType.ValueString() != "USER" || got.ResultTargetType.ValueString() != "ROLE" || got.TargetName.ValueString() != "Admin" || got.AuthType.ValueString() != "SUBJECT" {
		t.Fatalf("unexpected state: %+v, calls=%d", got, calls)
	}
}

func TestManagementLookupsRejectIncompleteOrFailedResponses(t *testing.T) {
	for _, tc := range []struct {
		name       string
		factory    func() datasource.DataSource
		values     map[string]string
		body       string
		httpStatus int
	}{
		{"device missing data", NewDeviceStatusDataSource, map[string]string{"device_id": "d"}, `{"statusCode":200}`, 200},
		{"device missing status", NewDeviceStatusDataSource, map[string]string{"device_id": "d"}, `{"statusCode":200,"data":{"diffTime":2}}`, 200},
		{"device invalid status", NewDeviceStatusDataSource, map[string]string{"device_id": "d"}, `{"statusCode":200,"data":{"status":"unknown"}}`, 200},
		{"device invalid diff time", NewDeviceStatusDataSource, map[string]string{"device_id": "d"}, `{"statusCode":200,"data":{"status":"suspended","diffTime":"invalid"}}`, 200},
		{"device business error", NewDeviceStatusDataSource, map[string]string{"device_id": "d"}, `{"statusCode":404,"message":"TOP_SECRET"}`, 200},
		{"device server error", NewDeviceStatusDataSource, map[string]string{"device_id": "d"}, `{"statusCode":500,"message":"TOP_SECRET"}`, 500},
		{"device malformed", NewDeviceStatusDataSource, map[string]string{"device_id": "d"}, `not json TOP_SECRET`, 200},
		{"application missing data", NewApplicationSubjectAuthDataSource, map[string]string{"target_id": "s", "target_type": "USER", "app_id": "a"}, `{"statusCode":200,"data":null}`, 200},
		{"application missing required output", NewApplicationSubjectAuthDataSource, map[string]string{"target_id": "s", "target_type": "USER", "app_id": "a"}, `{"statusCode":200,"data":{"appId":"a"}}`, 200},
		{"application business error", NewApplicationSubjectAuthDataSource, map[string]string{"target_id": "s", "target_type": "USER", "app_id": "a"}, `{"statusCode":403,"message":"TOP_SECRET"}`, 200},
		{"application server error", NewApplicationSubjectAuthDataSource, map[string]string{"target_id": "s", "target_type": "USER", "app_id": "a"}, `{"statusCode":500,"message":"TOP_SECRET"}`, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := lookupRead(t, tc.factory(), tc.values, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.httpStatus); fmt.Fprint(w, tc.body) })
			if !result.Diagnostics.HasError() || strings.Contains(fmt.Sprint(result.Diagnostics), "TOP_SECRET") || !result.State.Raw.IsNull() {
				t.Fatalf("expected safe error and no state: %v state=%v", result.Diagnostics, result.State.Raw)
			}
		})
	}
}

func TestManagementLookupsRejectEmptyInputsWithoutNetwork(t *testing.T) {
	for _, tc := range []struct {
		name    string
		factory func() datasource.DataSource
		values  map[string]string
	}{
		{"device", NewDeviceStatusDataSource, map[string]string{"device_id": ""}},
		{"subject", NewApplicationSubjectAuthDataSource, map[string]string{"target_id": "", "target_type": "USER", "app_id": "a"}},
		{"app", NewApplicationSubjectAuthDataSource, map[string]string{"target_id": "s", "target_type": "USER", "app_id": ""}},
		{"type", NewApplicationSubjectAuthDataSource, map[string]string{"target_id": "s", "target_type": "invalid", "app_id": "a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			result := lookupRead(t, tc.factory(), tc.values, func(w http.ResponseWriter, r *http.Request) { calls++ })
			if !result.Diagnostics.HasError() || calls != 0 {
				t.Fatalf("diagnostics=%v calls=%d", result.Diagnostics, calls)
			}
		})
	}
}

func TestManagementLookupRegistration(t *testing.T) {
	names := map[string]bool{}
	for _, factory := range (&AuthingProvider{}).DataSources(context.Background()) {
		d := factory()
		m := datasource.MetadataResponse{}
		d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "authing"}, &m)
		names[m.TypeName] = true
	}
	for _, name := range []string{"authing_device_status", "authing_application_subject_auth"} {
		if !names[name] {
			t.Errorf("missing %s", name)
		}
	}
}
