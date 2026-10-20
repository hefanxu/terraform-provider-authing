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

var _ resource.Resource = &TenantAdminResource{}
var _ resource.ResourceWithImportState = &TenantAdminResource{}

func NewTenantAdminResource() resource.Resource { return &TenantAdminResource{} }

type TenantAdminResource struct{ client *authingapi.Client }
type TenantAdminModel struct {
	ID         types.String `tfsdk:"id"`
	TenantID   types.String `tfsdk:"tenant_id"`
	LinkUserID types.String `tfsdk:"link_user_id"`
	MemberID   types.String `tfsdk:"member_id"`
}

func (r *TenantAdminResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tenant_admin"
}
func (r *TenantAdminResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Grants tenant administrator privilege to an existing tenant membership. Deletion revokes only administrator privilege; it never removes the membership or user.", Attributes: map[string]schema.Attribute{
		"id":           schema.StringAttribute{Computed: true, Description: "Versioned composite tenant and linked user identity (ta1.<base64url JSON array>).", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"tenant_id":    schema.StringAttribute{Required: true, Description: "Tenant ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"link_user_id": schema.StringAttribute{Required: true, Description: "Linked userpool user ID of an existing tenant member.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"member_id":    schema.StringAttribute{Computed: true, Description: "Pinned tenant membership ID; a changed membership blocks revocation."},
	}}
}
func (r *TenantAdminResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func adminID(tenant, user string) string {
	return "ta1." + strings.TrimPrefix(membershipID(tenant, user), "v1.")
}
func parseAdminID(id string) ([2]string, error) {
	if !strings.HasPrefix(id, "ta1.") {
		return [2]string{}, errors.New("expected ta1.<base64url JSON array of tenant ID, linked user ID>")
	}
	return parseMembershipID("v1." + strings.TrimPrefix(id, "ta1."))
}
func validAdmin(m TenantAdminModel) bool {
	return !m.TenantID.IsNull() && !m.TenantID.IsUnknown() && m.TenantID.ValueString() != "" && !m.LinkUserID.IsNull() && !m.LinkUserID.IsUnknown() && m.LinkUserID.ValueString() != ""
}

type tenantAdminUser struct {
	TenantID      string `json:"tenantId"`
	LinkUserID    string `json:"linkUserId"`
	MemberID      string `json:"memberId"`
	IsTenantAdmin *bool  `json:"isTenantAdmin"`
}

func (r *TenantAdminResource) send(ctx context.Context, endpoint, method string, body any) (membershipEnvelope, error) {
	var out membershipEnvelope
	if r.client == nil {
		return out, errors.New("Authing client is not configured")
	}
	raw, err := r.client.SendHttpRequestContext(ctx, endpoint, method, body)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("invalid Authing response: %w", err)
	}
	return out, nil
}
func (r *TenantAdminResource) get(ctx context.Context, m TenantAdminModel) (tenantAdminUser, bool, error) {
	var user tenantAdminUser
	if !validAdmin(m) {
		return user, false, errors.New("invalid tenant_id or link_user_id")
	}
	out, err := r.send(ctx, "/api/v3/get-tenant-user", http.MethodGet, map[string]string{"tenantId": m.TenantID.ValueString(), "linkUserId": m.LinkUserID.ValueString()})
	if err != nil {
		return user, false, err
	}
	if out.StatusCode == 404 {
		return user, false, nil
	}
	if out.StatusCode != 200 {
		return user, false, errors.New(membershipError(out, nil))
	}
	if err := json.Unmarshal(out.Data, &user); err != nil || user.TenantID != m.TenantID.ValueString() || user.LinkUserID != m.LinkUserID.ValueString() || user.MemberID == "" || user.IsTenantAdmin == nil {
		return user, false, errors.New("get-tenant-user returned missing or mismatched membership identity or admin flag")
	}
	return user, true, nil
}
func (r *TenantAdminResource) mutate(ctx context.Context, endpoint string, body any) error {
	out, err := r.send(ctx, endpoint, http.MethodPost, body)
	if err != nil {
		return err
	}
	if out.StatusCode != 200 {
		return errors.New(membershipError(out, nil))
	}
	// CommonResponseDto has no data field. Some deployments additionally return data.success.
	if len(out.Data) > 0 {
		var result struct {
			Success *bool `json:"success"`
		}
		if json.Unmarshal(out.Data, &result) != nil || result.Success == nil || !*result.Success {
			return errors.New("Authing returned status 200 without data.success: true")
		}
	}
	return nil
}
func (r *TenantAdminResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m TenantAdminModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	user, found, err := r.get(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Check tenant admin membership failed", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Tenant membership required", "Add the user as a tenant member before granting administrator privilege")
		return
	}
	if *user.IsTenantAdmin {
		resp.Diagnostics.AddError("Tenant admin already exists", "Import the existing administrator relation instead of adopting it through Create")
		return
	}
	if err := r.mutate(ctx, "/api/v3/set-tenant-admin", map[string]any{"tenantId": m.TenantID.ValueString(), "memberIds": []string{user.MemberID}}); err != nil {
		resp.Diagnostics.AddError("Set tenant admin failed", err.Error())
		return
	}
	confirmed, found, err := r.get(ctx, m)
	if err != nil || !found || confirmed.MemberID != user.MemberID || !*confirmed.IsTenantAdmin {
		resp.Diagnostics.AddError("Verify tenant admin failed", adminVerifyError(err))
		return
	}
	m.ID = types.StringValue(adminID(m.TenantID.ValueString(), m.LinkUserID.ValueString()))
	m.MemberID = types.StringValue(user.MemberID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func adminVerifyError(err error) string {
	if err != nil {
		return err.Error()
	}
	return "Administrator privilege on the original membership was not confirmed by get-tenant-user"
}
func (r *TenantAdminResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m TenantAdminModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	user, found, err := r.get(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Read tenant admin failed", err.Error())
		return
	}
	if !found || !*user.IsTenantAdmin {
		resp.State.RemoveResource(ctx)
		return
	}
	if !m.MemberID.IsNull() && !m.MemberID.IsUnknown() && m.MemberID.ValueString() != user.MemberID {
		resp.Diagnostics.AddError("Tenant membership replaced", "Remote member ID differs from the pinned administrator membership; refusing to adopt a replacement. Investigate before changing state.")
		return
	}
	m.ID = types.StringValue(adminID(m.TenantID.ValueString(), m.LinkUserID.ValueString()))
	m.MemberID = types.StringValue(user.MemberID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *TenantAdminResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Tenant admin update is unsupported", "Changing tenant_id or link_user_id requires replacement")
}
func (r *TenantAdminResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m TenantAdminModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	user, found, err := r.get(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Check tenant admin before revoke failed", err.Error())
		return
	}
	if !found || !*user.IsTenantAdmin {
		return
	}
	if m.MemberID.IsNull() || m.MemberID.IsUnknown() || m.MemberID.ValueString() != user.MemberID {
		resp.Diagnostics.AddError("Unsafe tenant admin revoke", "Remote member ID differs from state; refusing to revoke an administrator on a replacement membership")
		return
	}
	if err := r.mutate(ctx, "/api/v3/delete-tenant-admin", map[string]string{"tenantId": m.TenantID.ValueString(), "memberId": user.MemberID}); err != nil {
		resp.Diagnostics.AddError("Revoke tenant admin failed", err.Error())
		return
	}
	confirmed, found, err := r.get(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Verify tenant admin revoke failed", err.Error())
		return
	}
	if found && (confirmed.MemberID != user.MemberID || *confirmed.IsTenantAdmin) {
		resp.Diagnostics.AddError("Verify tenant admin revoke failed", "Administrator privilege on the original membership was not confirmed revoked; review remote membership before retrying")
		return
	}
}
func (r *TenantAdminResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := parseAdminID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	for key, value := range map[string]string{"id": req.ID, "tenant_id": parts[0], "link_user_id": parts[1]} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(key), value)...)
	}
}
