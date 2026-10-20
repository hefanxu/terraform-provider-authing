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

var _ datasource.DataSource = &DataObjectRowDataSource{}
var _ datasource.DataSourceWithConfigure = &DataObjectRowDataSource{}

type DataObjectRowDataSource struct{ client *authingapi.Client }
type DataObjectRowDataSourceModel struct {
	ModelID   types.String `tfsdk:"model_id"`
	RowID     types.String `tfsdk:"row_id"`
	CellsJSON types.String `tfsdk:"cells_json"`
}

func NewDataObjectRowDataSource() datasource.DataSource { return &DataObjectRowDataSource{} }
func (d *DataObjectRowDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_object_row"
}
func (d *DataObjectRowDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Read a metadata data-object row by model and row ID. Row writes are not supported: Authing does not specify whether write data keys are field IDs or keys, or how partial updates and relations round-trip. Cell values can include secrets and remain in Terraform state.", Attributes: map[string]schema.Attribute{
		"model_id":   schema.StringAttribute{Required: true, Description: "Data-object model ID."},
		"row_id":     schema.StringAttribute{Required: true, Description: "Exact row ID."},
		"cells_json": schema.StringAttribute{Computed: true, Sensitive: true, Description: "JSON object keyed by field ID; each value is the raw JSON value returned for that cell. May contain secrets. Protect Terraform state and derived outputs."},
	}}
}
func (d *DataObjectRowDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureGlobalSettingsClient(req, resp)
}
func (d *DataObjectRowDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state DataObjectRowDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.ModelID.IsNull() || state.ModelID.IsUnknown() || state.ModelID.ValueString() == "" || state.RowID.IsNull() || state.RowID.IsUnknown() || state.RowID.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid row identity", "model_id and row_id must be nonempty.")
		return
	}
	data, ok := readLookupData(ctx, d.client, "/api/v3/metadata/get-row", http.MethodGet, map[string]string{
		"modelId": state.ModelID.ValueString(), "rowId": state.RowID.ValueString(), "showFieldId": "true", "showRelation": "false",
	}, "rowId")
	if !ok {
		resp.Diagnostics.AddError("Unable to read data-object row", "Authing did not return a successful row response.")
		return
	}
	var rowID string
	if json.Unmarshal(data["rowId"], &rowID) != nil || rowID != state.RowID.ValueString() {
		resp.Diagnostics.AddError("Invalid data-object row", "Returned row identity does not match the requested row.")
		return
	}
	raw, exists := data["cellList"]
	if !exists || bytes.Equal(raw, []byte("null")) {
		resp.Diagnostics.AddError("Invalid data-object row", "Authing did not return a cell list.")
		return
	}
	var cells []struct {
		FieldID string          `json:"fieldId"`
		Value   json.RawMessage `json:"value"`
	}
	if json.Unmarshal(raw, &cells) != nil || cells == nil {
		resp.Diagnostics.AddError("Invalid data-object row", "Authing returned an invalid cell list.")
		return
	}
	values := make(map[string]json.RawMessage, len(cells))
	for _, cell := range cells {
		if cell.FieldID == "" || len(cell.Value) == 0 || bytes.Equal(cell.Value, []byte("null")) {
			resp.Diagnostics.AddError("Invalid data-object row", "A cell lacks a field ID or value.")
			return
		}
		if _, exists := values[cell.FieldID]; exists {
			resp.Diagnostics.AddError("Invalid data-object row", "Duplicate field ID in row response.")
			return
		}
		values[cell.FieldID] = cell.Value
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		resp.Diagnostics.AddError("Invalid data-object row", "Could not encode cell values.")
		return
	}
	state.CellsJSON = types.StringValue(string(encoded))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
