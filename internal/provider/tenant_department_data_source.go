package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

// This is deliberately read-only. DepartmentDto does not include a tenantId;
// the scoped request alone cannot prove the department's tenant identity.
var _ datasource.DataSource = &TenantDepartmentDataSource{}
var _ datasource.DataSourceWithConfigure = &TenantDepartmentDataSource{}

type TenantDepartmentDataSource struct{ client *authingapi.Client }
type TenantDepartmentDataSourceModel struct {
	ID                 types.String `tfsdk:"id"`
	TenantID           types.String `tfsdk:"tenant_id"`
	OrganizationCode   types.String `tfsdk:"organization_code"`
	DepartmentID       types.String `tfsdk:"department_id"`
	Name               types.String `tfsdk:"name"`
	ParentDepartmentID types.String `tfsdk:"parent_department_id"`
	Description        types.String `tfsdk:"description"`
}

func NewTenantDepartmentDataSource() datasource.DataSource { return &TenantDepartmentDataSource{} }
func (d *TenantDepartmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tenant_department"
}
func (d *TenantDepartmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Read-only department lookup with an explicit tenant query. Authing's department response has no tenant ID: the organization response confirms the requested organization belongs to the tenant, but cannot prove the returned department belongs to that tenant. No tenant-scoped department writes are exposed.", Attributes: map[string]schema.Attribute{
		"id":                   schema.StringAttribute{Computed: true, Description: "Composite lookup key, not proof of department tenant ownership."},
		"tenant_id":            schema.StringAttribute{Required: true, Description: "Explicit tenant ID; never defaults to provider scope."},
		"organization_code":    schema.StringAttribute{Required: true},
		"department_id":        schema.StringAttribute{Required: true, Description: "Authing system department ID (not openDepartmentId)."},
		"name":                 schema.StringAttribute{Computed: true},
		"parent_department_id": schema.StringAttribute{Computed: true},
		"description":          schema.StringAttribute{Computed: true},
	}}
}
func (d *TenantDepartmentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*authingapi.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", "Expected *authingapi.Client")
		return
	}
	d.client = c
}
func (d *TenantDepartmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m TenantDepartmentDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.TenantID.IsNull() || m.TenantID.IsUnknown() || m.TenantID.ValueString() == "" || m.OrganizationCode.IsNull() || m.OrganizationCode.IsUnknown() || m.OrganizationCode.ValueString() == "" || m.DepartmentID.IsNull() || m.DepartmentID.IsUnknown() || m.DepartmentID.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid tenant department lookup", "tenant_id, organization_code, and department_id must be nonempty")
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Tenant department lookup failed", "Authing client is not configured")
		return
	}
	send := (&TenantMembershipResource{client: d.client}).send
	org, err := send(ctx, "/api/v3/get-organization", http.MethodGet, map[string]string{"tenantId": m.TenantID.ValueString(), "organizationCode": m.OrganizationCode.ValueString()})
	if err != nil || org.StatusCode != 200 {
		resp.Diagnostics.AddError("Tenant department lookup failed", "Could not verify organization in the requested tenant")
		return
	}
	var organization struct {
		TenantID         string `json:"tenantId"`
		OrganizationCode string `json:"organizationCode"`
		OrganizationName string `json:"organizationName"`
	}
	if json.Unmarshal(org.Data, &organization) != nil || organization.TenantID != m.TenantID.ValueString() || organization.OrganizationCode != m.OrganizationCode.ValueString() || organization.OrganizationName == "" {
		resp.Diagnostics.AddError("Tenant department lookup failed", "Organization response omitted or mismatched tenant, code, or name")
		return
	}
	department, err := send(ctx, "/api/v3/get-department", http.MethodGet, map[string]string{"tenantId": m.TenantID.ValueString(), "organizationCode": m.OrganizationCode.ValueString(), "departmentId": m.DepartmentID.ValueString(), "departmentIdType": "department_id"})
	if err != nil || department.StatusCode != 200 {
		resp.Diagnostics.AddError("Tenant department lookup failed", "Department request failed or did not return a department")
		return
	}
	var data struct {
		OrganizationCode   string  `json:"organizationCode"`
		DepartmentID       string  `json:"departmentId"`
		Name               string  `json:"name"`
		ParentDepartmentID string  `json:"parentDepartmentId"`
		Description        *string `json:"description"`
	}
	if json.Unmarshal(department.Data, &data) != nil || data.OrganizationCode != m.OrganizationCode.ValueString() || data.DepartmentID != m.DepartmentID.ValueString() || data.Name == "" || data.ParentDepartmentID == "" {
		resp.Diagnostics.AddError("Tenant department lookup failed", "Department response omitted or mismatched code, ID, name, or parent")
		return
	}
	key, _ := json.Marshal([3]string{m.TenantID.ValueString(), m.OrganizationCode.ValueString(), m.DepartmentID.ValueString()})
	m.ID = types.StringValue("v1." + base64.RawURLEncoding.EncodeToString(key))
	m.Name = types.StringValue(data.Name)
	m.ParentDepartmentID = types.StringValue(data.ParentDepartmentID)
	if data.Description == nil {
		m.Description = types.StringNull()
	} else {
		m.Description = types.StringValue(*data.Description)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
