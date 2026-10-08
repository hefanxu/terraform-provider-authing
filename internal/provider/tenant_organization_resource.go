package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

var _ resource.Resource = &TenantOrganizationResource{}
var _ resource.ResourceWithImportState = &TenantOrganizationResource{}

func NewTenantOrganizationResource() resource.Resource { return &TenantOrganizationResource{} }

type TenantOrganizationResource struct{ client *authingapi.Client }
type TenantOrganizationModel struct {
	ID               types.String `tfsdk:"id"`
	TenantID         types.String `tfsdk:"tenant_id"`
	OrganizationCode types.String `tfsdk:"organization_code"`
	OrganizationName types.String `tfsdk:"organization_name"`
	Description      types.String `tfsdk:"description"`
}

func tenantOrganizationID(tenant, code string) string {
	b, _ := json.Marshal([2]string{tenant, code})
	return "v1." + base64.RawURLEncoding.EncodeToString(b)
}
func parseTenantOrganizationID(id string) ([2]string, error) {
	var parts [2]string
	if !strings.HasPrefix(id, "v1.") {
		return parts, errors.New("expected v1.<base64url JSON array of tenant ID, organization code>")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, "v1."))
	if err != nil || json.Unmarshal(b, &parts) != nil || parts[0] == "" || parts[1] == "" || tenantOrganizationID(parts[0], parts[1]) != id {
		return parts, errors.New("invalid tenant organization import ID")
	}
	return parts, nil
}
func (r *TenantOrganizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tenant_organization"
}
func (r *TenantOrganizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Manages a tenant-scoped organization. Deletion is refused when the organization has child departments; the Authing deletion endpoint deletes the entire organization tree.", Attributes: map[string]schema.Attribute{
		"id":                schema.StringAttribute{Computed: true, Description: "Versioned composite tenant ID and organization code.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"tenant_id":         schema.StringAttribute{Required: true, Description: "Exact tenant ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"organization_code": schema.StringAttribute{Required: true, Description: "Immutable organization code within the tenant.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"organization_name": schema.StringAttribute{Required: true, Description: "Organization name."},
		"description":       schema.StringAttribute{Optional: true, Computed: true, Description: "Organization description."},
	}}
}
func (r *TenantOrganizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func validTenantOrganization(m TenantOrganizationModel) error {
	if m.TenantID.IsNull() || m.TenantID.IsUnknown() || m.TenantID.ValueString() == "" || m.OrganizationCode.IsNull() || m.OrganizationCode.IsUnknown() || m.OrganizationCode.ValueString() == "" {
		return errors.New("tenant_id and organization_code must be nonempty")
	}
	if !m.ID.IsNull() && !m.ID.IsUnknown() && m.ID.ValueString() != tenantOrganizationID(m.TenantID.ValueString(), m.OrganizationCode.ValueString()) {
		return errors.New("composite ID does not match tenant_id and organization_code")
	}
	return nil
}

type tenantOrgData struct {
	TenantID         string `json:"tenantId"`
	OrganizationCode string `json:"organizationCode"`
	OrganizationName string `json:"organizationName"`
	Description      string `json:"description"`
	HasChildren      *bool  `json:"hasChildren"`
}

func (r *TenantOrganizationResource) send(ctx context.Context, endpoint, method string, payload any) (membershipEnvelope, error) {
	return (&TenantMembershipResource{client: r.client}).send(ctx, endpoint, method, payload)
}
func (r *TenantOrganizationResource) get(ctx context.Context, m TenantOrganizationModel) (tenantOrgData, bool, error) {
	var data tenantOrgData
	if err := validTenantOrganization(m); err != nil {
		return data, false, err
	}
	out, err := r.send(ctx, "/api/v3/get-organization", http.MethodGet, map[string]string{"tenantId": m.TenantID.ValueString(), "organizationCode": m.OrganizationCode.ValueString()})
	if err != nil {
		return data, false, err
	}
	if out.StatusCode == 404 {
		return data, false, nil
	}
	if out.StatusCode != 200 {
		return data, false, fmt.Errorf("Authing status %d", out.StatusCode)
	}
	if json.Unmarshal(out.Data, &data) != nil || data.TenantID != m.TenantID.ValueString() || data.OrganizationCode != m.OrganizationCode.ValueString() || data.OrganizationName == "" {
		return data, false, errors.New("get-organization returned missing or mismatched tenant, code, or name")
	}
	return data, true, nil
}
func validateTenantOrgMutation(raw json.RawMessage, m TenantOrganizationModel) error {
	var data tenantOrgData
	if json.Unmarshal(raw, &data) != nil || data.TenantID != m.TenantID.ValueString() || data.OrganizationCode != m.OrganizationCode.ValueString() || data.OrganizationName == "" {
		return errors.New("mutation returned missing or mismatched tenant, code, or name")
	}
	return nil
}
func hydrateTenantOrg(m *TenantOrganizationModel, data tenantOrgData) {
	m.ID = types.StringValue(tenantOrganizationID(m.TenantID.ValueString(), m.OrganizationCode.ValueString()))
	m.OrganizationName = types.StringValue(data.OrganizationName)
	m.Description = types.StringValue(data.Description)
}
func (r *TenantOrganizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m TenantOrganizationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := validTenantOrganization(m); err != nil {
		resp.Diagnostics.AddError("Invalid tenant organization", err.Error())
		return
	}
	if m.OrganizationName.IsNull() || m.OrganizationName.IsUnknown() || m.OrganizationName.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid organization name", "organization_name must be nonempty")
		return
	}
	_, found, err := r.get(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Check tenant organization before create failed", err.Error())
		return
	}
	if found {
		resp.Diagnostics.AddError("Tenant organization already exists", "Import the existing organization instead of adopting it through Create")
		return
	}
	body := map[string]any{"tenantId": m.TenantID.ValueString(), "organizationCode": m.OrganizationCode.ValueString(), "organizationName": m.OrganizationName.ValueString(), "metadata": map[string]any{}}
	if !m.Description.IsNull() && !m.Description.IsUnknown() {
		body["description"] = m.Description.ValueString()
	}
	out, err := r.send(ctx, "/api/v3/create-organization", http.MethodPost, body)
	if err != nil || out.StatusCode != 200 {
		resp.Diagnostics.AddError("Create tenant organization failed", fmt.Sprintf("%s. If the request reached Authing, check before retrying. Recovery import ID: %s", membershipError(out, err), tenantOrganizationID(m.TenantID.ValueString(), m.OrganizationCode.ValueString())))
		return
	}
	if err := validateTenantOrgMutation(out.Data, m); err != nil {
		resp.Diagnostics.AddError("Create tenant organization returned wrong identity", fmt.Sprintf("%v. The write may have created an organization; check Authing before retrying. Recovery import ID: %s", err, tenantOrganizationID(m.TenantID.ValueString(), m.OrganizationCode.ValueString())))
		return
	}
	data, found, err := r.get(ctx, m)
	if err != nil || !found {
		resp.Diagnostics.AddError("Verify created tenant organization failed", fmt.Sprintf("%s. The write may have succeeded; check Authing before retrying. Recovery import ID: %s", tenantOrgVerifyError(err), tenantOrganizationID(m.TenantID.ValueString(), m.OrganizationCode.ValueString())))
		return
	}
	hydrateTenantOrg(&m, data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func tenantOrgVerifyError(err error) string {
	if err != nil {
		return err.Error()
	}
	return "Organization was not confirmed by get-organization"
}
func (r *TenantOrganizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m TenantOrganizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	data, found, err := r.get(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Read tenant organization failed", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	hydrateTenantOrg(&m, data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *TenantOrganizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m TenantOrganizationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := validTenantOrganization(m); err != nil {
		resp.Diagnostics.AddError("Invalid tenant organization", err.Error())
		return
	}
	_, found, err := r.get(ctx, m)
	if err != nil || !found {
		resp.Diagnostics.AddError("Check tenant organization before update failed", tenantOrgVerifyError(err))
		return
	}
	body := map[string]any{"tenantId": m.TenantID.ValueString(), "organizationCode": m.OrganizationCode.ValueString(), "organizationName": m.OrganizationName.ValueString()}
	if !m.Description.IsNull() && !m.Description.IsUnknown() {
		body["description"] = m.Description.ValueString()
	}
	out, err := r.send(ctx, "/api/v3/update-organization", http.MethodPost, body)
	if err != nil || out.StatusCode != 200 {
		resp.Diagnostics.AddError("Update tenant organization failed", membershipError(out, err))
		return
	}
	if err := validateTenantOrgMutation(out.Data, m); err != nil {
		resp.Diagnostics.AddError("Update tenant organization returned wrong identity", err.Error())
		return
	}
	data, found, err := r.get(ctx, m)
	if err != nil || !found {
		resp.Diagnostics.AddError("Verify updated tenant organization failed", tenantOrgVerifyError(err))
		return
	}
	hydrateTenantOrg(&m, data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *TenantOrganizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m TenantOrganizationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	data, found, err := r.get(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Check tenant organization before delete failed", err.Error())
		return
	}
	if !found {
		return
	}
	if data.HasChildren == nil || *data.HasChildren {
		resp.Diagnostics.AddError("Unsafe tenant organization deletion", "Authing deletes the entire organization tree; child-department status is unknown or children exist. Remove departments first.")
		return
	}
	out, err := r.send(ctx, "/api/v3/delete-organization", http.MethodPost, map[string]string{"tenantId": m.TenantID.ValueString(), "organizationCode": m.OrganizationCode.ValueString()})
	if err != nil || out.StatusCode != 200 || !membershipSuccess(out.Data) {
		resp.Diagnostics.AddError("Delete tenant organization failed", membershipMutationError(out, err))
		return
	}
	_, found, err = r.get(ctx, m)
	if err != nil || found {
		resp.Diagnostics.AddError("Verify tenant organization deletion failed", tenantOrgVerifyError(err))
		return
	}
}
func (r *TenantOrganizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := parseTenantOrganizationID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	for key, value := range map[string]string{"id": req.ID, "tenant_id": parts[0], "organization_code": parts[1]} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(key), value)...)
	}
}
