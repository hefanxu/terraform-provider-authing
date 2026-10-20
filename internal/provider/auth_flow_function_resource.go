package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

var _ resource.Resource = &AuthFlowFunctionResource{}
var _ resource.ResourceWithImportState = &AuthFlowFunctionResource{}

func NewAuthFlowFunctionResource() resource.Resource { return &AuthFlowFunctionResource{} }

type AuthFlowFunctionResource struct{ client *authingapi.Client }
type AuthFlowFunctionModel struct {
	ID                 types.String `tfsdk:"id"`
	FuncName           types.String `tfsdk:"func_name"`
	FuncDescription    types.String `tfsdk:"func_description"`
	Scene              types.String `tfsdk:"scene"`
	SourceCode         types.String `tfsdk:"source_code"`
	IsAsynchronous     types.Bool   `tfsdk:"is_asynchronous"`
	Timeout            types.Int64  `tfsdk:"timeout"`
	TerminateOnTimeout types.Bool   `tfsdk:"terminate_on_timeout"`
	Enabled            types.Bool   `tfsdk:"enabled"`
}

func (r *AuthFlowFunctionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_auth_flow_function"
}
func (r *AuthFlowFunctionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Manages an AuthFlowFunction. Application scope (appId) is not exposed because the GET response does not return it. Source code is sensitive but remains in Terraform state.", Attributes: map[string]schema.Attribute{
		"id":                   schema.StringAttribute{Computed: true, Description: "Stable Authing funcId; import using this ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"func_name":            schema.StringAttribute{Required: true},
		"func_description":     schema.StringAttribute{Optional: true, Computed: true},
		"scene":                schema.StringAttribute{Required: true, Description: "Trigger scene; changing it replaces the function because update does not support scene.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"source_code":          schema.StringAttribute{Required: true, Sensitive: true, Description: "Function source code. Sensitive hides CLI output, NOT the state file; secure state storage is required."},
		"is_asynchronous":      schema.BoolAttribute{Optional: true, Computed: true},
		"timeout":              schema.Int64Attribute{Optional: true, Computed: true, Description: "Execution timeout in seconds (1–60)."},
		"terminate_on_timeout": schema.BoolAttribute{Optional: true, Computed: true},
		"enabled":              schema.BoolAttribute{Optional: true, Computed: true},
	}}
}
func (r *AuthFlowFunctionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

type flowEnvelope struct {
	StatusCode int             `json:"statusCode"`
	Data       json.RawMessage `json:"data"`
}
type flowRemote struct {
	FuncID             string  `json:"funcId"`
	FuncName           *string `json:"funcName"`
	FuncDescription    string  `json:"funcDescription"`
	Scene              *string `json:"scene"`
	SourceCode         *string `json:"sourceCode"`
	IsAsynchronous     *bool   `json:"isAsynchronous"`
	Timeout            *int64  `json:"timeout"`
	TerminateOnTimeout *bool   `json:"terminateOnTimeout"`
	Enabled            *bool   `json:"enabled"`
}

func flowSend(ctx context.Context, c *authingapi.Client, endpoint, method string, body any) (flowEnvelope, error) {
	var out flowEnvelope
	if c == nil {
		return out, errors.New("Authing client not configured")
	}
	raw, err := c.SendHttpRequestContext(ctx, endpoint, method, body)
	if err != nil {
		return out, err
	}
	if json.Unmarshal(raw, &out) != nil || out.StatusCode == 0 {
		return out, errors.New("invalid Authing response")
	}
	return out, nil
}
func flowGet(ctx context.Context, c *authingapi.Client, id string) (flowRemote, bool, error) {
	var v flowRemote
	if id == "" {
		return v, false, errors.New("empty funcId")
	}
	out, err := flowSend(ctx, c, "/api/v3/get-auth-flow-function", http.MethodGet, map[string]string{"funcId": id})
	if err != nil {
		return v, false, err
	}
	if out.StatusCode == 404 {
		return v, false, nil
	}
	if out.StatusCode != 200 {
		return v, false, fmt.Errorf("Authing status %d", out.StatusCode)
	}
	if json.Unmarshal(out.Data, &v) != nil || v.FuncID != id || v.FuncName == nil || v.Scene == nil || v.SourceCode == nil || v.IsAsynchronous == nil || v.Timeout == nil || v.TerminateOnTimeout == nil || v.Enabled == nil {
		return v, false, errors.New("get-auth-flow-function returned incomplete or mismatched funcId data")
	}
	return v, true, nil
}
func flowApply(m *AuthFlowFunctionModel, v flowRemote) {
	m.ID = types.StringValue(v.FuncID)
	m.FuncName = types.StringValue(*v.FuncName)
	m.FuncDescription = types.StringValue(v.FuncDescription)
	m.Scene = types.StringValue(*v.Scene)
	m.SourceCode = types.StringValue(*v.SourceCode)
	m.IsAsynchronous = types.BoolValue(*v.IsAsynchronous)
	m.Timeout = types.Int64Value(*v.Timeout)
	m.TerminateOnTimeout = types.BoolValue(*v.TerminateOnTimeout)
	m.Enabled = types.BoolValue(*v.Enabled)
}
func flowBody(m AuthFlowFunctionModel) map[string]any {
	b := map[string]any{"funcName": m.FuncName.ValueString(), "sourceCode": m.SourceCode.ValueString()}
	if !m.FuncDescription.IsNull() && !m.FuncDescription.IsUnknown() {
		b["funcDescription"] = m.FuncDescription.ValueString()
	}
	if !m.IsAsynchronous.IsNull() && !m.IsAsynchronous.IsUnknown() {
		b["isAsynchronous"] = m.IsAsynchronous.ValueBool()
	}
	if !m.Timeout.IsNull() && !m.Timeout.IsUnknown() {
		b["timeout"] = m.Timeout.ValueInt64()
	}
	if !m.TerminateOnTimeout.IsNull() && !m.TerminateOnTimeout.IsUnknown() {
		b["terminateOnTimeout"] = m.TerminateOnTimeout.ValueBool()
	}
	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() {
		b["enabled"] = m.Enabled.ValueBool()
	}
	return b
}
func flowWrite(ctx context.Context, c *authingapi.Client, endpoint string, body map[string]any) (string, error) {
	out, err := flowSend(ctx, c, endpoint, http.MethodPost, body)
	if err != nil {
		return "", err
	}
	if out.StatusCode != 200 {
		return "", fmt.Errorf("Authing status %d", out.StatusCode)
	}
	var v struct {
		FuncID string `json:"funcId"`
	}
	if json.Unmarshal(out.Data, &v) != nil || v.FuncID == "" {
		return "", errors.New("write response has no funcId")
	}
	return v.FuncID, nil
}
func flowVerify(m AuthFlowFunctionModel, v flowRemote) error {
	if m.FuncName.ValueString() != *v.FuncName || m.Scene.ValueString() != *v.Scene || m.SourceCode.ValueString() != *v.SourceCode {
		return errors.New("readback does not match requested function name, scene or source code")
	}
	if !m.FuncDescription.IsNull() && !m.FuncDescription.IsUnknown() && m.FuncDescription.ValueString() != v.FuncDescription {
		return errors.New("readback description does not match")
	}
	if !m.IsAsynchronous.IsNull() && !m.IsAsynchronous.IsUnknown() && m.IsAsynchronous.ValueBool() != *v.IsAsynchronous {
		return errors.New("readback asynchronous setting does not match")
	}
	if !m.Timeout.IsNull() && !m.Timeout.IsUnknown() && m.Timeout.ValueInt64() != *v.Timeout {
		return errors.New("readback timeout does not match")
	}
	if !m.TerminateOnTimeout.IsNull() && !m.TerminateOnTimeout.IsUnknown() && m.TerminateOnTimeout.ValueBool() != *v.TerminateOnTimeout {
		return errors.New("readback terminate-on-timeout setting does not match")
	}
	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() && m.Enabled.ValueBool() != *v.Enabled {
		return errors.New("readback enabled setting does not match")
	}
	return nil
}
func (r *AuthFlowFunctionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m AuthFlowFunctionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	b := flowBody(m)
	b["scene"] = m.Scene.ValueString()
	id, err := flowWrite(ctx, r.client, "/api/v3/create-auth-flow-function", b)
	if err != nil {
		resp.Diagnostics.AddError("Create AuthFlowFunction failed", err.Error())
		return
	}
	v, found, err := flowGet(ctx, r.client, id)
	if err == nil && found {
		err = flowVerify(m, v)
	}
	if err != nil || !found {
		resp.Diagnostics.AddError("Verify AuthFlowFunction creation failed", fmt.Sprintf("Authing returned funcId %q but readback failed (%v). Import this ID before retrying to avoid a duplicate.", id, err))
		return
	}
	flowApply(&m, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *AuthFlowFunctionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m AuthFlowFunctionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, found, err := flowGet(ctx, r.client, m.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Read AuthFlowFunction failed", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	flowApply(&m, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *AuthFlowFunctionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m, prior AuthFlowFunctionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := prior.ID.ValueString()
	if id == "" || (!m.ID.IsUnknown() && !m.ID.IsNull() && m.ID.ValueString() != id) {
		resp.Diagnostics.AddError("Update AuthFlowFunction failed", "Missing or changed funcId")
		return
	}
	if m.Scene.ValueString() != prior.Scene.ValueString() {
		resp.Diagnostics.AddError("Update AuthFlowFunction failed", "Scene cannot be updated; replace the resource")
		return
	}
	b := map[string]any{"funcId": id}
	if !m.FuncName.Equal(prior.FuncName) {
		b["funcName"] = m.FuncName.ValueString()
	}
	if !m.SourceCode.Equal(prior.SourceCode) {
		b["sourceCode"] = m.SourceCode.ValueString()
	}
	if !m.FuncDescription.Equal(prior.FuncDescription) {
		b["funcDescription"] = m.FuncDescription.ValueString()
	}
	if !m.IsAsynchronous.Equal(prior.IsAsynchronous) {
		b["isAsynchronous"] = m.IsAsynchronous.ValueBool()
	}
	if !m.Timeout.Equal(prior.Timeout) {
		b["timeout"] = m.Timeout.ValueInt64()
	}
	if !m.TerminateOnTimeout.Equal(prior.TerminateOnTimeout) {
		b["terminateOnTimeout"] = m.TerminateOnTimeout.ValueBool()
	}
	if !m.Enabled.Equal(prior.Enabled) {
		b["enabled"] = m.Enabled.ValueBool()
	}
	returned, err := flowWrite(ctx, r.client, "/api/v3/update-auth-flow-function", b)
	if err != nil {
		resp.Diagnostics.AddError("Update AuthFlowFunction failed", err.Error())
		return
	}
	if returned != id {
		resp.Diagnostics.AddError("Update AuthFlowFunction failed", "Authing returned a different funcId")
		return
	}
	v, found, err := flowGet(ctx, r.client, id)
	if err == nil && found {
		err = flowVerify(m, v)
	}
	if err != nil || !found {
		resp.Diagnostics.AddError("Verify AuthFlowFunction update failed", fmt.Sprintf("funcId %q: readback failed (%v)", id, err))
		return
	}
	flowApply(&m, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *AuthFlowFunctionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m AuthFlowFunctionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := m.ID.ValueString()
	_, found, err := flowGet(ctx, r.client, id)
	if err != nil {
		resp.Diagnostics.AddError("Check AuthFlowFunction before deletion failed", err.Error())
		return
	}
	if !found {
		return
	}
	out, err := flowSend(ctx, r.client, "/api/v3/delete-auth-flow-function", http.MethodPost, map[string]any{"funcId": id})
	if err != nil {
		resp.Diagnostics.AddError("Delete AuthFlowFunction failed", err.Error())
		return
	}
	if out.StatusCode == 404 {
		return
	}
	if out.StatusCode != 200 {
		resp.Diagnostics.AddError("Delete AuthFlowFunction failed", fmt.Sprintf("Authing status %d", out.StatusCode))
		return
	}
	// CommonResponseDto has no required data; reject an explicitly false success if returned.
	var ack struct {
		Success *bool `json:"success"`
	}
	if len(out.Data) > 0 && string(out.Data) != "null" && (json.Unmarshal(out.Data, &ack) != nil || (ack.Success != nil && !*ack.Success)) {
		resp.Diagnostics.AddError("Delete AuthFlowFunction failed", "Authing returned unsuccessful deletion data")
		return
	}
	_, found, err = flowGet(ctx, r.client, id)
	if err != nil {
		resp.Diagnostics.AddError("Verify AuthFlowFunction deletion failed", err.Error())
		return
	}
	if found {
		resp.Diagnostics.AddError("Verify AuthFlowFunction deletion failed", "Function still exists after deletion")
	}
}
func (r *AuthFlowFunctionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
