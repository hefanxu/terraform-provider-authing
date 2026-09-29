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

var _ resource.Resource = &DataPolicyAssignmentResource{}
var _ resource.ResourceWithImportState = &DataPolicyAssignmentResource{}

func NewDataPolicyAssignmentResource() resource.Resource { return &DataPolicyAssignmentResource{} }

type DataPolicyAssignmentResource struct{ client *authingapi.Client }
type DataPolicyAssignmentModel struct {
	ID         types.String `tfsdk:"id"`
	PolicyID   types.String `tfsdk:"policy_id"`
	TargetType types.String `tfsdk:"target_type"`
	TargetID   types.String `tfsdk:"target_id"`
}

func (r *DataPolicyAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_policy_assignment"
}
func (r *DataPolicyAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Manages one Authing data policy authorization for one subject. Changes to any identifier replace the assignment.", Attributes: map[string]schema.Attribute{
		"id":          schema.StringAttribute{Computed: true, Description: "Versioned base64url-encoded composite policy/subject identity.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"policy_id":   schema.StringAttribute{Required: true, Description: "Data policy ID.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"target_type": schema.StringAttribute{Required: true, Description: "Subject type: USER, ORG, GROUP, ROLE, or PROGRAMMATIC_ACCOUNT.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"target_id":   schema.StringAttribute{Required: true, Description: "Subject identifier.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
	}}
}
func (r *DataPolicyAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// The versioned base64url encoding is unambiguous even if identifiers contain separators.
func assignmentID(policy, typ, target string) string {
	b, _ := json.Marshal([3]string{policy, typ, target})
	return "v1." + base64.RawURLEncoding.EncodeToString(b)
}
func parseAssignmentID(id string) ([3]string, error) {
	var parts [3]string
	if !strings.HasPrefix(id, "v1.") {
		return parts, errors.New("expected v1.<base64url JSON array of policy ID, target type, target ID>")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, "v1."))
	if err != nil || json.Unmarshal(b, &parts) != nil || len(parts[0]) == 0 || len(parts[2]) == 0 || !validAssignmentType(parts[1]) || assignmentID(parts[0], parts[1], parts[2]) != id {
		return parts, errors.New("invalid data policy assignment import ID")
	}
	return parts, nil
}
func validAssignment(m DataPolicyAssignmentModel) bool {
	return !m.PolicyID.IsNull() && !m.PolicyID.IsUnknown() && m.PolicyID.ValueString() != "" &&
		!m.TargetID.IsNull() && !m.TargetID.IsUnknown() && m.TargetID.ValueString() != "" &&
		!m.TargetType.IsNull() && !m.TargetType.IsUnknown() && validAssignmentType(m.TargetType.ValueString())
}

func validAssignmentType(t string) bool {
	switch t {
	case "USER", "ORG", "GROUP", "ROLE", "PROGRAMMATIC_ACCOUNT":
		return true
	}
	return false
}

type assignmentEnvelope struct {
	StatusCode int             `json:"statusCode"`
	Message    string          `json:"message"`
	Data       json.RawMessage `json:"data"`
}

func (r *DataPolicyAssignmentResource) send(ctx context.Context, endpoint, method string, payload any) (assignmentEnvelope, error) {
	var out assignmentEnvelope
	if r.client == nil {
		return out, errors.New("Authing client is not configured")
	}
	b, err := r.client.SendHttpRequestContext(ctx, endpoint, method, payload)
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("invalid Authing response: %w", err)
	}
	return out, nil
}
func assignmentError(out assignmentEnvelope, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("Authing status %d: %s", out.StatusCode, out.Message)
}

// A list failure is never evidence that the authorization is absent. Query every
// page indicated by totalCount, but stop on an empty page to avoid infinite loops
// when concurrent revocations shrink the list.
func (r *DataPolicyAssignmentResource) exists(ctx context.Context, m DataPolicyAssignmentModel) (bool, error) {
	if !validAssignment(m) {
		return false, errors.New("invalid policy ID, subject ID, or subject type in assignment state")
	}
	const limit = 50
	for page, seen := 1, 0; ; page++ {
		out, err := r.send(ctx, "/api/v3/list-data-policy-targets", http.MethodGet, map[string]any{"policyId": m.PolicyID.ValueString(), "page": page, "limit": limit})
		if err != nil || out.StatusCode != 200 {
			return false, errors.New(assignmentError(out, err))
		}
		var data struct {
			TotalCount *int `json:"totalCount"`
			List       []struct {
				Identifier string `json:"targetIdentifier"`
				Type       string `json:"targetType"`
			} `json:"list"`
		}
		if err = json.Unmarshal(out.Data, &data); err != nil || data.List == nil || data.TotalCount != nil && *data.TotalCount < 0 {
			return false, errors.New("invalid data policy targets response")
		}
		for _, v := range data.List {
			if v.Identifier == m.TargetID.ValueString() && v.Type == m.TargetType.ValueString() {
				return true, nil
			}
		}
		seen += len(data.List)
		if data.TotalCount != nil && seen < *data.TotalCount && len(data.List) < limit {
			return false, errors.New("incomplete data policy targets pagination")
		}
		if data.TotalCount != nil && seen >= *data.TotalCount || len(data.List) == 0 || data.TotalCount == nil && len(data.List) < limit {
			return false, nil
		}
	}
}
func (r *DataPolicyAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m DataPolicyAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !validAssignment(m) {
		resp.Diagnostics.AddError("Invalid data policy assignment", "policy_id and target_id must be nonempty, and target_type must be USER, ORG, GROUP, ROLE, or PROGRAMMATIC_ACCOUNT")
		return
	}
	out, err := r.send(ctx, "/api/v3/authorize-data-policies", http.MethodPost, map[string]any{"policyIds": []string{m.PolicyID.ValueString()}, "targetList": []map[string]string{{"id": m.TargetID.ValueString(), "type": m.TargetType.ValueString()}}})
	if err != nil || out.StatusCode != 200 {
		resp.Diagnostics.AddError("Authorize data policy failed", assignmentError(out, err))
		return
	}
	found, err := r.exists(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Verify data policy assignment failed", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Verify data policy assignment failed", "Authorization is not present in the policy target list")
		return
	}
	m.ID = types.StringValue(assignmentID(m.PolicyID.ValueString(), m.TargetType.ValueString(), m.TargetID.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *DataPolicyAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m DataPolicyAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.exists(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Read data policy assignment failed", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	m.ID = types.StringValue(assignmentID(m.PolicyID.ValueString(), m.TargetType.ValueString(), m.TargetID.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *DataPolicyAssignmentResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Data policy assignment update is unsupported", "All assignment identifiers require replacement")
}
func (r *DataPolicyAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m DataPolicyAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.exists(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Check data policy assignment before revoke failed", err.Error())
		return
	}
	if !found {
		return
	}
	out, err := r.send(ctx, "/api/v3/revoke-data-policy", http.MethodPost, map[string]string{"policyId": m.PolicyID.ValueString(), "targetIdentifier": m.TargetID.ValueString(), "targetType": m.TargetType.ValueString()})
	if err != nil || out.StatusCode != 200 && out.StatusCode != 404 {
		resp.Diagnostics.AddError("Revoke data policy failed", assignmentError(out, err))
		return
	}
	if out.StatusCode == 200 {
		found, err = r.exists(ctx, m)
		if err != nil {
			resp.Diagnostics.AddError("Verify data policy revocation failed", err.Error())
			return
		}
		if found {
			resp.Diagnostics.AddError("Verify data policy revocation failed", "Authorization remains present after revoke")
		}
	}
}
func (r *DataPolicyAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := parseAssignmentID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	for key, value := range map[string]string{"id": req.ID, "policy_id": parts[0], "target_type": parts[1], "target_id": parts[2]} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(key), value)...)
	}
}
