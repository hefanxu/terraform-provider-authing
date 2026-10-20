package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

var _ datasource.DataSource = (*TenantDataSource)(nil)

func NewTenantDataSource() datasource.DataSource { return &TenantDataSource{} }

type TenantDataSource struct{ client *authingapi.Client }
type TenantDataSourceModel struct {
	TenantID    types.String `tfsdk:"tenant_id"`
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	AppIDs      types.Set    `tfsdk:"app_ids"`
	Description types.String `tfsdk:"description"`
	SourceAppID types.String `tfsdk:"source_app_id"`
	Code        types.String `tfsdk:"code"`
}

func (d *TenantDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tenant"
}
func (d *TenantDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Looks up an Authing tenant by its stable tenant ID.", Attributes: map[string]schema.Attribute{
		"tenant_id": schema.StringAttribute{Required: true}, "id": schema.StringAttribute{Computed: true}, "name": schema.StringAttribute{Computed: true},
		"app_ids": schema.SetAttribute{Computed: true, ElementType: types.StringType}, "description": schema.StringAttribute{Computed: true}, "source_app_id": schema.StringAttribute{Computed: true}, "code": schema.StringAttribute{Computed: true},
	}}
}
func (d *TenantDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*authingapi.Client)
	if !ok {
		resp.Diagnostics.AddError("Invalid client", "Expected *authingapi.Client")
		return
	}
	d.client = c
}
func (d *TenantDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config TenantDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := config.TenantID.ValueString()
	if !validTenantID(id) {
		resp.Diagnostics.AddError("Invalid tenant ID", "Expected a non-empty tenant ID")
		return
	}
	result, status, err := readTenant(ctx, d.client, id, nil)
	if status == 404 {
		resp.Diagnostics.AddError("Tenant not found", fmt.Sprintf("No tenant with ID %q", id))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Read tenant failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &TenantDataSourceModel{TenantID: types.StringValue(id), ID: result.ID, Name: result.Name, AppIDs: result.AppIDs, Description: result.Description, SourceAppID: result.SourceAppID, Code: result.Code})...)
}
