package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

// --- Group Resource ---

var _ resource.Resource = &GroupResource{}
var _ resource.ResourceWithImportState = &GroupResource{}

func NewGroupResource() resource.Resource {
	return &GroupResource{}
}

type GroupResource struct {
	client *authingapi.Client
}

type GroupModel struct {
	ID          types.String `tfsdk:"id"`
	Code        types.String `tfsdk:"code"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Type        types.String `tfsdk:"type"`
}

type nonemptyGroupTypeValidator struct{}

func (nonemptyGroupTypeValidator) Description(context.Context) string {
	return "Group type must be nonempty; Authing documents no default or enum."
}
func (v nonemptyGroupTypeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (nonemptyGroupTypeValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !req.ConfigValue.IsNull() && !req.ConfigValue.IsUnknown() && strings.TrimSpace(req.ConfigValue.ValueString()) == "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid group type", "Provide a nonempty Authing group type; the API does not document a default or enum.")
	}
}

func (r *GroupResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

func (r *GroupResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Authing User Group.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"code": schema.StringAttribute{
				Required:    true,
				Description: "Unique code / identifier for the group.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the group.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Description; omitted on create sends an empty string, omitted after refresh adopts the remote value. Configure an empty string to clear.",
			},
			"type": schema.StringAttribute{
				Required:      true,
				Description:   "Authing group type (for example, static). Explicitly required; the API does not document a default or enum.",
				Validators:    []validator.String{nonemptyGroupTypeValidator{}},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *GroupResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*authingapi.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *authingapi.Client")
		return
	}
	r.client = client
}

// Keep presence information: SDK zero values cannot distinguish missing fields.
type groupRemote struct {
	Code        string  `json:"code"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Type        *string `json:"type"`
}

func groupSend(ctx context.Context, c *authingapi.Client, endpoint, method string, body any) (publicEnvelope, error) {
	return publicSend(ctx, c, endpoint, method, body)
}
func groupGet(ctx context.Context, c *authingapi.Client, code string) (groupRemote, bool, error) {
	var v groupRemote
	if code == "" {
		return v, false, errors.New("empty group code")
	}
	out, err := groupSend(ctx, c, "/api/v3/get-group", "GET", map[string]string{"code": code})
	if err != nil {
		return v, false, err
	}
	if out.StatusCode == 404 {
		return v, false, nil
	}
	if out.StatusCode != 200 {
		return v, false, fmt.Errorf("Authing status %d", out.StatusCode)
	}
	if json.Unmarshal(out.Data, &v) != nil || v.Code != code || v.Name == nil || v.Description == nil || v.Type == nil || *v.Type == "" {
		return v, false, errors.New("group GET returned missing fields or mismatched code")
	}
	return v, true, nil
}
func groupApply(m *GroupModel, v groupRemote) {
	m.ID = types.StringValue(v.Code)
	m.Code = types.StringValue(v.Code)
	m.Name = types.StringValue(*v.Name)
	m.Description = types.StringValue(*v.Description)
	m.Type = types.StringValue(*v.Type)
}
func groupVerify(m GroupModel, v groupRemote) error {
	if v.Code != m.Code.ValueString() || *v.Name != m.Name.ValueString() || *v.Type != m.Type.ValueString() || *v.Description != m.Description.ValueString() {
		return errors.New("configured group fields not confirmed by exact-code GET")
	}
	return nil
}
func groupWrite(ctx context.Context, c *authingapi.Client, endpoint string, m GroupModel, create bool) error {
	body := map[string]any{"code": m.Code.ValueString(), "name": m.Name.ValueString(), "description": m.Description.ValueString()}
	if create {
		body["type"] = m.Type.ValueString()
	}
	out, err := groupSend(ctx, c, endpoint, "POST", body)
	if err != nil {
		return err
	}
	var v groupRemote
	if out.StatusCode != 200 || json.Unmarshal(out.Data, &v) != nil || v.Code != m.Code.ValueString() {
		return errors.New("group write failed or returned mismatched code")
	}
	return nil
}
func groupIdentity(m GroupModel) error {
	if m.ID.ValueString() == "" || m.ID.ValueString() != m.Code.ValueString() {
		return errors.New("state ID and group code must match")
	}
	return nil
}
func (r *GroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m GroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if strings.TrimSpace(m.Type.ValueString()) == "" || m.Code.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid group", "code and type must be nonempty")
		return
	}
	if err := groupWrite(ctx, r.client, "/api/v3/create-group", m, true); err != nil {
		resp.Diagnostics.AddError("Create group failed", err.Error())
		return
	}
	v, found, err := groupGet(ctx, r.client, m.Code.ValueString())
	if err != nil || !found {
		resp.Diagnostics.AddError("Verify group creation failed", fmt.Sprintf("Created group code %q but readback failed. Import that code before retrying to avoid duplication.", m.Code.ValueString()))
		return
	}
	if err := groupVerify(m, v); err != nil {
		resp.Diagnostics.AddError("Verify group creation failed", fmt.Sprintf("Created group code %q but %s. Import before retrying.", m.Code.ValueString(), err))
		return
	}
	groupApply(&m, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *GroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m GroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Import sets both; tolerate legacy imported state whose id is not yet known.
	if !m.ID.IsNull() && !m.ID.IsUnknown() && m.ID.ValueString() != m.Code.ValueString() {
		resp.Diagnostics.AddError("Invalid group identity", "state ID and code differ")
		return
	}
	v, found, err := groupGet(ctx, r.client, m.Code.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Read group failed", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	groupApply(&m, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *GroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m GroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := groupIdentity(m); err != nil {
		resp.Diagnostics.AddError("Invalid group identity", err.Error())
		return
	}
	if err := groupWrite(ctx, r.client, "/api/v3/update-group", m, false); err != nil {
		resp.Diagnostics.AddError("Update group failed", err.Error())
		return
	}
	v, found, err := groupGet(ctx, r.client, m.Code.ValueString())
	if err != nil || !found {
		resp.Diagnostics.AddError("Verify group update failed", "exact-code GET failed")
		return
	}
	if err := groupVerify(m, v); err != nil {
		resp.Diagnostics.AddError("Verify group update failed", err.Error())
		return
	}
	groupApply(&m, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *GroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m GroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := groupIdentity(m); err != nil {
		resp.Diagnostics.AddError("Invalid group identity", err.Error())
		return
	}
	_, found, err := groupGet(ctx, r.client, m.Code.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Check group before deletion failed", err.Error())
		return
	}
	if !found {
		return
	}
	out, err := groupSend(ctx, r.client, "/api/v3/delete-groups-batch", "POST", map[string]any{"codeList": []string{m.Code.ValueString()}})
	if err != nil || out.StatusCode != 200 || !membershipSuccess(out.Data) {
		resp.Diagnostics.AddError("Delete group failed", "delete response unsuccessful")
		return
	}
	_, found, err = groupGet(ctx, r.client, m.Code.ValueString())
	if err != nil || found {
		resp.Diagnostics.AddError("Verify group deletion failed", "absence not confirmed by exact-code GET")
	}
}
func (r *GroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("code"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// --- Group Member Resource ---

var _ resource.Resource = &GroupMemberResource{}

func NewGroupMemberResource() resource.Resource {
	return &GroupMemberResource{}
}

type GroupMemberResource struct {
	client *authingapi.Client
}

type GroupMemberModel struct {
	ID        types.String `tfsdk:"id"`
	GroupCode types.String `tfsdk:"group_code"`
	UserId    types.String `tfsdk:"user_id"`
}

func (r *GroupMemberResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group_member"
}

func (r *GroupMemberResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Binds a user to an Authing group.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"group_code": schema.StringAttribute{
				Required:    true,
				Description: "Group code.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_id": schema.StringAttribute{
				Required:    true,
				Description: "User ID to add to the group.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *GroupMemberResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*authingapi.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *authingapi.Client")
		return
	}
	r.client = client
}

func (r *GroupMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan GroupMemberModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.AddGroupMembers(&dto.AddGroupMembersReqDto{
		Code:    plan.GroupCode.ValueString(),
		UserIds: []string{plan.UserId.ValueString()},
	})
	if res == nil || res.StatusCode != 200 {
		resp.Diagnostics.AddError("Failed to add user to group", "Error response from Authing")
		return
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s:%s", plan.GroupCode.ValueString(), plan.UserId.ValueString()))
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *GroupMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state GroupMemberModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The SDK DTO erases absent lists and totals, so inspect the envelope.
	body, err := r.client.SendHttpRequestContext(ctx, "/api/v3/get-user-groups", "GET", &dto.GetUserGroupsDto{UserId: state.UserId.ValueString()})
	var res struct {
		StatusCode int `json:"statusCode"`
		Data       *struct {
			TotalCount *int `json:"totalCount"`
			List       *[]struct {
				Code string `json:"code"`
			} `json:"list"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &res) == nil && res.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil || res.StatusCode != 200 || res.Data == nil || res.Data.List == nil || res.Data.TotalCount == nil || *res.Data.TotalCount < 0 || *res.Data.TotalCount != len(*res.Data.List) {
		resp.Diagnostics.AddError("Failed to read group member", "Authing returned an invalid or incomplete membership list")
		return
	}

	found := false
	for _, g := range *res.Data.List {
		if g.Code == "" {
			resp.Diagnostics.AddError("Failed to read group member", "Authing returned a group without a code")
			return
		}
		if g.Code == state.GroupCode.ValueString() {
			found = true
			break
		}
	}

	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *GroupMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
}

func (r *GroupMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state GroupMemberModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.RemoveGroupMembers(&dto.RemoveGroupMembersReqDto{
		Code:    state.GroupCode.ValueString(),
		UserIds: []string{state.UserId.ValueString()},
	})
	if res == nil || res.StatusCode != 404 && (res.StatusCode != 200 || !res.Data.Success) {
		resp.Diagnostics.AddError("Failed to remove group member", "Authing returned an invalid or unsuccessful response")
	}
}

// --- Group Data Source ---

var _ datasource.DataSource = &GroupDataSource{}

func NewGroupDataSource() datasource.DataSource {
	return &GroupDataSource{}
}

type GroupDataSource struct {
	client *authingapi.Client
}

func (d *GroupDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

func (d *GroupDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Fetches details of an Authing user group.",
		Attributes: map[string]dschema.Attribute{
			"id": dschema.StringAttribute{
				Computed: true,
			},
			"code": dschema.StringAttribute{
				Required:    true,
				Description: "The group code.",
			},
			"name": dschema.StringAttribute{
				Computed:    true,
				Description: "Group name.",
			},
			"description": dschema.StringAttribute{
				Computed:    true,
				Description: "Group description.",
			},
			"type": dschema.StringAttribute{
				Computed:    true,
				Description: "Group type.",
			},
		},
	}
}

func (d *GroupDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*authingapi.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", "Expected *authingapi.Client")
		return
	}
	d.client = client
}

func (d *GroupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state GroupModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := d.client.GetGroup(&dto.GetGroupDto{
		Code: state.Code.ValueString(),
	})
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		resp.Diagnostics.AddError("Authing Group Not Found", "Failed to retrieve group by code.")
		return
	}

	state.ID = types.StringValue(res.Data.Code)
	state.Name = types.StringValue(res.Data.Name)
	state.Type = types.StringValue(res.Data.Type)
	if res.Data.Description != "" {
		state.Description = types.StringValue(res.Data.Description)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
