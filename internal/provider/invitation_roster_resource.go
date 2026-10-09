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

var _ resource.Resource = &InvitationRosterResource{}
var _ resource.ResourceWithImportState = &InvitationRosterResource{}

func NewInvitationRosterResource() resource.Resource { return &InvitationRosterResource{} }

type InvitationRosterResource struct{ client *authingapi.Client }
type InvitationRosterModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	PolicyID types.String `tfsdk:"policy_id"`
}

func (r *InvitationRosterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_invitation_roster"
}
func (r *InvitationRosterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Persistent invitation roster metadata; does not send invitations or manage roster users.", Attributes: map[string]schema.Attribute{
		"id":        schema.StringAttribute{Computed: true, Description: "Stable roster ID; import using this ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":      schema.StringAttribute{Required: true, Description: "Roster name."},
		"policy_id": schema.StringAttribute{Optional: true, Description: "Associated invitation policy ID. Removing it unbinds the policy from this roster."},
	}}
}
func (r *InvitationRosterResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

type rosterEnvelope struct {
	StatusCode int             `json:"statusCode"`
	Data       json.RawMessage `json:"data"`
}

func (r *InvitationRosterResource) send(ctx context.Context, endpoint, method string, payload any) (rosterEnvelope, error) {
	var out rosterEnvelope
	if r.client == nil {
		return out, errors.New("Authing client is not configured")
	}
	raw, err := r.client.SendHttpRequestContext(ctx, endpoint, method, payload)
	if err != nil {
		return out, err
	}
	if json.Unmarshal(raw, &out) != nil {
		return out, errors.New("invalid invitation roster response")
	}
	return out, nil
}
func rosterStatus(out rosterEnvelope, err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("Authing invitation roster status %d", out.StatusCode)
}
func validRosterName(m InvitationRosterModel) bool {
	return !m.Name.IsNull() && !m.Name.IsUnknown() && m.Name.ValueString() != ""
}
func rosterPolicy(m InvitationRosterModel) (string, error) {
	if m.PolicyID.IsUnknown() {
		return "", errors.New("policy_id is unknown")
	}
	if m.PolicyID.IsNull() {
		return "", nil
	}
	if m.PolicyID.ValueString() == "" {
		return "", errors.New("policy_id must not be empty; omit it to unbind")
	}
	return m.PolicyID.ValueString(), nil
}
func (r *InvitationRosterResource) get(ctx context.Context, id string) (InvitationRosterModel, bool, error) {
	var m InvitationRosterModel
	if id == "" {
		return m, false, errors.New("missing invitation roster ID")
	}
	out, err := r.send(ctx, "/api/v3/get-invitation-roster", http.MethodGet, map[string]any{"rosterId": id, "withAssignedPolicy": true})
	if err != nil {
		return m, false, err
	}
	if out.StatusCode == 404 {
		return m, false, nil
	}
	if out.StatusCode != 200 {
		return m, false, rosterStatus(out, nil)
	}
	var data struct {
		ID             string  `json:"rosterId"`
		Name           string  `json:"name"`
		PolicyID       *string `json:"policyId"`
		AssignedPolicy *struct {
			PolicyID string `json:"policyId"`
		} `json:"assignedPolicy"`
	}
	if json.Unmarshal(out.Data, &data) != nil || data.ID != id || data.Name == "" {
		return m, false, errors.New("get-invitation-roster returned missing or mismatched roster data")
	}
	if data.PolicyID != nil && data.AssignedPolicy != nil && *data.PolicyID != data.AssignedPolicy.PolicyID {
		return m, false, errors.New("get-invitation-roster returned conflicting policy IDs")
	}
	m.ID = types.StringValue(id)
	m.Name = types.StringValue(data.Name)
	m.PolicyID = types.StringNull()
	policyID := data.PolicyID
	if policyID == nil && data.AssignedPolicy != nil {
		policyID = &data.AssignedPolicy.PolicyID
	}
	if policyID != nil && *policyID != "" {
		m.PolicyID = types.StringValue(*policyID)
	}
	return m, true, nil
}
func rosterConfirmed(planned, actual InvitationRosterModel) bool {
	return planned.Name.Equal(actual.Name) && planned.PolicyID.Equal(actual.PolicyID)
}
func rosterVerificationError(err error) string {
	if err != nil {
		return err.Error()
	}
	return "roster not confirmed by get-invitation-roster"
}
func (r *InvitationRosterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m InvitationRosterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	policy, err := rosterPolicy(m)
	if !validRosterName(m) || err != nil {
		resp.Diagnostics.AddError("Invalid invitation roster", "name must not be empty and policy_id, when set, must be nonempty and known")
		return
	}
	out, err := r.send(ctx, "/api/v3/create-invitation-roster", http.MethodPost, map[string]any{"name": m.Name.ValueString()})
	if err != nil || out.StatusCode != 200 {
		resp.Diagnostics.AddError("Create invitation roster failed", rosterStatus(out, err).Error())
		return
	}
	var data struct {
		ID string `json:"rosterId"`
	}
	if json.Unmarshal(out.Data, &data) != nil || data.ID == "" {
		resp.Diagnostics.AddError("Create invitation roster failed", "response did not contain rosterId; inspect Authing before retrying to avoid a duplicate")
		return
	}
	if policy != "" {
		out, err = r.send(ctx, "/api/v3/update-invitation-roster", http.MethodPost, map[string]any{"rosterId": data.ID, "policyId": policy})
		if err != nil || out.StatusCode != 200 {
			resp.Diagnostics.AddError("Associate invitation policy failed", fmt.Sprintf("Authing created roster %q but policy association failed (%v). Import this ID before retrying.", data.ID, rosterStatus(out, err)))
			return
		}
	}
	actual, found, err := r.get(ctx, data.ID)
	if err != nil || !found || !rosterConfirmed(m, actual) {
		detail := rosterVerificationError(err)
		if err == nil && found {
			detail = "get-invitation-roster did not confirm the configured values"
		}
		resp.Diagnostics.AddError("Verify invitation roster creation failed", fmt.Sprintf("Authing created roster %q but readback failed (%s). Import this ID before retrying to avoid creating a duplicate.", data.ID, detail))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &actual)...)
}
func (r *InvitationRosterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m InvitationRosterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	actual, found, err := r.get(ctx, m.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Read invitation roster failed", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &actual)...)
}
func (r *InvitationRosterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var planned, prior InvitationRosterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	policy, err := rosterPolicy(planned)
	if !validRosterName(planned) || err != nil || prior.ID.IsNull() || prior.ID.IsUnknown() || prior.ID.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid invitation roster", "name, policy_id, or roster ID is invalid")
		return
	}
	id := prior.ID.ValueString()
	remote, found, err := r.get(ctx, id)
	if err != nil || !found {
		resp.Diagnostics.AddError("Check invitation roster before update failed", rosterVerificationError(err))
		return
	}
	if !remote.PolicyID.Equal(prior.PolicyID) {
		resp.Diagnostics.AddError("Invitation roster policy changed", "Refresh state before changing policy association to avoid unbinding a different policy.")
		return
	}
	if !planned.Name.Equal(remote.Name) || policy != "" && !planned.PolicyID.Equal(remote.PolicyID) {
		fields := map[string]any{"rosterId": id}
		if !planned.Name.Equal(remote.Name) {
			fields["name"] = planned.Name.ValueString()
		}
		if policy != "" && !planned.PolicyID.Equal(remote.PolicyID) {
			fields["policyId"] = policy
		}
		if len(fields) > 1 {
			out, err := r.send(ctx, "/api/v3/update-invitation-roster", http.MethodPost, fields)
			if err != nil || out.StatusCode != 200 {
				resp.Diagnostics.AddError("Update invitation roster failed", rosterStatus(out, err).Error())
				return
			}
		}
	}
	if policy == "" && !remote.PolicyID.IsNull() {
		out, err := r.send(ctx, "/api/v3/update-invitation-roster-batch-unbind-policy", http.MethodPost, map[string]any{"policyId": remote.PolicyID.ValueString(), "rosterIds": []string{id}})
		if err != nil || out.StatusCode != 200 || !membershipSuccess(out.Data) {
			resp.Diagnostics.AddError("Unbind invitation roster policy failed", rosterStatus(out, err).Error())
			return
		}
	}
	actual, found, err := r.get(ctx, id)
	if err != nil || !found || !rosterConfirmed(planned, actual) {
		detail := rosterVerificationError(err)
		if err == nil && found {
			detail = "get-invitation-roster did not confirm the configured values"
		}
		resp.Diagnostics.AddError("Verify invitation roster update failed", detail)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &actual)...)
}
func (r *InvitationRosterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m InvitationRosterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := m.ID.ValueString()
	_, found, err := r.get(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Check invitation roster before deletion failed", err.Error())
		return
	}
	if !found {
		return
	}
	out, err := r.send(ctx, "/api/v3/delete-invitation-roster", http.MethodPost, map[string]any{"id": id})
	if err != nil || out.StatusCode != 200 || !membershipSuccess(out.Data) {
		detail := rosterStatus(out, err).Error()
		if err == nil && out.StatusCode == 200 {
			detail = "Authing did not return data.success=true"
		}
		resp.Diagnostics.AddError("Delete invitation roster failed", detail)
		return
	}
	_, found, err = r.get(ctx, id)
	if err != nil || found {
		if err == nil {
			err = errors.New("roster still exists after deletion")
		}
		resp.Diagnostics.AddError("Verify invitation roster deletion failed", err.Error())
	}
}
func (r *InvitationRosterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "roster ID must not be empty")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
