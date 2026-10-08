package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

var _ resource.Resource = (*TenantResource)(nil)
var _ resource.ResourceWithImportState = (*TenantResource)(nil)

func NewTenantResource() resource.Resource { return &TenantResource{} }

type TenantResource struct{ client *authingapi.Client }
type TenantModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	AppIDs      types.Set    `tfsdk:"app_ids"`
	Description types.String `tfsdk:"description"`
	SourceAppID types.String `tfsdk:"source_app_id"`
	Code        types.String `tfsdk:"code"`
}

func (r *TenantResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tenant"
}
func (r *TenantResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Manages an Authing tenant. Deleting this resource deletes the tenant. app_ids exclusively manages the full associated application set; changes outside Terraform will be overwritten on apply.", Attributes: map[string]schema.Attribute{
		"id":            schema.StringAttribute{Computed: true, Description: "Stable tenant ID; import using this ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":          schema.StringAttribute{Required: true},
		"app_ids":       schema.SetAttribute{Required: true, ElementType: types.StringType, Description: "Complete associated application ID set. Terraform exclusively owns this association; an empty set removes all associations."},
		"description":   schema.StringAttribute{Optional: true},
		"source_app_id": schema.StringAttribute{Optional: true, Description: "Source application ID, when applicable. Omitted for console-created tenants."},
		"code":          schema.StringAttribute{Computed: true, Description: "Tenant code returned by Authing."},
	}}
}
func (r *TenantResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

type tenantRemote struct {
	TenantID    string   `json:"tenantId"`
	Name        string   `json:"name"`
	AppIDs      []string `json:"appIds"`
	Description string   `json:"description"`
	SourceAppID string   `json:"sourceAppId"`
	Code        string   `json:"code"`
}

// 404 is a missing object only when Authing supplies its documented status envelope.
func tenantRequest(ctx context.Context, c *authingapi.Client, endpoint, method string, body any) (int, json.RawMessage, error) {
	if c == nil {
		return 0, nil, errors.New("client is not configured")
	}
	raw, err := c.SendHttpRequestContext(ctx, endpoint, method, body)
	if err != nil {
		return 0, nil, err
	}
	var e struct {
		StatusCode int             `json:"statusCode"`
		Data       json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		return 0, nil, fmt.Errorf("invalid Authing tenant response: %w", err)
	}
	if e.StatusCode == 404 {
		return 404, nil, nil
	}
	if e.StatusCode != 200 {
		return e.StatusCode, nil, fmt.Errorf("Authing statusCode=%d", e.StatusCode)
	}
	return 200, e.Data, nil
}
func tenantSuccess(data json.RawMessage) error {
	var result struct {
		Success *bool `json:"success"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.Success == nil || !*result.Success {
		return errors.New("Authing did not confirm success")
	}
	return nil
}
func tenantIDs(ctx context.Context, set types.Set) ([]string, error) {
	if set.IsNull() || set.IsUnknown() {
		return nil, errors.New("app_ids must be known")
	}
	var ids []string
	if d := set.ElementsAs(ctx, &ids, false); d.HasError() {
		return nil, fmt.Errorf("invalid app_ids: %v", d)
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return nil, errors.New("app_ids cannot contain an empty ID")
		}
	}
	return ids, nil
}
func validTenantID(id string) bool { return id != "" && strings.TrimSpace(id) == id }
func readTenant(ctx context.Context, c *authingapi.Client, id string, previous *TenantModel) (TenantModel, int, error) {
	var m TenantModel
	status, data, err := tenantRequest(ctx, c, "/api/v3/get-tenant", http.MethodGet, map[string]string{"tenantId": id})
	if err != nil || status == 404 {
		return m, status, err
	}
	if len(data) == 0 || string(data) == "null" {
		return m, status, errors.New("missing tenant data in successful response")
	}
	var remote tenantRemote
	if err := json.Unmarshal(data, &remote); err != nil {
		return m, status, fmt.Errorf("invalid tenant data: %w", err)
	}
	if remote.TenantID != id || remote.Name == "" || remote.AppIDs == nil {
		return m, status, errors.New("response missing or mismatching tenant identity, name or appIds")
	}
	values := make([]attr.Value, 0, len(remote.AppIDs))
	seen := map[string]bool{}
	for _, app := range remote.AppIDs {
		if strings.TrimSpace(app) == "" || seen[app] {
			return m, status, errors.New("invalid or duplicate remote appIds")
		}
		seen[app] = true
		values = append(values, types.StringValue(app))
	}
	m = TenantModel{ID: types.StringValue(id), Name: types.StringValue(remote.Name), AppIDs: types.SetValueMust(types.StringType, values), Description: types.StringValue(remote.Description), SourceAppID: types.StringValue(remote.SourceAppID), Code: types.StringValue(remote.Code)}
	if remote.Description == "" && previous != nil && previous.Description.IsNull() {
		m.Description = types.StringNull()
	}
	if remote.SourceAppID == "" && previous != nil && previous.SourceAppID.IsNull() {
		m.SourceAppID = types.StringNull()
	}
	return m, status, nil
}
func tenantBody(ctx context.Context, m TenantModel) (map[string]any, error) {
	if strings.TrimSpace(m.Name.ValueString()) == "" {
		return nil, errors.New("name must not be empty")
	}
	ids, err := tenantIDs(ctx, m.AppIDs)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"name": m.Name.ValueString(), "appIds": ids}
	if !m.Description.IsNull() && !m.Description.IsUnknown() {
		body["description"] = m.Description.ValueString()
	}
	if !m.SourceAppID.IsNull() && !m.SourceAppID.IsUnknown() {
		body["sourceAppId"] = m.SourceAppID.ValueString()
	}
	return body, nil
}
func (r *TenantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var p TenantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &p)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := tenantBody(ctx, p)
	if err != nil {
		resp.Diagnostics.AddError("Invalid tenant", err.Error())
		return
	}
	status, data, err := tenantRequest(ctx, r.client, "/api/v3/create-tenant", http.MethodPost, body)
	if err != nil || status != 200 {
		resp.Diagnostics.AddError("Create tenant failed", tenantFailure(status, err))
		return
	}
	var created tenantRemote
	if err := json.Unmarshal(data, &created); err != nil || !validTenantID(created.TenantID) {
		resp.Diagnostics.AddError("Create tenant failed", "Authing did not return a valid tenantId")
		return
	}
	// Keep the ID even if the subsequent read fails, to avoid losing track of a created tenant.
	p.ID = types.StringValue(created.TenantID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &p)...)
	if resp.Diagnostics.HasError() {
		return
	}
	next, status, err := readTenant(ctx, r.client, created.TenantID, &p)
	if err != nil || status == 404 {
		resp.Diagnostics.AddError("Read created tenant failed", tenantFailure(status, err))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}
func tenantFailure(status int, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("Authing statusCode=%d", status)
}
func (r *TenantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var s TenantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validTenantID(s.ID.ValueString()) {
		resp.Diagnostics.AddError("Invalid tenant ID", "Expected a non-empty tenant ID")
		return
	}
	next, status, err := readTenant(ctx, r.client, s.ID.ValueString(), &s)
	if status == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Read tenant failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}
func (r *TenantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var p, s TenantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &p)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validTenantID(s.ID.ValueString()) || (!p.ID.IsUnknown() && p.ID.ValueString() != s.ID.ValueString()) {
		resp.Diagnostics.AddError("Invalid tenant ID", "Tenant identity cannot change")
		return
	}
	body, err := tenantBody(ctx, p)
	if err != nil {
		resp.Diagnostics.AddError("Invalid tenant", err.Error())
		return
	}
	body["tenantId"] = s.ID.ValueString()
	if p.Description.IsNull() && !s.Description.IsNull() {
		body["description"] = ""
	}
	if p.SourceAppID.IsNull() && !s.SourceAppID.IsNull() {
		body["sourceAppId"] = ""
	}
	status, data, err := tenantRequest(ctx, r.client, "/api/v3/update-tenant", http.MethodPost, body)
	if err != nil || status != 200 {
		resp.Diagnostics.AddError("Update tenant failed", tenantFailure(status, err))
		return
	}
	if err := tenantSuccess(data); err != nil {
		resp.Diagnostics.AddError("Update tenant failed", err.Error())
		return
	}
	p.ID = s.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &p)...)
	if resp.Diagnostics.HasError() {
		return
	}
	next, status, err := readTenant(ctx, r.client, s.ID.ValueString(), &p)
	if err != nil || status == 404 {
		resp.Diagnostics.AddError("Read updated tenant failed", tenantFailure(status, err))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}
func (r *TenantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var s TenantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validTenantID(s.ID.ValueString()) {
		resp.Diagnostics.AddError("Invalid tenant ID", "Expected a non-empty tenant ID")
		return
	}
	status, data, err := tenantRequest(ctx, r.client, "/api/v3/delete-tenant", http.MethodPost, map[string]string{"tenantId": s.ID.ValueString()})
	if status == 404 {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete tenant failed", err.Error())
		return
	}
	if err := tenantSuccess(data); err != nil {
		resp.Diagnostics.AddError("Delete tenant failed", err.Error())
	}
}
func (r *TenantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !validTenantID(req.ID) {
		resp.Diagnostics.AddError("Invalid tenant ID", "Expected a non-empty tenant ID")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
