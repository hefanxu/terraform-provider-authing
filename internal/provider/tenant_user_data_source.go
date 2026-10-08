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

var _ datasource.DataSource = &TenantUserDataSource{}
var _ datasource.DataSourceWithConfigure = &TenantUserDataSource{}

// TenantUserDataSource decodes only allowlisted identity and display fields.
// Never add password, salt, or the raw response to its state or diagnostics.
type TenantUserDataSource struct{ client *authingapi.Client }
type TenantUserDataSourceModel struct {
	TenantID      types.String `tfsdk:"tenant_id"`
	MemberID      types.String `tfsdk:"member_id"`
	LinkUserID    types.String `tfsdk:"link_user_id"`
	IsTenantAdmin types.Bool   `tfsdk:"is_tenant_admin"`
	Username      types.String `tfsdk:"username"`
	Name          types.String `tfsdk:"name"`
	Nickname      types.String `tfsdk:"nickname"`
	Photo         types.String `tfsdk:"photo"`
}

func NewTenantUserDataSource() datasource.DataSource { return &TenantUserDataSource{} }
func (d *TenantUserDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tenant_user"
}
func (d *TenantUserDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Read one tenant member by linked user ID or membership ID. Only identity, administrator flag, and nonsecret display fields are stored; password and salt are never exposed.", Attributes: map[string]schema.Attribute{
		"tenant_id":       schema.StringAttribute{Required: true, Description: "Explicit tenant ID; does not default to the provider tenant."},
		"member_id":       schema.StringAttribute{Optional: true, Computed: true, Description: "Tenant membership ID. Supply this or link_user_id, but not both."},
		"link_user_id":    schema.StringAttribute{Optional: true, Computed: true, Description: "Linked userpool user ID. Supply this or member_id, but not both."},
		"is_tenant_admin": schema.BoolAttribute{Computed: true},
		"username":        schema.StringAttribute{Computed: true},
		"name":            schema.StringAttribute{Computed: true},
		"nickname":        schema.StringAttribute{Computed: true},
		"photo":           schema.StringAttribute{Computed: true},
	}}
}
func (d *TenantUserDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
func (d *TenantUserDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state TenantUserDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	present := func(s types.String) bool { return !s.IsNull() && !s.IsUnknown() && s.ValueString() != "" }
	member := present(state.MemberID)
	linked := present(state.LinkUserID)
	if !present(state.TenantID) || member == linked || (!state.MemberID.IsNull() && !member) || (!state.LinkUserID.IsNull() && !linked) {
		resp.Diagnostics.AddError("Invalid tenant member lookup", "tenant_id must be nonempty; specify exactly one nonempty member_id or link_user_id.")
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Tenant member lookup failed", "Authing client is not configured.")
		return
	}
	query := map[string]string{"tenantId": state.TenantID.ValueString()}
	if member {
		query["memberId"] = state.MemberID.ValueString()
	} else {
		query["linkUserId"] = state.LinkUserID.ValueString()
	}
	raw, err := d.client.SendHttpRequestContext(ctx, "/api/v3/get-tenant-user", http.MethodGet, query)
	if err != nil {
		resp.Diagnostics.AddError("Tenant member lookup failed", "Authing request failed; no member state was returned.")
		return
	}
	var envelope struct {
		StatusCode *int            `json:"statusCode"`
		Data       json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.StatusCode == nil || *envelope.StatusCode != http.StatusOK || len(envelope.Data) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Data), []byte("null")) {
		resp.Diagnostics.AddError("Tenant member lookup failed", "Authing returned an unsuccessful or incomplete member response (including 404); no member state was returned.")
		return
	}
	var user struct {
		TenantID      *string `json:"tenantId"`
		MemberID      *string `json:"memberId"`
		LinkUserID    *string `json:"linkUserId"`
		IsTenantAdmin *bool   `json:"isTenantAdmin"`
		Username      *string `json:"username"`
		Name          *string `json:"name"`
		Nickname      *string `json:"nickname"`
		Photo         *string `json:"photo"`
	}
	if json.Unmarshal(envelope.Data, &user) != nil || user.TenantID == nil || *user.TenantID != state.TenantID.ValueString() || user.MemberID == nil || *user.MemberID == "" || user.LinkUserID == nil || *user.LinkUserID == "" || user.IsTenantAdmin == nil || (member && *user.MemberID != state.MemberID.ValueString()) || (linked && *user.LinkUserID != state.LinkUserID.ValueString()) {
		resp.Diagnostics.AddError("Invalid tenant member response", "Authing returned incomplete or mismatched tenant membership identity or administrator flag; no member state was returned.")
		return
	}
	state.MemberID = types.StringValue(*user.MemberID)
	state.LinkUserID = types.StringValue(*user.LinkUserID)
	state.IsTenantAdmin = types.BoolValue(*user.IsTenantAdmin)
	state.Username = customFieldString(user.Username)
	state.Name = customFieldString(user.Name)
	state.Nickname = customFieldString(user.Nickname)
	state.Photo = customFieldString(user.Photo)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
