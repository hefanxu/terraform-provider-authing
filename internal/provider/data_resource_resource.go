package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

var _ resource.Resource = (*DataResourceResource)(nil)
var _ resource.ResourceWithImportState = (*DataResourceResource)(nil)

func NewDataResourceResource() resource.Resource { return &DataResourceResource{} }

type DataResourceResource struct{ client *authingapi.Client }
type DataResourceModel struct {
	ID            types.String `tfsdk:"id"`
	NamespaceCode types.String `tfsdk:"namespace_code"`
	ResourceCode  types.String `tfsdk:"resource_code"`
	ResourceName  types.String `tfsdk:"resource_name"`
	Type          types.String `tfsdk:"type"`
	Struct        types.String `tfsdk:"struct"`
	Actions       types.Set    `tfsdk:"actions"`
	Description   types.String `tfsdk:"description"`
}

func (r *DataResourceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_resource"
}
func (r *DataResourceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Manages a STRING or ARRAY Authing data resource. Deletion removes the remote resource.", Attributes: map[string]schema.Attribute{
		"id":             schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"namespace_code": schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"resource_code":  schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"resource_name":  schema.StringAttribute{Required: true},
		"type":           schema.StringAttribute{Required: true, Description: "STRING or ARRAY. TREE and extendFieldList are not managed.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"struct":         schema.StringAttribute{Required: true, Description: "JSON-encoded string for STRING, or JSON array of distinct strings for ARRAY (up to 50)."},
		"actions":        schema.SetAttribute{Required: true, ElementType: types.StringType, Description: "Permission action names (up to 50)."},
		"description":    schema.StringAttribute{Optional: true},
	}}
}
func (r *DataResourceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*authingapi.Client)
	if !ok {
		resp.Diagnostics.AddError("Invalid client", "Expected *authingapi.Client")
		return
	}
	r.client = c
}

func dataResourceID(ns, code string) string {
	b, _ := json.Marshal([]string{ns, code})
	return string(b)
}
func parseDataResourceID(id string) (string, string, error) {
	var parts []string
	if err := json.Unmarshal([]byte(id), &parts); err != nil || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("ID must be a JSON array of two non-empty strings: [\"namespace\",\"resource\"]")
	}
	return parts[0], parts[1], nil
}
func dataResourceStruct(kind, text string) (json.RawMessage, string, error) {
	if kind != "STRING" && kind != "ARRAY" {
		return nil, "", errors.New("type must be STRING or ARRAY; TREE is not safely managed")
	}
	var value any
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, "", fmt.Errorf("invalid struct JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, "", errors.New("struct contains multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return nil, "", errors.New("invalid trailing struct JSON")
	}
	switch kind {
	case "STRING":
		v, ok := value.(string)
		if !ok || len(v) == 0 || len(v) > 500 {
			return nil, "", errors.New("STRING struct must be a JSON string of 1–500 bytes")
		}
	case "ARRAY":
		v, ok := value.([]any)
		if !ok || len(v) > 50 {
			return nil, "", errors.New("ARRAY struct must be a JSON array of at most 50 distinct strings")
		}
		seen := map[string]bool{}
		for _, item := range v {
			s, ok := item.(string)
			if !ok || seen[s] {
				return nil, "", errors.New("ARRAY struct must contain distinct strings")
			}
			seen[s] = true
		}
	}
	b, _ := json.Marshal(value)
	return b, string(b), nil
}
func dataResourceActions(ctx context.Context, v types.Set) ([]string, error) {
	if v.IsNull() || v.IsUnknown() {
		return nil, errors.New("actions must be known")
	}
	var a []string
	if d := v.ElementsAs(ctx, &a, false); d.HasError() {
		return nil, fmt.Errorf("invalid actions: %v", d)
	}
	if len(a) > 50 {
		return nil, errors.New("actions exceeds 50 entries")
	}
	return a, nil
}

type dataResourceRemote struct {
	NamespaceCode   string          `json:"namespaceCode"`
	ResourceCode    string          `json:"resourceCode"`
	ResourceName    string          `json:"resourceName"`
	Type            string          `json:"type"`
	Struct          json.RawMessage `json:"struct"`
	Actions         []string        `json:"actions"`
	Description     string          `json:"description"`
	ExtendFieldList json.RawMessage `json:"extendFieldList"`
}

func dataResourceRequest(ctx context.Context, c *authingapi.Client, endpoint, method string, body any) (int, dataResourceRemote, error) {
	var remote dataResourceRemote
	if c == nil {
		return 0, remote, errors.New("client is not configured")
	}
	raw, err := c.SendHttpRequestContext(ctx, endpoint, method, body)
	if err != nil {
		return 0, remote, err
	}
	var envelope struct {
		StatusCode int             `json:"statusCode"`
		Message    string          `json:"message"`
		Data       json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return 0, remote, err
	}
	if envelope.StatusCode == 404 {
		return 404, remote, nil
	}
	if envelope.StatusCode != 200 {
		return envelope.StatusCode, remote, fmt.Errorf("Authing statusCode=%d: %s", envelope.StatusCode, envelope.Message)
	}
	if endpoint == "/api/v3/get-data-resource" {
		if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
			return 200, remote, errors.New("missing data resource in successful response")
		}
		if err := json.Unmarshal(envelope.Data, &remote); err != nil {
			return 200, remote, err
		}
	}
	return 200, remote, nil
}
func dataResourceWriteError(status int, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("Authing statusCode=%d", status)
}
func readDataResource(ctx context.Context, c *authingapi.Client, ns, code string, previous *DataResourceModel) (DataResourceModel, int, error) {
	var result DataResourceModel
	status, remote, err := dataResourceRequest(ctx, c, "/api/v3/get-data-resource", http.MethodGet, map[string]string{"namespaceCode": ns, "resourceCode": code})
	if err != nil || status == 404 {
		return result, status, err
	}
	if remote.NamespaceCode != ns || remote.ResourceCode != code || remote.ResourceName == "" {
		return result, status, errors.New("response missing or mismatching data resource identity")
	}
	if len(remote.ExtendFieldList) > 0 && string(remote.ExtendFieldList) != "null" && string(remote.ExtendFieldList) != "[]" {
		return result, status, errors.New("resource has unmanaged extendFieldList; refusing incomplete state")
	}
	_, canonical, err := dataResourceStruct(remote.Type, string(remote.Struct))
	if err != nil {
		return result, status, fmt.Errorf("remote struct: %w", err)
	}
	if remote.Actions == nil {
		return result, status, errors.New("response missing actions")
	}
	if len(remote.Actions) > 50 {
		return result, status, errors.New("response has too many actions")
	}
	seen := map[string]bool{}
	values := make([]attr.Value, 0, len(remote.Actions))
	for _, a := range remote.Actions {
		if seen[a] {
			return result, status, errors.New("duplicate remote actions")
		}
		seen[a] = true
		values = append(values, types.StringValue(a))
	}
	result = DataResourceModel{ID: types.StringValue(dataResourceID(ns, code)), NamespaceCode: types.StringValue(ns), ResourceCode: types.StringValue(code), ResourceName: types.StringValue(remote.ResourceName), Type: types.StringValue(remote.Type), Struct: types.StringValue(canonical), Actions: types.SetValueMust(types.StringType, values), Description: types.StringValue(remote.Description)}
	if remote.Description == "" && previous != nil && previous.Description.IsNull() {
		result.Description = types.StringNull()
	}
	if previous != nil && !previous.Struct.IsNull() && !previous.Struct.IsUnknown() {
		if _, old, e := dataResourceStruct(remote.Type, previous.Struct.ValueString()); e == nil && old == canonical {
			result.Struct = previous.Struct
		}
	}
	return result, status, nil
}
func (r *DataResourceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var p DataResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &p)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if p.NamespaceCode.ValueString() == "" || p.ResourceCode.ValueString() == "" || p.ResourceName.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid resource", "Namespace, code and name must not be empty")
		return
	}
	raw, _, err := dataResourceStruct(p.Type.ValueString(), p.Struct.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid struct", err.Error())
		return
	}
	actions, err := dataResourceActions(ctx, p.Actions)
	if err != nil {
		resp.Diagnostics.AddError("Invalid actions", err.Error())
		return
	}
	body := map[string]any{"namespaceCode": p.NamespaceCode.ValueString(), "resourceCode": p.ResourceCode.ValueString(), "resourceName": p.ResourceName.ValueString(), "type": p.Type.ValueString(), "struct": raw, "actions": actions}
	if !p.Description.IsNull() && !p.Description.IsUnknown() {
		body["description"] = p.Description.ValueString()
	}
	status, _, err := dataResourceRequest(ctx, r.client, "/api/v3/create-data-resource", http.MethodPost, body)
	if err != nil || status != 200 {
		resp.Diagnostics.AddError("Create data resource failed", dataResourceWriteError(status, err))
		return
	}
	p.ID = types.StringValue(dataResourceID(p.NamespaceCode.ValueString(), p.ResourceCode.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &p)...)
}
func (r *DataResourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var s DataResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ns, code, err := parseDataResourceID(s.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid data resource ID", err.Error())
		return
	}
	next, status, err := readDataResource(ctx, r.client, ns, code, &s)
	if status == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Read data resource failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}
func (r *DataResourceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var p, s DataResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &p)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ns, code, err := parseDataResourceID(s.ID.ValueString())
	if err != nil || p.NamespaceCode.ValueString() != ns || p.ResourceCode.ValueString() != code || p.Type.ValueString() != s.Type.ValueString() {
		resp.Diagnostics.AddError("Immutable data resource identity", "Namespace, code and type require replacement")
		return
	}
	raw, _, err := dataResourceStruct(p.Type.ValueString(), p.Struct.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid struct", err.Error())
		return
	}
	actions, err := dataResourceActions(ctx, p.Actions)
	if err != nil {
		resp.Diagnostics.AddError("Invalid actions", err.Error())
		return
	}
	// Refuse to mutate a resource that changed type or acquired unmanaged fields
	// after the last refresh. Update omits type and extension fields entirely.
	current, status, err := readDataResource(ctx, r.client, ns, code, &s)
	if err != nil {
		resp.Diagnostics.AddError("Update data resource failed", err.Error())
		return
	}
	if status == 404 || current.Type.ValueString() != p.Type.ValueString() {
		resp.Diagnostics.AddError("Update data resource failed", "Remote data resource is missing or changed type; refresh before applying")
		return
	}
	body := map[string]any{"namespaceCode": ns, "resourceCode": code, "resourceName": p.ResourceName.ValueString(), "struct": raw, "actions": actions}
	if !p.Description.IsNull() && !p.Description.IsUnknown() {
		body["description"] = p.Description.ValueString()
	} else if !s.Description.IsNull() && !s.Description.IsUnknown() {
		body["description"] = ""
	}
	status, _, err = dataResourceRequest(ctx, r.client, "/api/v3/update-data-resource", http.MethodPost, body)
	if err != nil || status != 200 {
		resp.Diagnostics.AddError("Update data resource failed", dataResourceWriteError(status, err))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &p)...)
}
func (r *DataResourceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var s DataResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ns, code, err := parseDataResourceID(s.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid data resource ID", err.Error())
		return
	}
	_, _, err = dataResourceRequest(ctx, r.client, "/api/v3/delete-data-resource", http.MethodPost, map[string]string{"namespaceCode": ns, "resourceCode": code})
	if err != nil {
		resp.Diagnostics.AddError("Delete data resource failed", err.Error())
	}
}
func (r *DataResourceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ns, code, err := parseDataResourceID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid data resource ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), dataResourceID(ns, code))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("namespace_code"), ns)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("resource_code"), code)...)
}
