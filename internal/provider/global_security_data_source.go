package provider

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

var _ datasource.DataSource = &GlobalSecuritySettingsDataSource{}
var _ datasource.DataSourceWithConfigure = &GlobalSecuritySettingsDataSource{}
var _ datasource.DataSource = &GlobalMFASettingsDataSource{}
var _ datasource.DataSourceWithConfigure = &GlobalMFASettingsDataSource{}

type GlobalSecuritySettingsDataSource struct{ client *authingapi.Client }
type GlobalSecuritySettingsDataSourceModel struct {
	RegisterDisabled          types.Bool `tfsdk:"register_disabled"`
	LoginRequireEmailVerified types.Bool `tfsdk:"login_require_email_verified"`
}

func NewGlobalSecuritySettingsDataSource() datasource.DataSource {
	return &GlobalSecuritySettingsDataSource{}
}
func (d *GlobalSecuritySettingsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_global_security_settings"
}
func (d *GlobalSecuritySettingsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads nonsecret user-pool security settings. This lookup does not manage settings or verify tenant scope.", Attributes: map[string]schema.Attribute{
		"register_disabled":            schema.BoolAttribute{Computed: true, Description: "Whether user self-registration is disabled."},
		"login_require_email_verified": schema.BoolAttribute{Computed: true, Description: "Whether email verification is required for email login."},
	}}
}
func (d *GlobalSecuritySettingsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureGlobalSettingsClient(req, resp)
}
func (d *GlobalSecuritySettingsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	data, ok := readLookupData(ctx, d.client, "/api/v3/get-security-settings", http.MethodGet, nil)
	if !ok {
		resp.Diagnostics.AddError("Unable to read security settings", "Authing did not return a successful security settings response.")
		return
	}
	var registerDisabled, loginRequireEmailVerified bool
	if !decodeRequiredGlobalField(data, "registerDisabled", &registerDisabled) || !decodeRequiredGlobalField(data, "loginRequireEmailVerified", &loginRequireEmailVerified) {
		resp.Diagnostics.AddError("Invalid security settings", "Authing did not return valid required security flags.")
		return
	}
	state := GlobalSecuritySettingsDataSourceModel{RegisterDisabled: types.BoolValue(registerDisabled), LoginRequireEmailVerified: types.BoolValue(loginRequireEmailVerified)}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

type GlobalMFASettingsDataSource struct{ client *authingapi.Client }
type GlobalMFASettingsDataSourceModel struct {
	EnabledFactors types.List `tfsdk:"enabled_factors"`
}

func NewGlobalMFASettingsDataSource() datasource.DataSource { return &GlobalMFASettingsDataSource{} }
func (d *GlobalMFASettingsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_global_mfa_settings"
}
func (d *GlobalMFASettingsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads enabled global MFA factors without changing them or verifying tenant scope.", Attributes: map[string]schema.Attribute{
		"enabled_factors": schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: "Enabled factors, in API order: OTP, SMS, EMAIL, or FACE (empty list when none)."},
	}}
}
func (d *GlobalMFASettingsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureGlobalSettingsClient(req, resp)
}
func (d *GlobalMFASettingsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	data, ok := readLookupData(ctx, d.client, "/api/v3/get-global-mfa-settings", http.MethodGet, nil)
	if !ok {
		resp.Diagnostics.AddError("Unable to read MFA settings", "Authing did not return a successful MFA settings response.")
		return
	}
	var factors []string
	if !decodeRequiredGlobalField(data, "enabledFactors", &factors) || factors == nil {
		resp.Diagnostics.AddError("Invalid MFA settings", "Authing did not return an enabled factors list.")
		return
	}
	for _, factor := range factors {
		switch factor {
		case "OTP", "SMS", "EMAIL", "FACE":
		default:
			resp.Diagnostics.AddError("Invalid MFA settings", "Authing returned an unsupported MFA factor.")
			return
		}
	}
	list, diags := types.ListValueFrom(ctx, types.StringType, factors)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &GlobalMFASettingsDataSourceModel{EnabledFactors: list})...)
}

// Decode only explicitly selected, schema-verified fields, never entire response DTOs.
func decodeRequiredGlobalField(data map[string]json.RawMessage, key string, target any) bool {
	raw, ok := data[key]
	return ok && len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, target) == nil
}
func configureGlobalSettingsClient(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *authingapi.Client {
	if req.ProviderData == nil {
		return nil
	}
	client, ok := req.ProviderData.(*authingapi.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", "Expected *authingapi.Client")
		return nil
	}
	return client
}
