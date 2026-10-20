package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

var _ datasource.DataSource = &TenantCustomFieldDataSource{}
var _ datasource.DataSourceWithConfigure = &TenantCustomFieldDataSource{}

// TenantCustomFieldDataSource deliberately exposes only ordinary, readable metadata.
// The write DTO accepts settings that the list DTO cannot return; an upsert could
// silently overwrite these settings on an existing definition. This lookup does
// not return field values and rejects definitions explicitly marked encrypted.
type TenantCustomFieldDataSource struct{ client *authingapi.Client }

type TenantCustomFieldDataSourceModel struct {
	TenantID              types.String `tfsdk:"tenant_id"`
	TargetType            types.String `tfsdk:"target_type"`
	Key                   types.String `tfsdk:"key"`
	DataType              types.String `tfsdk:"data_type"`
	Label                 types.String `tfsdk:"label"`
	Description           types.String `tfsdk:"description"`
	UserEditable          types.Bool   `tfsdk:"user_editable"`
	VisibleInAdminConsole types.Bool   `tfsdk:"visible_in_admin_console"`
	VisibleInUserCenter   types.Bool   `tfsdk:"visible_in_user_center"`
}

func NewTenantCustomFieldDataSource() datasource.DataSource { return &TenantCustomFieldDataSource{} }
func (d *TenantCustomFieldDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tenant_custom_field"
}
func (d *TenantCustomFieldDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Read a single custom-field definition in an explicit tenant scope. Read-only: updates cannot safely preserve settings omitted from Authing's list response; deleting a definition may remove stored field data. Does not expose encrypted fields or advanced settings.", Attributes: map[string]schema.Attribute{
		"tenant_id":                schema.StringAttribute{Required: true, Description: "Tenant ID; never defaults to the user pool."},
		"target_type":              schema.StringAttribute{Required: true, Description: "USER, ROLE, or DEPARTMENT (GROUP is not supported by the list API)."},
		"key":                      schema.StringAttribute{Required: true, Description: "Exact custom field key."},
		"data_type":                schema.StringAttribute{Computed: true},
		"label":                    schema.StringAttribute{Computed: true},
		"description":              schema.StringAttribute{Computed: true},
		"user_editable":            schema.BoolAttribute{Computed: true},
		"visible_in_admin_console": schema.BoolAttribute{Computed: true},
		"visible_in_user_center":   schema.BoolAttribute{Computed: true},
	}}
}
func (d *TenantCustomFieldDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*authingapi.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", "Expected *authingapi.Client")
		return
	}
	d.client = c
}
func customFieldString(v *string) types.String {
	if v == nil {
		return types.StringNull()
	}
	return types.StringValue(*v)
}
func customFieldBool(v *bool) types.Bool {
	if v == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*v)
}
func (d *TenantCustomFieldDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state TenantCustomFieldDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.TenantID.IsNull() || state.TenantID.IsUnknown() || state.TenantID.ValueString() == "" || state.Key.IsNull() || state.Key.IsUnknown() || state.Key.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid custom field identity", "tenant_id and key must be nonempty.")
		return
	}
	switch state.TargetType.ValueString() {
	case "USER", "ROLE", "DEPARTMENT":
	default:
		resp.Diagnostics.AddError("Invalid custom field target type", "target_type must be USER, ROLE, or DEPARTMENT; GROUP is not supported by get-custom-fields.")
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Custom field lookup failed", "Authing client is not configured.")
		return
	}
	raw, err := d.client.SendHttpRequestContext(ctx, "/api/v3/get-custom-fields", http.MethodGet, map[string]string{"tenantId": state.TenantID.ValueString(), "targetType": state.TargetType.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Custom field lookup failed", "Authing request failed; no field state was returned.")
		return
	}
	var envelope struct {
		StatusCode *int            `json:"statusCode"`
		Data       json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.StatusCode == nil || *envelope.StatusCode != http.StatusOK || len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		resp.Diagnostics.AddError("Custom field lookup failed", "Authing returned an unsuccessful or incomplete field list (including 404); no field state was returned.")
		return
	}
	var list []struct {
		TargetType            *string `json:"targetType"`
		Key                   *string `json:"key"`
		DataType              *string `json:"dataType"`
		Label                 *string `json:"label"`
		Description           *string `json:"description"`
		IsUnique              *bool   `json:"isUnique"`
		UserEditable          *bool   `json:"userEditable"`
		VisibleInAdminConsole *bool   `json:"visibleInAdminConsole"`
		VisibleInUserCenter   *bool   `json:"visibleInUserCenter"`
		Encrypted             *bool   `json:"encrypted"`
	}
	if json.Unmarshal(envelope.Data, &list) != nil || list == nil {
		resp.Diagnostics.AddError("Invalid custom field response", "Authing did not return a field list.")
		return
	}
	matches := 0
	for _, field := range list {
		if field.Key != nil && *field.Key == state.Key.ValueString() && field.TargetType != nil && *field.TargetType == state.TargetType.ValueString() {
			matches++
		}
	}
	if matches > 1 {
		resp.Diagnostics.AddError("Ambiguous custom field response", "Authing returned duplicate definitions for the requested target type and key.")
		return
	}
	for _, field := range list {
		if field.Key == nil || *field.Key != state.Key.ValueString() {
			continue
		}
		if field.TargetType == nil || *field.TargetType != state.TargetType.ValueString() {
			continue
		}
		if field.DataType == nil || field.Label == nil || field.IsUnique == nil || field.VisibleInAdminConsole == nil || *field.Label == "" {
			resp.Diagnostics.AddError("Invalid custom field response", "Matching field has incomplete required metadata.")
			return
		}
		if field.Encrypted != nil && *field.Encrypted {
			resp.Diagnostics.AddError("Encrypted custom field unsupported", "This lookup does not expose encrypted custom field definitions.")
			return
		}
		state.DataType = customFieldString(field.DataType)
		state.Label = customFieldString(field.Label)
		state.Description = customFieldString(field.Description)
		state.UserEditable = customFieldBool(field.UserEditable)
		state.VisibleInAdminConsole = customFieldBool(field.VisibleInAdminConsole)
		state.VisibleInUserCenter = customFieldBool(field.VisibleInUserCenter)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	resp.Diagnostics.AddError("Custom field not found", "No definition matched the requested tenant, target type, and key.")
}
