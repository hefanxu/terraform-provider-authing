package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

const deviceStatusPath = "/api/v3/device-status"
const subjectAuthPath = "/api/v3/get-subject-auth-detail"

// readLookupData deliberately never includes remote response bodies in diagnostics.
// Authing may include sensitive account details in error messages.
func readLookupData(ctx context.Context, client *authingapi.Client, path, method string, payload any, required ...string) (map[string]json.RawMessage, bool) {
	if client == nil {
		return nil, false
	}
	body, err := client.SendHttpRequestContext(ctx, path, method, payload)
	if err != nil {
		return nil, false
	}
	var envelope struct {
		StatusCode *int            `json:"statusCode"`
		Data       json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.StatusCode == nil || *envelope.StatusCode != http.StatusOK || len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		return nil, false
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(envelope.Data, &data) != nil || data == nil {
		return nil, false
	}
	for _, field := range required {
		raw, ok := data[field]
		if !ok || len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			return nil, false
		}
		var text string
		if json.Unmarshal(raw, &text) != nil || text == "" {
			return nil, false
		}
	}
	return data, true
}

func lookupString(data map[string]json.RawMessage, key string) types.String {
	var value string
	if raw, ok := data[key]; ok && json.Unmarshal(raw, &value) == nil {
		return types.StringValue(value)
	}
	return types.StringNull()
}

var _ datasource.DataSource = &DeviceStatusDataSource{}
var _ datasource.DataSourceWithConfigure = &DeviceStatusDataSource{}

type DeviceStatusDataSource struct{ client *authingapi.Client }
type DeviceStatusDataSourceModel struct {
	DeviceID types.String `tfsdk:"device_id"`
	Status   types.String `tfsdk:"status"`
	DiffTime types.Number `tfsdk:"diff_time"`
}

func NewDeviceStatusDataSource() datasource.DataSource { return &DeviceStatusDataSource{} }
func (d *DeviceStatusDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_device_status"
}
func (d *DeviceStatusDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads the current status of an Authing terminal device; does not manage device lifecycle.", Attributes: map[string]schema.Attribute{
		"device_id": schema.StringAttribute{Required: true, Description: "Terminal device row ID returned when the device was created."},
		"status":    schema.StringAttribute{Computed: true, Description: "Current device status: activated, suspended, or deactivated."},
		"diff_time": schema.NumberAttribute{Computed: true, Description: "Remaining suspension time in seconds, when returned by Authing; otherwise null."},
	}}
}
func (d *DeviceStatusDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*authingapi.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", "Expected *authingapi.Client")
		return
	}
	d.client = client
}
func (d *DeviceStatusDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state DeviceStatusDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.DeviceID.IsNull() || state.DeviceID.IsUnknown() || state.DeviceID.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid device ID", "device_id must be nonempty.")
		return
	}
	data, ok := readLookupData(ctx, d.client, deviceStatusPath, http.MethodPost, map[string]string{"id": state.DeviceID.ValueString()}, "status")
	if !ok {
		resp.Diagnostics.AddError("Unable to read device status", "Authing did not return a successful, complete device status response.")
		return
	}
	status := lookupString(data, "status")
	switch status.ValueString() {
	case "activated", "suspended", "deactivated":
	default:
		resp.Diagnostics.AddError("Invalid device status response", "Authing returned an unsupported device status.")
		return
	}
	state.Status = status
	state.DiffTime = types.NumberNull()
	if raw, exists := data["diffTime"]; exists && !bytes.Equal(raw, []byte("null")) {
		var number json.Number
		if json.Unmarshal(raw, &number) != nil {
			resp.Diagnostics.AddError("Invalid device status response", "Authing returned an invalid diffTime.")
			return
		}
		value, _, err := big.ParseFloat(number.String(), 10, 256, big.ToNearestEven)
		if err != nil {
			resp.Diagnostics.AddError("Invalid device status response", "Authing returned an invalid diffTime.")
			return
		}
		state.DiffTime = types.NumberValue(value)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

var _ datasource.DataSource = &ApplicationSubjectAuthDataSource{}
var _ datasource.DataSourceWithConfigure = &ApplicationSubjectAuthDataSource{}

type ApplicationSubjectAuthDataSource struct{ client *authingapi.Client }
type ApplicationSubjectAuthDataSourceModel struct {
	TargetID         types.String `tfsdk:"target_id"`
	TargetType       types.String `tfsdk:"target_type"`
	AppID            types.String `tfsdk:"app_id"`
	AppName          types.String `tfsdk:"app_name"`
	ReqTargetID      types.String `tfsdk:"req_target_id"`
	ReqTargetName    types.String `tfsdk:"req_target_name"`
	ReqTargetType    types.String `tfsdk:"req_target_type"`
	ResultTargetType types.String `tfsdk:"result_target_type"`
	TargetName       types.String `tfsdk:"target_name"`
	AuthType         types.String `tfsdk:"auth_type"`
}

func NewApplicationSubjectAuthDataSource() datasource.DataSource {
	return &ApplicationSubjectAuthDataSource{}
}
func (d *ApplicationSubjectAuthDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application_subject_auth"
}
func (d *ApplicationSubjectAuthDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads application authorization details for a subject; does not grant or revoke access.", Attributes: map[string]schema.Attribute{
		"target_id":          schema.StringAttribute{Required: true, Description: "Subject ID to query."},
		"target_type":        schema.StringAttribute{Required: true, Description: "Subject type: USER, ROLE, GROUP, ORG, or AK_SK."},
		"app_id":             schema.StringAttribute{Required: true, Description: "Application ID to query."},
		"app_name":           schema.StringAttribute{Computed: true, Description: "Application name."},
		"req_target_id":      schema.StringAttribute{Computed: true, Description: "Requested subject ID returned by Authing."},
		"req_target_name":    schema.StringAttribute{Computed: true, Description: "Requested subject name returned by Authing."},
		"req_target_type":    schema.StringAttribute{Computed: true, Description: "Requested subject type returned by Authing."},
		"result_target_type": schema.StringAttribute{Computed: true, Description: "Target subject type returned by Authing."},
		"target_name":        schema.StringAttribute{Computed: true, Description: "Target subject name returned by Authing."},
		"auth_type":          schema.StringAttribute{Computed: true, Description: "Authorization type returned by Authing: DEFAULT, ALL, SELF, or SUBJECT."},
	}}
}
func (d *ApplicationSubjectAuthDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*authingapi.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", "Expected *authingapi.Client")
		return
	}
	d.client = client
}
func (d *ApplicationSubjectAuthDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ApplicationSubjectAuthDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.TargetID.IsNull() || state.TargetID.IsUnknown() || state.TargetID.ValueString() == "" || state.AppID.IsNull() || state.AppID.IsUnknown() || state.AppID.ValueString() == "" || state.TargetType.IsNull() || state.TargetType.IsUnknown() {
		resp.Diagnostics.AddError("Invalid subject authorization query", "target_id, target_type, and app_id must be known and nonempty.")
		return
	}
	switch state.TargetType.ValueString() {
	case "USER", "ROLE", "GROUP", "ORG", "AK_SK":
	default:
		resp.Diagnostics.AddError("Invalid subject authorization query", "target_type must be USER, ROLE, GROUP, ORG, or AK_SK.")
		return
	}
	data, ok := readLookupData(ctx, d.client, subjectAuthPath, http.MethodGet, map[string]string{"targetId": state.TargetID.ValueString(), "targetType": state.TargetType.ValueString(), "appId": state.AppID.ValueString()}, "appId", "appName", "reqTargetId", "reqTargetName", "reqTargetType", "targetType", "targetName", "authType")
	if !ok {
		resp.Diagnostics.AddError("Unable to read subject authorization", "Authing did not return a successful, complete authorization detail response.")
		return
	}
	if lookupString(data, "appId").ValueString() != state.AppID.ValueString() || lookupString(data, "reqTargetId").ValueString() != state.TargetID.ValueString() || lookupString(data, "reqTargetType").ValueString() != state.TargetType.ValueString() {
		resp.Diagnostics.AddError("Mismatched subject authorization", "Authing returned authorization details for a different application or subject.")
		return
	}
	state.AppName = lookupString(data, "appName")
	state.ReqTargetID = lookupString(data, "reqTargetId")
	state.ReqTargetName = lookupString(data, "reqTargetName")
	state.ReqTargetType = lookupString(data, "reqTargetType")
	state.ResultTargetType = lookupString(data, "targetType")
	state.TargetName = lookupString(data, "targetName")
	state.AuthType = lookupString(data, "authType")
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
