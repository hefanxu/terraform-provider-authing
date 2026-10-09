package provider

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

var _ datasource.DataSource = &PublicAccountDataSource{}

func NewPublicAccountDataSource() datasource.DataSource { return &PublicAccountDataSource{} }

type PublicAccountDataSource struct{ client *authingapi.Client }
type PublicAccountDataModel struct {
	UserID   types.String `tfsdk:"user_id"`
	ID       types.String `tfsdk:"id"`
	Username types.String `tfsdk:"username"`
	Name     types.String `tfsdk:"name"`
	Nickname types.String `tfsdk:"nickname"`
	Email    types.String `tfsdk:"email"`
}

func (d *PublicAccountDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_public_account"
}
func (d *PublicAccountDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Looks up a public account by Authing user ID.", Attributes: map[string]schema.Attribute{
		"user_id": schema.StringAttribute{Required: true}, "id": schema.StringAttribute{Computed: true}, "username": schema.StringAttribute{Computed: true}, "name": schema.StringAttribute{Computed: true}, "nickname": schema.StringAttribute{Computed: true}, "email": schema.StringAttribute{Computed: true},
	}}
}
func (d *PublicAccountDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
func (d *PublicAccountDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m PublicAccountDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.UserID.IsNull() || m.UserID.IsUnknown() || m.UserID.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid public account lookup", "user_id must be nonempty")
		return
	}
	v, found, err := publicGet(ctx, d.client, m.UserID.ValueString())
	if err == nil && !found {
		err = errors.New("public account not found")
	}
	if err != nil {
		resp.Diagnostics.AddError("Lookup public account failed", err.Error())
		return
	}
	m.ID = types.StringValue(v.UserID)
	m.Username = publicString(v.Username)
	m.Name = publicString(v.Name)
	m.Nickname = publicString(v.Nickname)
	m.Email = publicString(v.Email)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
