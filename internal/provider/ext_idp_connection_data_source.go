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

const extIdpConnectionsPath = "/api/v3/list-ext-idp-conns"

var _ datasource.DataSource = &ExtIdpConnectionDataSource{}
var _ datasource.DataSourceWithConfigure = &ExtIdpConnectionDataSource{}

type ExtIdpConnectionDataSource struct{ client *authingapi.Client }
type ExtIdpConnectionDataSourceModel struct {
	ExtIdpID     types.String `tfsdk:"ext_idp_id"`
	ConnectionID types.String `tfsdk:"connection_id"`
	Type         types.String `tfsdk:"type"`
	Identifier   types.String `tfsdk:"identifier"`
	DisplayName  types.String `tfsdk:"display_name"`
	Logo         types.String `tfsdk:"logo"`
	LoginOnly    types.Bool   `tfsdk:"login_only"`
}

func NewExtIdpConnectionDataSource() datasource.DataSource { return &ExtIdpConnectionDataSource{} }
func (d *ExtIdpConnectionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ext_idp_connection"
}
func (d *ExtIdpConnectionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Look up an external identity provider connection by its parent and connection IDs. Connection configuration fields are never exposed.", Attributes: map[string]schema.Attribute{
		"ext_idp_id":    schema.StringAttribute{Required: true, Description: "External identity provider ID (parent)."},
		"connection_id": schema.StringAttribute{Required: true, Description: "Connection ID within the external identity provider."},
		"type":          schema.StringAttribute{Computed: true, Description: "Connection type, if returned."},
		"identifier":    schema.StringAttribute{Computed: true, Description: "Connection identifier, if returned."},
		"display_name":  schema.StringAttribute{Computed: true, Description: "Display name, if returned."},
		"logo":          schema.StringAttribute{Computed: true, Description: "Logo URL, if returned."},
		"login_only":    schema.BoolAttribute{Computed: true, Description: "Whether the connection is login-only, if returned."},
	}}
}
func (d *ExtIdpConnectionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
func (d *ExtIdpConnectionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ExtIdpConnectionDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.ExtIdpID.IsNull() || state.ExtIdpID.IsUnknown() || state.ExtIdpID.ValueString() == "" || state.ConnectionID.IsNull() || state.ConnectionID.IsUnknown() || state.ConnectionID.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid connection lookup", "ext_idp_id and connection_id must be known and nonempty.")
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Unable to read connection", "Authing client is not configured.")
		return
	}
	raw, err := d.client.SendHttpRequestContext(ctx, extIdpConnectionsPath, http.MethodGet, map[string]string{"id": state.ExtIdpID.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Unable to read connection", "Authing connection lookup failed.")
		return
	}
	var envelope struct {
		StatusCode *int            `json:"statusCode"`
		Data       json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.StatusCode == nil || *envelope.StatusCode != http.StatusOK || len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		resp.Diagnostics.AddError("Unable to read connection", "Authing did not return a successful connection list.")
		return
	}
	// Decode only allowlisted properties: fields may contain client secrets and must
	// never be mapped to schema, state, diagnostics, or logs.
	var connections []struct {
		ID          string  `json:"id"`
		ExtIdpID    string  `json:"extIdpId"`
		Type        *string `json:"type"`
		Identifier  *string `json:"identifier"`
		DisplayName *string `json:"displayName"`
		Logo        *string `json:"logo"`
		LoginOnly   *bool   `json:"loginOnly"`
	}
	if len(envelope.Data) == 0 || envelope.Data[0] != '[' || json.Unmarshal(envelope.Data, &connections) != nil {
		resp.Diagnostics.AddError("Unable to read connection", "Authing returned an invalid connection list.")
		return
	}
	found := false
	for _, conn := range connections {
		if conn.ID != state.ConnectionID.ValueString() {
			continue
		}
		if found || conn.ExtIdpID != state.ExtIdpID.ValueString() {
			resp.Diagnostics.AddError("Mismatched connection", "Authing returned a duplicate or different-parent connection ID.")
			return
		}
		found = true
		state.Type = optionalConnectionString(conn.Type)
		state.Identifier = optionalConnectionString(conn.Identifier)
		state.DisplayName = optionalConnectionString(conn.DisplayName)
		state.Logo = optionalConnectionString(conn.Logo)
		state.LoginOnly = types.BoolNull()
		if conn.LoginOnly != nil {
			state.LoginOnly = types.BoolValue(*conn.LoginOnly)
		}
	}
	if !found {
		resp.Diagnostics.AddError("Connection not found", "Authing did not return the requested connection for this external identity provider.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
func optionalConnectionString(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(*value)
}
