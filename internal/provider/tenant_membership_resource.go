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

var _ resource.Resource = &TenantMembershipResource{}
var _ resource.ResourceWithImportState = &TenantMembershipResource{}

func NewTenantMembershipResource() resource.Resource { return &TenantMembershipResource{} }

type TenantMembershipResource struct{ client *authingapi.Client }
type TenantMembershipModel struct {
	ID         types.String `tfsdk:"id"`
	TenantID   types.String `tfsdk:"tenant_id"`
	LinkUserID types.String `tfsdk:"link_user_id"`
	MemberID   types.String `tfsdk:"member_id"`
}

func (r *TenantMembershipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tenant_membership"
}
func (r *TenantMembershipResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Links an existing userpool user to a tenant. Deletion detaches only this tenant membership, never the userpool user.", Attributes: map[string]schema.Attribute{
		"id":           schema.StringAttribute{Computed: true, Description: "Versioned composite tenant and linked user identity.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"tenant_id":    schema.StringAttribute{Required: true, Description: "Tenant ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"link_user_id": schema.StringAttribute{Required: true, Description: "Existing userpool user ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"member_id":    schema.StringAttribute{Computed: true, Description: "Authing tenant member ID, read back from the API."},
	}}
}
func (r *TenantMembershipResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func membershipID(tenant, user string) string {
	b, _ := json.Marshal([2]string{tenant, user})
	return "v1." + base64.RawURLEncoding.EncodeToString(b)
}
func parseMembershipID(id string) ([2]string, error) {
	var parts [2]string
	if !strings.HasPrefix(id, "v1.") {
		return parts, errors.New("expected v1.<base64url JSON array of tenant ID, linked user ID>")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, "v1."))
	if err != nil || json.Unmarshal(b, &parts) != nil || parts[0] == "" || parts[1] == "" || membershipID(parts[0], parts[1]) != id {
		return parts, errors.New("invalid tenant membership import ID")
	}
	return parts, nil
}
func validMembership(m TenantMembershipModel) bool {
	return !m.TenantID.IsNull() && !m.TenantID.IsUnknown() && m.TenantID.ValueString() != "" && !m.LinkUserID.IsNull() && !m.LinkUserID.IsUnknown() && m.LinkUserID.ValueString() != ""
}

type membershipEnvelope struct {
	StatusCode int             `json:"statusCode"`
	Data       json.RawMessage `json:"data"`
}

func (r *TenantMembershipResource) send(ctx context.Context, endpoint, method string, payload any) (membershipEnvelope, error) {
	var out membershipEnvelope
	if r.client == nil {
		return out, errors.New("Authing client is not configured")
	}
	raw, err := r.client.SendHttpRequestContext(ctx, endpoint, method, payload)
	if err != nil {
		return out, err
	}
	if json.Unmarshal(raw, &out) != nil {
		return out, errors.New("invalid Authing response")
	}
	return out, nil
}
func membershipError(out membershipEnvelope, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("Authing status %d", out.StatusCode)
}
func (r *TenantMembershipResource) get(ctx context.Context, m TenantMembershipModel) (string, bool, error) {
	if !validMembership(m) {
		return "", false, errors.New("invalid tenant_id or link_user_id in membership state")
	}
	out, err := r.send(ctx, "/api/v3/get-tenant-user", http.MethodGet, map[string]string{"tenantId": m.TenantID.ValueString(), "linkUserId": m.LinkUserID.ValueString()})
	if err != nil {
		return "", false, err
	}
	if out.StatusCode == 404 {
		return "", false, nil
	}
	if out.StatusCode != 200 {
		return "", false, errors.New(membershipError(out, nil))
	}
	var data struct {
		TenantID   string `json:"tenantId"`
		LinkUserID string `json:"linkUserId"`
		MemberID   string `json:"memberId"`
	}
	if json.Unmarshal(out.Data, &data) != nil || data.TenantID != m.TenantID.ValueString() || data.LinkUserID != m.LinkUserID.ValueString() || data.MemberID == "" {
		return "", false, errors.New("get-tenant-user returned missing or mismatched membership identity")
	}
	return data.MemberID, true, nil
}
func (r *TenantMembershipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m TenantMembershipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validMembership(m) {
		resp.Diagnostics.AddError("Invalid tenant membership", "tenant_id and link_user_id must be nonempty")
		return
	}
	_, found, err := r.get(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Check tenant membership before add failed", err.Error())
		return
	}
	if found {
		resp.Diagnostics.AddError("Tenant membership already exists", "Import the existing membership instead of adopting it through Create")
		return
	}
	out, err := r.send(ctx, "/api/v3/add-tenant-users", http.MethodPost, map[string]any{"tenantId": m.TenantID.ValueString(), "linkUserIds": []string{m.LinkUserID.ValueString()}})
	if err != nil || out.StatusCode != 200 || !membershipSuccess(out.Data) {
		resp.Diagnostics.AddError("Add tenant membership failed", membershipMutationError(out, err))
		return
	}
	member, found, err := r.get(ctx, m)
	if err != nil || !found {
		resp.Diagnostics.AddError("Verify tenant membership failed", membershipVerificationError(err))
		return
	}
	m.ID = types.StringValue(membershipID(m.TenantID.ValueString(), m.LinkUserID.ValueString()))
	m.MemberID = types.StringValue(member)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func membershipSuccess(raw json.RawMessage) bool {
	var v struct {
		Success *bool `json:"success"`
	}
	return json.Unmarshal(raw, &v) == nil && v.Success != nil && *v.Success
}
func membershipVerificationError(err error) string {
	if err != nil {
		return err.Error()
	}
	return "Membership was not confirmed by get-tenant-user"
}
func membershipMutationError(out membershipEnvelope, err error) string {
	if err != nil || out.StatusCode != 200 {
		return membershipError(out, err)
	}
	return "Authing returned status 200 without data.success: true"
}
func (r *TenantMembershipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m TenantMembershipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	member, found, err := r.get(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Read tenant membership failed", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	m.ID = types.StringValue(membershipID(m.TenantID.ValueString(), m.LinkUserID.ValueString()))
	m.MemberID = types.StringValue(member)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *TenantMembershipResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Tenant membership update is unsupported", "Changing tenant_id or link_user_id requires replacement")
}
func (r *TenantMembershipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m TenantMembershipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	member, found, err := r.get(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Check tenant membership before detach failed", err.Error())
		return
	}
	if !found {
		return
	}
	// Pin the exact member ID. A stale state must not detach a replacement membership.
	if m.MemberID.IsNull() || m.MemberID.IsUnknown() || m.MemberID.ValueString() != member {
		resp.Diagnostics.AddError("Unsafe tenant membership detach", "Remote member ID differs from state; refresh state and review the replacement before detaching")
		return
	}
	out, err := r.send(ctx, "/api/v3/remove-tenant-users", http.MethodPost, map[string]any{"tenantId": m.TenantID.ValueString(), "memberIds": []string{member}})
	if err != nil || out.StatusCode != 200 || !membershipSuccess(out.Data) {
		resp.Diagnostics.AddError("Detach tenant membership failed", membershipMutationError(out, err))
		return
	}
	_, found, err = r.get(ctx, m)
	if err != nil || found {
		resp.Diagnostics.AddError("Verify tenant membership detach failed", membershipVerificationError(err))
		return
	}
}
func (r *TenantMembershipResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := parseMembershipID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	for key, value := range map[string]string{"id": req.ID, "tenant_id": parts[0], "link_user_id": parts[1]} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(key), value)...)
	}
}
