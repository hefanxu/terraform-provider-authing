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

var _ resource.Resource = &InvitationPolicyResource{}
var _ resource.ResourceWithImportState = &InvitationPolicyResource{}

func NewInvitationPolicyResource() resource.Resource { return &InvitationPolicyResource{} }

type InvitationPolicyResource struct{ client *authingapi.Client }
type InvitationPolicyModel struct {
	ID                      types.String `tfsdk:"id"`
	Name                    types.String `tfsdk:"name"`
	EnabledIdentifierVerify types.Bool   `tfsdk:"enabled_identifier_verify"`
	EnabledInfoFill         types.Bool   `tfsdk:"enabled_info_fill"`
	RegisterInfoFillMsg     types.String `tfsdk:"register_info_fill_msg"`
}

func (r *InvitationPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_invitation_policy"
}
func (r *InvitationPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Persistent invitation policy. Sending invitations and generating links are separate one-shot operations and are not managed here. Password configuration is deliberately excluded.", Attributes: map[string]schema.Attribute{
		"id":                        schema.StringAttribute{Computed: true, Description: "Authing policy ID; import with this ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":                      schema.StringAttribute{Required: true, Description: "Policy name (1–200 characters)."},
		"enabled_identifier_verify": schema.BoolAttribute{Optional: true, Computed: true, Description: "Require an identity verification code."},
		"enabled_info_fill":         schema.BoolAttribute{Optional: true, Computed: true, Description: "Require registration information completion."},
		"register_info_fill_msg":    schema.StringAttribute{Optional: true, Computed: true, Description: "Information completion prompt; null and empty string are distinct."},
	}}
}
func (r *InvitationPolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

type invitationEnvelope struct {
	StatusCode int             `json:"statusCode"`
	Data       json.RawMessage `json:"data"`
}

func (r *InvitationPolicyResource) send(ctx context.Context, endpoint, method string, payload any) (invitationEnvelope, error) {
	var out invitationEnvelope
	if r.client == nil {
		return out, errors.New("Authing client is not configured")
	}
	raw, err := r.client.SendHttpRequestContext(ctx, endpoint, method, payload)
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, errors.New("invalid invitation policy response")
	}
	return out, nil
}
func invitationStatus(out invitationEnvelope, err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("Authing invitation policy status %d", out.StatusCode)
}
func validInvitationName(m InvitationPolicyModel) bool {
	return !m.Name.IsNull() && !m.Name.IsUnknown() && len(m.Name.ValueString()) > 0 && len(m.Name.ValueString()) <= 200
}
func invitationFields(m InvitationPolicyModel) map[string]any {
	fields := map[string]any{"name": m.Name.ValueString()}
	if !m.EnabledIdentifierVerify.IsNull() && !m.EnabledIdentifierVerify.IsUnknown() {
		fields["enabledIdentifierVerify"] = m.EnabledIdentifierVerify.ValueBool()
	}
	if !m.EnabledInfoFill.IsNull() && !m.EnabledInfoFill.IsUnknown() {
		fields["enabledInfoFill"] = m.EnabledInfoFill.ValueBool()
	}
	if !m.RegisterInfoFillMsg.IsNull() && !m.RegisterInfoFillMsg.IsUnknown() {
		fields["registerInfoFillMsg"] = m.RegisterInfoFillMsg.ValueString()
	}
	return fields
}
func (r *InvitationPolicyResource) get(ctx context.Context, id string) (InvitationPolicyModel, bool, error) {
	var m InvitationPolicyModel
	if id == "" {
		return m, false, errors.New("missing invitation policy ID")
	}
	out, err := r.send(ctx, "/api/v3/get-invitation-policy", http.MethodGet, map[string]string{"policyId": id})
	if err != nil {
		return m, false, err
	}
	if out.StatusCode == 404 {
		return m, false, nil
	}
	if out.StatusCode != 200 {
		return m, false, invitationStatus(out, nil)
	}
	var data struct {
		ID                      string  `json:"policyId"`
		Name                    string  `json:"name"`
		EnabledIdentifierVerify *bool   `json:"enabledIdentifierVerify"`
		EnabledInfoFill         *bool   `json:"enabledInfoFill"`
		RegisterInfoFillMsg     *string `json:"registerInfoFillMsg"`
	}
	if json.Unmarshal(out.Data, &data) != nil || data.ID != id || data.Name == "" || data.EnabledIdentifierVerify == nil || data.EnabledInfoFill == nil {
		return m, false, errors.New("get-invitation-policy returned missing or mismatched policy data")
	}
	m.ID = types.StringValue(id)
	m.Name = types.StringValue(data.Name)
	m.EnabledIdentifierVerify = types.BoolValue(*data.EnabledIdentifierVerify)
	m.EnabledInfoFill = types.BoolValue(*data.EnabledInfoFill)
	if data.RegisterInfoFillMsg != nil {
		m.RegisterInfoFillMsg = types.StringValue(*data.RegisterInfoFillMsg)
	}
	return m, true, nil
}
func (r *InvitationPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m InvitationPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validInvitationName(m) {
		resp.Diagnostics.AddError("Invalid invitation policy name", "name must contain 1–200 characters")
		return
	}
	out, err := r.send(ctx, "/api/v3/create-invitation-policy", http.MethodPost, invitationFields(m))
	if err != nil || out.StatusCode != 200 {
		resp.Diagnostics.AddError("Create invitation policy failed", invitationStatus(out, err).Error())
		return
	}
	var data struct {
		ID string `json:"policyId"`
	}
	if json.Unmarshal(out.Data, &data) != nil || data.ID == "" {
		resp.Diagnostics.AddError("Create invitation policy failed", "response did not contain policyId")
		return
	}
	actual, found, err := r.get(ctx, data.ID)
	if err != nil || !found {
		resp.Diagnostics.AddError("Verify invitation policy creation failed", fmt.Sprintf("Authing created invitation policy %q but readback failed (%s). Import that ID before retrying to avoid creating a duplicate.", data.ID, invitationVerificationError(err)))
		return
	}
	if !invitationConfirmed(m, actual) {
		resp.Diagnostics.AddError("Verify invitation policy creation failed", "get-invitation-policy did not confirm the configured values")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &actual)...)
}
func invitationConfirmed(planned, actual InvitationPolicyModel) bool {
	return planned.Name.Equal(actual.Name) &&
		(planned.EnabledIdentifierVerify.IsNull() || planned.EnabledIdentifierVerify.IsUnknown() || planned.EnabledIdentifierVerify.Equal(actual.EnabledIdentifierVerify)) &&
		(planned.EnabledInfoFill.IsNull() || planned.EnabledInfoFill.IsUnknown() || planned.EnabledInfoFill.Equal(actual.EnabledInfoFill)) &&
		(planned.RegisterInfoFillMsg.IsNull() || planned.RegisterInfoFillMsg.IsUnknown() || planned.RegisterInfoFillMsg.Equal(actual.RegisterInfoFillMsg))
}
func invitationVerificationError(err error) string {
	if err != nil {
		return err.Error()
	}
	return "policy not confirmed by get-invitation-policy"
}
func (r *InvitationPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m InvitationPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	actual, found, err := r.get(ctx, m.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Read invitation policy failed", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &actual)...)
}
func (r *InvitationPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var planned, prior InvitationPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validInvitationName(planned) || prior.ID.IsNull() || prior.ID.IsUnknown() || prior.ID.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid invitation policy", "name or policy ID is missing")
		return
	}
	fields := map[string]any{"policyId": prior.ID.ValueString()}
	if !planned.Name.Equal(prior.Name) {
		fields["name"] = planned.Name.ValueString()
	}
	if !planned.EnabledIdentifierVerify.Equal(prior.EnabledIdentifierVerify) && !planned.EnabledIdentifierVerify.IsNull() && !planned.EnabledIdentifierVerify.IsUnknown() {
		fields["enabledIdentifierVerify"] = planned.EnabledIdentifierVerify.ValueBool()
	}
	if !planned.EnabledInfoFill.Equal(prior.EnabledInfoFill) && !planned.EnabledInfoFill.IsNull() && !planned.EnabledInfoFill.IsUnknown() {
		fields["enabledInfoFill"] = planned.EnabledInfoFill.ValueBool()
	}
	if !planned.RegisterInfoFillMsg.Equal(prior.RegisterInfoFillMsg) && !planned.RegisterInfoFillMsg.IsNull() && !planned.RegisterInfoFillMsg.IsUnknown() {
		fields["registerInfoFillMsg"] = planned.RegisterInfoFillMsg.ValueString()
	}
	if len(fields) > 1 {
		out, err := r.send(ctx, "/api/v3/update-invitation-policy", http.MethodPost, fields)
		if err != nil || out.StatusCode != 200 {
			resp.Diagnostics.AddError("Update invitation policy failed", invitationStatus(out, err).Error())
			return
		}
	}
	actual, found, err := r.get(ctx, prior.ID.ValueString())
	if err != nil || !found {
		resp.Diagnostics.AddError("Verify invitation policy update failed", invitationVerificationError(err))
		return
	}
	if !invitationConfirmed(planned, actual) {
		resp.Diagnostics.AddError("Verify invitation policy update failed", "get-invitation-policy did not confirm the configured values")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &actual)...)
}
func (r *InvitationPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m InvitationPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := m.ID.ValueString()
	_, found, err := r.get(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Check invitation policy before deletion failed", err.Error())
		return
	}
	if !found {
		return
	}
	out, err := r.send(ctx, "/api/v3/delete-invitation-policies-batch", http.MethodPost, map[string]any{"policyIds": []string{id}})
	if err != nil {
		resp.Diagnostics.AddError("Delete invitation policy failed", err.Error())
		return
	}
	if out.StatusCode == 404 {
		_, found, err = r.get(ctx, id)
		if err == nil && !found {
			return
		}
	}
	if out.StatusCode != 200 || !membershipSuccess(out.Data) {
		resp.Diagnostics.AddError("Delete invitation policy failed", membershipMutationError(membershipEnvelope{StatusCode: out.StatusCode, Data: out.Data}, nil))
		return
	}
	_, found, err = r.get(ctx, id)
	if err != nil || found {
		if err == nil {
			err = errors.New("policy still exists after deletion")
		}
		resp.Diagnostics.AddError("Verify invitation policy deletion failed", err.Error())
	}
}
func (r *InvitationPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "policy ID must not be empty")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
