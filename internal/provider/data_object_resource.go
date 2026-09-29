package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"github.com/Authing/authing-golang-sdk/v3/management"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/valyala/fasthttp"
)

var _ resource.Resource = &DataObjectResource{}
var _ resource.ResourceWithImportState = &DataObjectResource{}

func NewDataObjectResource() resource.Resource { return &DataObjectResource{} }

type DataObjectResource struct{ client *management.ManagementClient }
type DataObjectModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	Type         types.String `tfsdk:"type"`
	ParentKey    types.String `tfsdk:"parent_key"`
	Enable       types.Bool   `tfsdk:"enable"`
	DataType     types.String `tfsdk:"data_type"`
	ShowFieldKey types.String `tfsdk:"show_field_key"`
}

func (r *DataObjectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_object"
}
func (r *DataObjectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Manages a custom Authing data object model. Deleting a model may also delete its fields and data; back up data first.", Attributes: map[string]schema.Attribute{
		"id":   schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name": schema.StringAttribute{Required: true}, "description": schema.StringAttribute{Required: true},
		"type":       schema.StringAttribute{Required: true, Description: "Model type; custom is used for custom objects.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"parent_key": schema.StringAttribute{Required: true}, "enable": schema.BoolAttribute{Required: true},
		"data_type":      schema.StringAttribute{Required: true, Description: "list or tree.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"show_field_key": schema.StringAttribute{Required: true, Description: "Field key displayed for the model. Explicitly required because the API update requires it but get-model does not return it; supply an empty string only when no field is displayed."},
	}}
}
func (r *DataObjectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*management.ManagementClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *management.ManagementClient")
		return
	}
	r.client = c
}
func objectError(status int, msg string) string {
	return fmt.Sprintf("Authing statusCode=%d: %s", status, msg)
}
func (r *DataObjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var p DataObjectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &p)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The create API cannot set showFieldKey. Refuse a non-empty desired value
	// instead of persisting state that claims it was applied remotely.
	if !p.ShowFieldKey.IsNull() && !p.ShowFieldKey.IsUnknown() && p.ShowFieldKey.ValueString() != "" {
		resp.Diagnostics.AddError("Cannot set show_field_key during creation", "Create the model with show_field_key = \"\", create its fields, then update show_field_key in a subsequent apply.")
		return
	}
	res := r.client.CreateModel(&dto.CreateFunctionModelDto{Name: p.Name.ValueString(), Description: p.Description.ValueString(), Type: p.Type.ValueString(), ParentKey: p.ParentKey.ValueString(), Enable: p.Enable.ValueBool(), DataType: p.DataType.ValueString()})
	if res == nil {
		resp.Diagnostics.AddError("Create data object failed", "No valid response from Authing")
		return
	}
	if res.StatusCode != 200 || res.Data.Id == "" {
		resp.Diagnostics.AddError("Create data object failed", objectError(res.StatusCode, res.Message))
		return
	}
	p.ID = types.StringValue(res.Data.Id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &p)...)
}
func (r *DataObjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var s DataObjectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res := r.client.GetModel(&dto.GetModelDto{Id: s.ID.ValueString()})
	if res == nil {
		resp.Diagnostics.AddError("Read data object failed", "No valid response from Authing")
		return
	}
	if res.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if res.StatusCode != 200 {
		resp.Diagnostics.AddError("Read data object failed", objectError(res.StatusCode, res.Message))
		return
	}
	if res.Data.Id == "" {
		resp.Diagnostics.AddError("Read data object failed", "Successful response contained no model ID")
		return
	}
	s.ID = types.StringValue(res.Data.Id)
	s.Name = types.StringValue(res.Data.Name)
	s.Description = types.StringValue(res.Data.Description)
	s.Type = types.StringValue(res.Data.Type)
	s.ParentKey = types.StringValue(res.Data.ParentKey)
	s.Enable = types.BoolValue(res.Data.Enable)
	s.DataType = types.StringValue(res.Data.DataType)
	resp.Diagnostics.Append(resp.State.Set(ctx, &s)...)
}
func (r *DataObjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var p DataObjectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &p)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Read the current metadata before a full update so field order and detail configuration survive.
	existing := r.client.GetModel(&dto.GetModelDto{Id: p.ID.ValueString()})
	if existing == nil {
		resp.Diagnostics.AddError("Update data object failed", "Could not fetch existing model")
		return
	}
	if existing.StatusCode != 200 || existing.Data.Id == "" {
		resp.Diagnostics.AddError("Update data object failed", objectError(existing.StatusCode, existing.Message))
		return
	}
	config := existing.Data.Config
	if config == nil {
		config = map[string]any{}
	}
	body := map[string]any{"id": p.ID.ValueString(), "name": p.Name.ValueString(), "description": p.Description.ValueString(), "type": p.Type.ValueString(), "parentKey": p.ParentKey.ValueString(), "enable": p.Enable.ValueBool(), "fieldOrder": existing.Data.FieldOrder, "config": config, "showFieldKey": p.ShowFieldKey.ValueString()}
	// SDK UpdateFunctionModelDto omits the required showFieldKey member; use its authenticated transport.
	b, err := r.client.SendHttpRequest("/api/v3/metadata/update-model", fasthttp.MethodPost, body)
	if err != nil {
		resp.Diagnostics.AddError("Update data object failed", err.Error())
		return
	}
	var result dto.FunctionModelResDto
	if err = json.Unmarshal(b, &result); err != nil {
		resp.Diagnostics.AddError("Update data object failed", err.Error())
		return
	}
	if result.StatusCode != 200 || result.Data.Id == "" {
		resp.Diagnostics.AddError("Update data object failed", objectError(result.StatusCode, result.Message))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &p)...)
}
func (r *DataObjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var s DataObjectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res := r.client.RemoveModel(&dto.FunctionModelIdDto{Id: s.ID.ValueString()})
	if res == nil {
		resp.Diagnostics.AddError("Delete data object failed", "No valid response from Authing")
		return
	}
	if res.StatusCode != 200 && res.StatusCode != 404 {
		resp.Diagnostics.AddError("Delete data object failed", objectError(res.StatusCode, res.Message))
	}
}
func (r *DataObjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
