package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

var _ datasource.DataSource = &DeviceExclusiveRuleSettingsDataSource{}
var _ datasource.DataSourceWithConfigure = &DeviceExclusiveRuleSettingsDataSource{}
var _ datasource.DataSource = &DeviceExclusiveValidScopeSettingsDataSource{}
var _ datasource.DataSourceWithConfigure = &DeviceExclusiveValidScopeSettingsDataSource{}

type DeviceExclusiveRuleSettingsDataSource struct{ client *authingapi.Client }
type DeviceExclusiveRuleSettingsDataSourceModel struct {
	Rule             types.String `tfsdk:"rule"`
	MaxOnlineDevices types.Number `tfsdk:"max_online_devices"`
	MaxOnlineIPs     types.Number `tfsdk:"max_online_ips"`
}

func NewDeviceExclusiveRuleSettingsDataSource() datasource.DataSource {
	return &DeviceExclusiveRuleSettingsDataSource{}
}
func (d *DeviceExclusiveRuleSettingsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_device_exclusive_rule_settings"
}
func (d *DeviceExclusiveRuleSettingsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads user-pool device-exclusive rule settings without modifying them. This endpoint provides no verified tenant identity.", Attributes: map[string]schema.Attribute{
		"rule":               schema.StringAttribute{Computed: true, Description: "Rule: disable, condition:device, or condition:ip."},
		"max_online_devices": schema.NumberAttribute{Computed: true, Description: "Device limit, or null when deviceRule is omitted."},
		"max_online_ips":     schema.NumberAttribute{Computed: true, Description: "IP limit, or null when ipRule is omitted."},
	}}
}
func (d *DeviceExclusiveRuleSettingsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureGlobalSettingsClient(req, resp)
}
func (d *DeviceExclusiveRuleSettingsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	data, ok := readLookupData(ctx, d.client, "/api/v3/get-device-exclusive-rule-settings", http.MethodGet, nil)
	if !ok {
		resp.Diagnostics.AddError("Unable to read device-exclusive rules", "Authing did not return a successful rule settings response.")
		return
	}
	var rule string
	if !decodeRequiredGlobalField(data, "rule", &rule) {
		resp.Diagnostics.AddError("Invalid device-exclusive rules", "Authing returned an invalid rule.")
		return
	}
	switch rule {
	case "disable", "condition:device", "condition:ip":
	default:
		resp.Diagnostics.AddError("Invalid device-exclusive rules", "Authing returned an unsupported rule.")
		return
	}
	devices, devicePresent, valid := readDeviceExclusiveLimit(data, "deviceRule", "maxOnlineDevices")
	if !valid || (rule == "condition:device" && !devicePresent) {
		resp.Diagnostics.AddError("Invalid device-exclusive rules", "Authing returned an invalid device limit.")
		return
	}
	ips, ipPresent, valid := readDeviceExclusiveLimit(data, "ipRule", "maxOnlineIPs")
	if !valid || (rule == "condition:ip" && !ipPresent) {
		resp.Diagnostics.AddError("Invalid device-exclusive rules", "Authing returned an invalid IP limit.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &DeviceExclusiveRuleSettingsDataSourceModel{Rule: types.StringValue(rule), MaxOnlineDevices: devices, MaxOnlineIPs: ips})...)
}

// A missing optional rule object maps to null; a present object must be complete.
func readDeviceExclusiveLimit(data map[string]json.RawMessage, object, field string) (types.Number, bool, bool) {
	raw, present := data[object]
	if !present {
		return types.NumberNull(), false, true
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return types.NumberNull(), true, false
	}
	var number json.Number
	if !decodeRequiredGlobalField(values, field, &number) {
		return types.NumberNull(), true, false
	}
	value, _, err := big.ParseFloat(number.String(), 10, 256, big.ToNearestEven)
	if err != nil || value.IsInf() {
		return types.NumberNull(), true, false
	}
	return types.NumberValue(value), true, true
}

type DeviceExclusiveValidScopeSettingsDataSource struct{ client *authingapi.Client }
type DeviceExclusiveValidScopeSettingsDataSourceModel struct {
	AppIDs types.List `tfsdk:"app_ids"`
}

func NewDeviceExclusiveValidScopeSettingsDataSource() datasource.DataSource {
	return &DeviceExclusiveValidScopeSettingsDataSource{}
}
func (d *DeviceExclusiveValidScopeSettingsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_device_exclusive_valid_scope_settings"
}
func (d *DeviceExclusiveValidScopeSettingsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads the application IDs in the user-pool device-exclusive valid scope. This endpoint provides no verified tenant identity and does not manage scope.", Attributes: map[string]schema.Attribute{
		"app_ids": schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: "Application IDs in API order; empty list when no applications are in scope. Names/logos are not stored."},
	}}
}
func (d *DeviceExclusiveValidScopeSettingsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureGlobalSettingsClient(req, resp)
}
func (d *DeviceExclusiveValidScopeSettingsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Unable to read device-exclusive scope", "Authing client is not configured.")
		return
	}
	body, err := d.client.SendHttpRequestContext(ctx, "/api/v3/get-device-exclusive-valid-scope-settings", http.MethodGet, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read device-exclusive scope", "Authing did not return a successful scope response.")
		return
	}
	var envelope struct {
		StatusCode *int            `json:"statusCode"`
		Data       json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.StatusCode == nil || *envelope.StatusCode != http.StatusOK || len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		resp.Diagnostics.AddError("Unable to read device-exclusive scope", "Authing did not return a successful scope response.")
		return
	}
	var entries []json.RawMessage
	if json.Unmarshal(envelope.Data, &entries) != nil || entries == nil {
		resp.Diagnostics.AddError("Invalid device-exclusive scope", "Authing did not return a scope list.")
		return
	}
	ids := make([]string, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		var fields map[string]json.RawMessage
		if json.Unmarshal(entry, &fields) != nil || fields == nil {
			resp.Diagnostics.AddError("Invalid device-exclusive scope", "Authing returned an invalid scope entry.")
			return
		}
		var id, createdAt string
		if !decodeRequiredGlobalField(fields, "appId", &id) || id == "" || !decodeRequiredGlobalField(fields, "createdAt", &createdAt) || seen[id] {
			resp.Diagnostics.AddError("Invalid device-exclusive scope", "Authing returned an incomplete or duplicate scope entry.")
			return
		}
		if _, err := time.Parse(time.RFC3339Nano, createdAt); err != nil {
			resp.Diagnostics.AddError("Invalid device-exclusive scope", "Authing returned an invalid scope timestamp.")
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	list, diags := types.ListValueFrom(ctx, types.StringType, ids)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &DeviceExclusiveValidScopeSettingsDataSourceModel{AppIDs: list})...)
}
