package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/valyala/fasthttp"
	"terraform-provider-authing/internal/authingapi"
)

var _ resource.Resource = &DataObjectFieldResource{}
var _ resource.ResourceWithImportState = &DataObjectFieldResource{}

func NewDataObjectFieldResource() resource.Resource { return &DataObjectFieldResource{} }

type DataObjectFieldResource struct{ client *authingapi.Client }
type DataObjectFieldModel struct {
	ID       types.String `tfsdk:"id"`
	ModelID  types.String `tfsdk:"model_id"`
	Key      types.String `tfsdk:"key"`
	Name     types.String `tfsdk:"name"`
	Type     types.String `tfsdk:"type"`
	Show     types.Bool   `tfsdk:"show"`
	Editable types.Bool   `tfsdk:"editable"`
}

func (r *DataObjectFieldResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_object_field"
}
func (r *DataObjectFieldResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Manages a basic field on an Authing data object. Advanced validation, relation, enum, and default settings are not managed by this resource; avoid managing such fields here.", Attributes: map[string]schema.Attribute{
		"id":       schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"model_id": schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"key":      schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"name":     schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"type":     schema.StringAttribute{Required: true, Description: "Basic field type: Text, Textarea, Number, Boolean, or Date.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"show":     schema.BoolAttribute{Required: true, PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()}}, "editable": schema.BoolAttribute{Required: true, PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()}},
	}}
}
func (r *DataObjectFieldResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*authingapi.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *authingapi.Client")
		return
	}
	r.client = c
}

// The generated SDK DTOs omit currently required request keys; send explicit JSON via the SDK's authenticated transport.
func (r *DataObjectFieldResource) send(path string, method string, body any) (fieldReply, error) {
	b, err := r.client.SendHttpRequest(path, method, body)
	if err != nil {
		return fieldReply{}, err
	}
	var out fieldReply
	if err = json.Unmarshal(b, &out); err != nil {
		return out, err
	}
	return out, nil
}

type fieldReply struct {
	StatusCode int             `json:"statusCode"`
	Message    string          `json:"message"`
	Data       json.RawMessage `json:"data"`
}
type objectField struct {
	ID       string          `json:"id"`
	ModelID  string          `json:"modelId"`
	Key      string          `json:"key"`
	Name     string          `json:"name"`
	Type     json.RawMessage `json:"type"`
	Show     bool            `json:"show"`
	Editable bool            `json:"editable"`
}

func fieldPayload(p DataObjectFieldModel) map[string]any {
	return map[string]any{
		"modelId": p.ModelID.ValueString(), "name": p.Name.ValueString(), "key": p.Key.ValueString(), "type": p.Type.ValueString(), "show": p.Show.ValueBool(), "editable": p.Editable.ValueBool(),
		"help": "", "default": map[string]any{}, "require": false, "unique": false, "maxLength": 0, "max": 0, "min": 0, "regexp": "", "format": 0, "dropDown": map[string]any{"key": "", "label": ""}, "fuzzySearch": false, "forLogin": false, "relationType": "", "relationMultiple": false, "relationShowKey": "", "relationOptionalRange": map[string]any{"key": "", "operator": "", "value": ""}, "userVisible": false, "accept": []string{}, "size": 0, "userEditable": true, "showTenant": true, "cooperatorEditable": true,
	}
}
func fieldStatusError(resp *resource.CreateResponse, heading string, out fieldReply, err error) {
	if err != nil {
		resp.Diagnostics.AddError(heading, err.Error())
	} else {
		resp.Diagnostics.AddError(heading, objectError(out.StatusCode, out.Message))
	}
}
func (r *DataObjectFieldResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var p DataObjectFieldModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &p)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, supported := map[string]bool{"Text": true, "Textarea": true, "Number": true, "Boolean": true, "Date": true}[p.Type.ValueString()]; !supported {
		resp.Diagnostics.AddError("Unsupported field type", "Only Text, Textarea, Number, Boolean, and Date basic fields are supported")
		return
	}
	out, err := r.send("/api/v3/metadata/create-field", fasthttp.MethodPost, fieldPayload(p))
	if err != nil || out.StatusCode != 200 {
		fieldStatusError(resp, "Create data object field failed", out, err)
		return
	}
	var f objectField
	if err = json.Unmarshal(out.Data, &f); err != nil || f.ID == "" || f.ModelID != p.ModelID.ValueString() || f.Key != p.Key.ValueString() {
		resp.Diagnostics.AddError("Create data object field failed", "Response missing or mismatched ID, model ID, or key")
		return
	}
	p.ID = types.StringValue(f.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &p)...)
}
func (r *DataObjectFieldResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var s DataObjectFieldModel
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if s.ModelID.IsNull() || s.ModelID.ValueString() == "" {
		resp.Diagnostics.AddError("Read data object field failed", "model_id is required; import with model_id:field_id")
		return
	}
	// OpenAPI specifies modelId and from as required query parameters; the SDK ListField DTO lacks from.
	out, err := r.send("/api/v3/metadata/list-field", fasthttp.MethodGet, map[string]string{"modelId": s.ModelID.ValueString(), "from": "terraform"})
	if err != nil {
		resp.Diagnostics.AddError("Read data object field failed", err.Error())
		return
	}
	if out.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if out.StatusCode != 200 {
		resp.Diagnostics.AddError("Read data object field failed", objectError(out.StatusCode, out.Message))
		return
	}
	var fields []objectField
	if err = json.Unmarshal(out.Data, &fields); err != nil {
		resp.Diagnostics.AddError("Read data object field failed", fmt.Sprintf("Invalid list response: %v", err))
		return
	}
	for _, f := range fields {
		if f.ID != s.ID.ValueString() {
			continue
		}
		if f.ModelID != s.ModelID.ValueString() || f.Key == "" {
			resp.Diagnostics.AddError("Read data object field failed", "Field response has mismatched model ID or missing key")
			return
		}
		s.Key = types.StringValue(f.Key)
		s.Name = types.StringValue(f.Name)
		s.Show = types.BoolValue(f.Show)
		s.Editable = types.BoolValue(f.Editable)
		// The response type is numeric although create accepts an enum string.
		var n int
		if json.Unmarshal(f.Type, &n) == nil {
			names := map[int]string{1: "Text", 2: "Textarea", 3: "Number", 4: "Boolean", 5: "Date"}
			if typ, ok := names[n]; ok {
				s.Type = types.StringValue(typ)
			}
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &s)...)
		return
	}
	resp.State.RemoveResource(ctx)
}
func (r *DataObjectFieldResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unsafe field update refused", "The SDK cannot round-trip all required field settings. Changes require replacement; import existing advanced fields read-only or manage them outside this resource.")
}
func (r *DataObjectFieldResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var s DataObjectFieldModel
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.send("/api/v3/metadata/remove-field", fasthttp.MethodPost, &dto.FunctionModelFieldIdDto{Id: s.ID.ValueString(), ModelId: s.ModelID.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Delete data object field failed", err.Error())
		return
	}
	if out.StatusCode != 200 && out.StatusCode != 404 {
		resp.Diagnostics.AddError("Delete data object field failed", objectError(out.StatusCode, out.Message))
	}
}
func (r *DataObjectFieldResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use model_id:field_id")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("model_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
