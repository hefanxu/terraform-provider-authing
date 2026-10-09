package provider

import (
	"context"
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

var _ resource.Resource = &CustomDomainResource{}
var _ resource.ResourceWithImportState = &CustomDomainResource{}

func NewCustomDomainResource() resource.Resource { return &CustomDomainResource{} }

type CustomDomainResource struct{ client *authingapi.Client }
type CustomDomainModel struct {
	ID            types.String `tfsdk:"id"`
	CustomDomain  types.String `tfsdk:"custom_domain"`
	DNSTxtName    types.String `tfsdk:"dns_txt_name"`
	DNSTxtValue   types.String `tfsdk:"dns_txt_value"`
	DNSVerified   types.Bool   `tfsdk:"dns_verified"`
	CNAME         types.String `tfsdk:"cname"`
	HTTPSVerified types.Bool   `tfsdk:"https_verified"`
}

func (r *CustomDomainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_domain"
}
func (r *CustomDomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Manages the single custom domain of the configured user pool. Destruction removes the domain; import an existing domain instead of creating over it. TLS certificate and private key are deliberately excluded from Terraform state.", Attributes: map[string]schema.Attribute{
		"id":             schema.StringAttribute{Computed: true, Description: "Domain name.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"custom_domain":  schema.StringAttribute{Required: true, Description: "Immutable custom domain name. Changing it removes the old domain and creates a new one.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"dns_txt_name":   schema.StringAttribute{Computed: true, Description: "DNS TXT verification record name."},
		"dns_txt_value":  schema.StringAttribute{Computed: true, Description: "DNS TXT verification record value (not a certificate private key)."},
		"dns_verified":   schema.BoolAttribute{Computed: true, Description: "DNS verification status."},
		"cname":          schema.StringAttribute{Computed: true, Description: "Target CNAME."},
		"https_verified": schema.BoolAttribute{Computed: true, Description: "HTTPS verification status."},
	}}
}
func (r *CustomDomainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func validCustomDomain(m CustomDomainModel) bool {
	name := m.CustomDomain.ValueString()
	return !m.CustomDomain.IsNull() && !m.CustomDomain.IsUnknown() && name != "" && strings.TrimSpace(name) == name && !strings.ContainsAny(name, " /\\\t\n\r") && (m.ID.IsNull() || m.ID.IsUnknown() || m.ID.ValueString() == name)
}

// Explicitly decode only public metadata; never retain the certificate/private key in a DTO.
type customDomainPublic struct {
	CustomDomain  string  `json:"customDomain"`
	DNSTxtName    *string `json:"dnsTxtName"`
	DNSTxtValue   *string `json:"dnsTxtValue"`
	DNSVerified   *bool   `json:"dnsVerified"`
	CNAME         *string `json:"cname"`
	HTTPSVerified *bool   `json:"httpsVerified"`
}

func (r *CustomDomainResource) send(ctx context.Context, endpoint, method string, payload any) (membershipEnvelope, error) {
	return (&TenantMembershipResource{client: r.client}).send(ctx, endpoint, method, payload)
}
func (r *CustomDomainResource) get(ctx context.Context) (customDomainPublic, bool, error) {
	var data customDomainPublic
	out, err := r.send(ctx, "/api/v3/get-custom-domain", http.MethodGet, nil)
	if err != nil {
		return data, false, err
	}
	if out.StatusCode == 404 {
		return data, false, nil
	}
	if out.StatusCode != 200 {
		return data, false, fmt.Errorf("Authing status %d", out.StatusCode)
	}
	if json.Unmarshal(out.Data, &data) != nil || data.CustomDomain == "" {
		return data, false, errors.New("get-custom-domain returned missing or invalid domain identity")
	}
	return data, true, nil
}
func domainString(v *string) types.String {
	if v == nil {
		return types.StringNull()
	}
	return types.StringValue(*v)
}
func domainBool(v *bool) types.Bool {
	if v == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*v)
}
func hydrateCustomDomain(m *CustomDomainModel, d customDomainPublic) {
	m.ID = types.StringValue(d.CustomDomain)
	m.CustomDomain = types.StringValue(d.CustomDomain)
	m.DNSTxtName = domainString(d.DNSTxtName)
	m.DNSTxtValue = domainString(d.DNSTxtValue)
	m.DNSVerified = domainBool(d.DNSVerified)
	m.CNAME = domainString(d.CNAME)
	m.HTTPSVerified = domainBool(d.HTTPSVerified)
}
func (r *CustomDomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m CustomDomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validCustomDomain(m) {
		resp.Diagnostics.AddError("Invalid custom domain", "custom_domain must be a nonempty domain name and must match the ID if present")
		return
	}
	_, found, err := r.get(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Check custom domain before create failed", err.Error())
		return
	}
	if found {
		resp.Diagnostics.AddError("Custom domain already exists", "This user pool already has a custom domain. Import the existing domain instead of adopting it through Create.")
		return
	}
	name := m.CustomDomain.ValueString()
	out, err := r.send(ctx, "/api/v3/create-custom-domain", http.MethodPost, map[string]string{"customDomain": name})
	if err != nil || out.StatusCode != 200 {
		resp.Diagnostics.AddError("Create custom domain failed", fmt.Sprintf("%s. The write may have succeeded; check Authing before retrying. Recovery import ID: %s", membershipError(out, err), name))
		return
	}
	var created struct {
		CustomDomain string `json:"customDomain"`
	}
	if json.Unmarshal(out.Data, &created) != nil || created.CustomDomain != name {
		resp.Diagnostics.AddError("Create custom domain returned wrong identity", fmt.Sprintf("Check Authing before retrying. Recovery import ID: %s", name))
		return
	}
	data, found, err := r.get(ctx)
	if err != nil || !found || data.CustomDomain != name {
		resp.Diagnostics.AddError("Verify created custom domain failed", fmt.Sprintf("Readback did not confirm the requested domain. Check Authing before retrying. Recovery import ID: %s", name))
		return
	}
	hydrateCustomDomain(&m, data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *CustomDomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m CustomDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validCustomDomain(m) {
		resp.Diagnostics.AddError("Invalid custom domain state", "State domain and ID must match")
		return
	}
	data, found, err := r.get(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Read custom domain failed", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	if data.CustomDomain != m.CustomDomain.ValueString() {
		resp.Diagnostics.AddError("Custom domain identity changed", "The user pool has a different custom domain; refusing to adopt a replacement. Review state and import explicitly.")
		return
	}
	hydrateCustomDomain(&m, data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *CustomDomainResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Custom domain update is unsupported", "Changing custom_domain requires replacement; TLS certificate management is not supported")
}
func (r *CustomDomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m CustomDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validCustomDomain(m) {
		resp.Diagnostics.AddError("Invalid custom domain state", "State domain and ID must match")
		return
	}
	data, found, err := r.get(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Check custom domain before removal failed", err.Error())
		return
	}
	if !found {
		return
	}
	if data.CustomDomain != m.CustomDomain.ValueString() {
		resp.Diagnostics.AddError("Unsafe custom domain removal", "Remote customDomain differs from state; refusing to remove another domain")
		return
	}
	out, err := r.send(ctx, "/api/v3/remove-custom-domain", http.MethodPost, nil)
	if err != nil || out.StatusCode != 200 || !membershipSuccess(out.Data) {
		resp.Diagnostics.AddError("Remove custom domain failed", membershipMutationError(out, err))
		return
	}
	_, found, err = r.get(ctx)
	if err != nil || found {
		resp.Diagnostics.AddError("Verify custom domain removal failed", "Could not confirm the user pool custom domain is absent; check Authing before retrying")
		return
	}
}
func (r *CustomDomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	m := CustomDomainModel{CustomDomain: types.StringValue(req.ID), ID: types.StringValue(req.ID)}
	if !validCustomDomain(m) {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a nonempty custom domain name, such as sso.example.com")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("custom_domain"), req.ID)...)
}
