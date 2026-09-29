package provider

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

var _ datasource.DataSource = (*DataResourceDataSource)(nil)

func NewDataResourceDataSource() datasource.DataSource { return &DataResourceDataSource{} }

type DataResourceDataSource struct{ client *authingapi.Client }
type DataResourceDataSourceModel DataResourceModel

func (d *DataResourceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_resource"
}
func (d *DataResourceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Looks up a STRING or ARRAY Authing data resource by namespace and resource code.", Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true}, "namespace_code": schema.StringAttribute{Required: true}, "resource_code": schema.StringAttribute{Required: true}, "resource_name": schema.StringAttribute{Computed: true}, "type": schema.StringAttribute{Computed: true}, "struct": schema.StringAttribute{Computed: true}, "actions": schema.SetAttribute{Computed: true, ElementType: types.StringType}, "description": schema.StringAttribute{Computed: true},
	}}
}
func (d *DataResourceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
func (d *DataResourceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config DataResourceDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ns, code := config.NamespaceCode.ValueString(), config.ResourceCode.ValueString()
	if ns == "" || code == "" {
		resp.Diagnostics.AddError("Invalid lookup", "Namespace and resource code must not be empty")
		return
	}
	result, status, err := readDataResource(ctx, d.client, ns, code, nil)
	if status == 404 {
		resp.Diagnostics.AddError("Data resource not found", fmt.Sprintf("%s / %s", ns, code))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Read data resource failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, (*DataResourceDataSourceModel)(&result))...)
}
