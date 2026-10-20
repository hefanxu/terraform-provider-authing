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

var _ resource.Resource = &InvitationInviteeResource{}
var _ resource.ResourceWithImportState = &InvitationInviteeResource{}

func NewInvitationInviteeResource() resource.Resource { return &InvitationInviteeResource{} }

type InvitationInviteeResource struct{ client *authingapi.Client }
type InvitationInviteeModel struct {
	ID        types.String `tfsdk:"id"`
	RosterID  types.String `tfsdk:"roster_id"`
	InviteeID types.String `tfsdk:"invitee_id"`
	Name      types.String `tfsdk:"name"`
	Email     types.String `tfsdk:"email"`
	Phone     types.String `tfsdk:"phone"`
}

func (r *InvitationInviteeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_invitation_invitee"
}
func (r *InvitationInviteeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Persistent entry in an invitation roster. Does not send invitations. Name, email and phone are PII stored in Terraform state; protect the state backend. Deletion removes only the exact invitee ID.", Attributes: map[string]schema.Attribute{
		"id":         schema.StringAttribute{Computed: true, Description: "Versioned composite roster and invitee ID, used for import.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"roster_id":  schema.StringAttribute{Required: true, Description: "Parent invitation roster ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"invitee_id": schema.StringAttribute{Computed: true, Description: "Stable invitee ID returned by Authing.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":       schema.StringAttribute{Required: true, Description: "Invitee name (PII in state)."},
		"email":      schema.StringAttribute{Required: true, Description: "Invitee email (PII in state); Authing treats it case-insensitively."},
		"phone":      schema.StringAttribute{Optional: true, Description: "Invitee phone (PII in state). Omit when no phone is set."},
	}}
}
func (r *InvitationInviteeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func invitationInviteeID(roster, invitee string) string {
	b, _ := json.Marshal([2]string{roster, invitee})
	return "ii1." + base64.RawURLEncoding.EncodeToString(b)
}
func parseInvitationInviteeID(id string) ([2]string, error) {
	var parts [2]string
	if !strings.HasPrefix(id, "ii1.") {
		return parts, errors.New("expected ii1.<base64url JSON array of roster ID, invitee ID>")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, "ii1."))
	if err != nil || json.Unmarshal(b, &parts) != nil || parts[0] == "" || parts[1] == "" || invitationInviteeID(parts[0], parts[1]) != id {
		return parts, errors.New("invalid invitation invitee composite ID")
	}
	return parts, nil
}
func validInvitee(m InvitationInviteeModel) bool {
	return !m.RosterID.IsNull() && !m.RosterID.IsUnknown() && m.RosterID.ValueString() != "" && !m.Name.IsNull() && !m.Name.IsUnknown() && m.Name.ValueString() != "" && !m.Email.IsNull() && !m.Email.IsUnknown() && m.Email.ValueString() != "" && !m.Phone.IsUnknown() && (m.Phone.IsNull() || m.Phone.ValueString() != "")
}
func inviteePhone(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
func inviteeConfirmed(a, b InvitationInviteeModel) bool {
	return a.Name.Equal(b.Name) && a.Email.Equal(b.Email) && a.Phone.Equal(b.Phone)
}

type inviteeEnvelope struct {
	StatusCode int             `json:"statusCode"`
	Data       json.RawMessage `json:"data"`
}

func (r *InvitationInviteeResource) send(ctx context.Context, endpoint string, payload any) (inviteeEnvelope, error) {
	var out inviteeEnvelope
	if r.client == nil {
		return out, errors.New("Authing client is not configured")
	}
	raw, err := r.client.SendHttpRequestContext(ctx, endpoint, http.MethodPost, payload)
	if err != nil {
		return out, err
	}
	if json.Unmarshal(raw, &out) != nil || out.StatusCode == 0 {
		return out, errors.New("invalid invitation invitee response")
	}
	return out, nil
}
func inviteeStatus(out inviteeEnvelope, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("Authing invitation invitee status %d", out.StatusCode)
}

type inviteeRowData struct {
	RosterID         string `json:"rosterId"`
	InviteeID        string `json:"inviteeId"`
	Name             string `json:"name"`
	Email            string `json:"email"`
	Phone            string `json:"phone"`
	PhoneCountryCode string `json:"phoneCountryCode"`
}

func (d inviteeRowData) model() InvitationInviteeModel {
	return InvitationInviteeModel{ID: types.StringValue(invitationInviteeID(d.RosterID, d.InviteeID)), RosterID: types.StringValue(d.RosterID), InviteeID: types.StringValue(d.InviteeID), Name: types.StringValue(d.Name), Email: types.StringValue(d.Email), Phone: inviteePhone(d.Phone)}
}
func (r *InvitationInviteeResource) list(ctx context.Context, roster string) ([]inviteeRowData, error) {
	if roster == "" {
		return nil, errors.New("missing roster ID")
	}
	const limit = 50
	rows := make([]inviteeRowData, 0)
	seen := make(map[string]bool)
	for page := 1; page <= 10000; page++ {
		out, err := r.send(ctx, "/api/v3/list-invitation-invitees", map[string]any{"rosterId": roster, "page": page, "limit": limit})
		if err != nil || out.StatusCode != 200 {
			return nil, errors.New("list-invitation-invitees: " + inviteeStatus(out, err))
		}
		var data struct {
			List       *[]inviteeRowData `json:"list"`
			TotalCount *int              `json:"totalCount"`
		}
		if json.Unmarshal(out.Data, &data) != nil || data.List == nil || data.TotalCount != nil && (*data.TotalCount < 0 || *data.TotalCount < len(rows)+len(*data.List)) || len(*data.List) > limit {
			return nil, errors.New("incomplete or malformed invitation invitee page")
		}
		for _, row := range *data.List {
			if row.RosterID != roster || row.InviteeID == "" || row.Name == "" || row.Email == "" || seen[row.InviteeID] {
				return nil, errors.New("invalid, mismatched or duplicate invitation invitee list row")
			}
			seen[row.InviteeID] = true
			rows = append(rows, row)
		}
		if data.TotalCount != nil {
			if len(rows) == *data.TotalCount {
				return rows, nil
			}
			if len(*data.List) < limit {
				return nil, errors.New("incomplete invitation invitee pagination")
			}
		} else if len(*data.List) < limit {
			return rows, nil
		}
	}
	return nil, errors.New("invitation invitee pagination limit exceeded")
}
func (r *InvitationInviteeResource) get(ctx context.Context, roster, invitee string) (InvitationInviteeModel, bool, error) {
	var zero InvitationInviteeModel
	if invitee == "" {
		return zero, false, errors.New("missing invitee ID")
	}
	rows, err := r.list(ctx, roster)
	if err != nil {
		return zero, false, err
	}
	for _, row := range rows {
		if row.InviteeID == invitee {
			return row.model(), true, nil
		}
	}
	return zero, false, nil
}
func (r *InvitationInviteeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m InvitationInviteeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validInvitee(m) {
		resp.Diagnostics.AddError("Invalid invitation invitee", "roster_id, name, email must be nonempty; phone must be nonempty when set")
		return
	}
	rows, err := r.list(ctx, m.RosterID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Preflight invitation invitee failed", err.Error())
		return
	}
	for _, row := range rows {
		if strings.EqualFold(row.Email, m.Email.ValueString()) {
			resp.Diagnostics.AddError("Invitee already exists", "An invitee with this email already exists in the roster; import the existing row instead of adopting or duplicating it.")
			return
		}
	}
	fields := map[string]any{"rosterId": m.RosterID.ValueString(), "name": m.Name.ValueString(), "email": m.Email.ValueString()}
	if !m.Phone.IsNull() {
		fields["phone"] = m.Phone.ValueString()
	}
	out, err := r.send(ctx, "/api/v3/create-invitation-invitee", fields)
	if err != nil || out.StatusCode != 200 {
		resp.Diagnostics.AddError("Create invitation invitee failed", inviteeStatus(out, err)+"; inspect the roster before retrying if the request may have succeeded")
		return
	}
	var data inviteeRowData
	if json.Unmarshal(out.Data, &data) != nil || data.InviteeID == "" || data.RosterID != m.RosterID.ValueString() {
		resp.Diagnostics.AddError("Create invitation invitee failed", "Response did not contain a matching rosterId and inviteeId; inspect the roster before retrying to avoid a duplicate.")
		return
	}
	actual, found, err := r.get(ctx, data.RosterID, data.InviteeID)
	if err != nil || !found || !inviteeConfirmed(m, actual) {
		detail := "entry missing or configured fields differ"
		if err != nil {
			detail = err.Error()
		}
		resp.Diagnostics.AddError("Verify invitation invitee creation failed", fmt.Sprintf("Authing returned invitee %q in roster %q, but list readback did not confirm its fields (%s). Import %q before retrying to avoid a duplicate.", data.InviteeID, data.RosterID, detail, invitationInviteeID(data.RosterID, data.InviteeID)))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &actual)...)
}
func (r *InvitationInviteeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m InvitationInviteeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	parts, err := parseInvitationInviteeID(m.ID.ValueString())
	if err != nil || !m.RosterID.IsNull() && !m.RosterID.IsUnknown() && m.RosterID.ValueString() != "" && m.RosterID.ValueString() != parts[0] || !m.InviteeID.IsNull() && !m.InviteeID.IsUnknown() && m.InviteeID.ValueString() != "" && m.InviteeID.ValueString() != parts[1] {
		resp.Diagnostics.AddError("Invalid invitation invitee state", "Composite ID conflicts with roster_id or invitee_id")
		return
	}
	actual, found, err := r.get(ctx, parts[0], parts[1])
	if err != nil {
		resp.Diagnostics.AddError("Read invitation invitee failed", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &actual)...)
}
func (r *InvitationInviteeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior InvitationInviteeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	parts, err := parseInvitationInviteeID(prior.ID.ValueString())
	if err != nil || !validInvitee(plan) || plan.RosterID.ValueString() != parts[0] || prior.RosterID.ValueString() != parts[0] || prior.InviteeID.ValueString() != parts[1] {
		resp.Diagnostics.AddError("Invalid invitation invitee update", "Invalid or changed roster/invitee identity or fields")
		return
	}
	rows, err := r.list(ctx, parts[0])
	if err != nil {
		resp.Diagnostics.AddError("Check invitation invitee before update failed", err.Error())
		return
	}
	var row inviteeRowData
	found := false
	for _, candidate := range rows {
		if candidate.InviteeID == parts[1] {
			row, found = candidate, true
			break
		}
	}
	if !found {
		resp.Diagnostics.AddError("Check invitation invitee before update failed", "Exact invitee ID was not confirmed")
		return
	}
	remote := row.model()
	if !strings.EqualFold(plan.Email.ValueString(), remote.Email.ValueString()) {
		for _, row := range rows {
			if row.InviteeID != parts[1] && strings.EqualFold(row.Email, plan.Email.ValueString()) {
				resp.Diagnostics.AddError("Invitee email already exists", "The requested email is already present in this roster.")
				return
			}
		}
	}
	if !inviteeConfirmed(plan, remote) {
		phone := ""
		if !plan.Phone.IsNull() {
			phone = plan.Phone.ValueString()
		}
		fields := map[string]any{"rosterId": parts[0], "inviteeId": parts[1], "name": plan.Name.ValueString(), "email": plan.Email.ValueString(), "phone": phone}
		if row.PhoneCountryCode != "" {
			fields["phoneCountryCode"] = row.PhoneCountryCode
		}
		out, err := r.send(ctx, "/api/v3/edit-invitation-invitee", fields)
		if err != nil || out.StatusCode != 200 || !membershipSuccess(out.Data) {
			resp.Diagnostics.AddError("Edit invitation invitee failed", inviteeStatus(out, err)+" or data.success was not true")
			return
		}
	}
	actual, found, err := r.get(ctx, parts[0], parts[1])
	if err != nil || !found || !inviteeConfirmed(plan, actual) {
		resp.Diagnostics.AddError("Verify invitation invitee update failed", "List readback did not confirm the exact invitee and configured fields; refresh before retrying.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &actual)...)
}
func (r *InvitationInviteeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m InvitationInviteeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	parts, err := parseInvitationInviteeID(m.ID.ValueString())
	if err != nil || m.RosterID.ValueString() != parts[0] || m.InviteeID.ValueString() != parts[1] {
		resp.Diagnostics.AddError("Invalid invitation invitee state", "Composite ID conflicts with roster_id or invitee_id")
		return
	}
	_, found, err := r.get(ctx, parts[0], parts[1])
	if err != nil {
		resp.Diagnostics.AddError("Check invitation invitee before deletion failed", err.Error())
		return
	}
	if !found {
		return
	}
	out, err := r.send(ctx, "/api/v3/batch-delete-invitation-invitees", map[string]any{"rosterId": parts[0], "inviteeIds": []string{parts[1]}})
	if err != nil || out.StatusCode != 200 || !membershipSuccess(out.Data) {
		resp.Diagnostics.AddError("Delete invitation invitee failed", inviteeStatus(out, err)+" or data.success was not true")
		return
	}
	_, found, err = r.get(ctx, parts[0], parts[1])
	if err != nil || found {
		resp.Diagnostics.AddError("Verify invitation invitee deletion failed", "Exact invitee absence was not confirmed by complete list readback.")
	}
}
func (r *InvitationInviteeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := parseInvitationInviteeID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("roster_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("invitee_id"), parts[1])...)
}
