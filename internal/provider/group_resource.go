package provider

import (
	"context"
	"encoding/json"
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
				Description: "Description of the group; omitted values are sent as an empty string (required by Authing).",
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

func (r *GroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan GroupModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Type.IsNull() || plan.Type.IsUnknown() || plan.Type.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid group type", "type must be a nonempty Authing group type; no default is documented")
		return
	}

	createReq := &dto.CreateGroupReqDto{
		Code:        plan.Code.ValueString(),
		Name:        plan.Name.ValueString(),
		Type:        plan.Type.ValueString(),
		Description: plan.Description.ValueString(),
	}

	res := r.client.CreateGroup(createReq)
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to create Authing group", errMsg)
		return
	}

	plan.ID = types.StringValue(res.Data.Code)
	plan.Code = types.StringValue(res.Data.Code)
	plan.Name = types.StringValue(res.Data.Name)
	plan.Description = types.StringValue(res.Data.Description)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *GroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state GroupModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.GetGroup(&dto.GetGroupDto{
		Code: state.Code.ValueString(),
	})
	if res != nil && res.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		resp.Diagnostics.AddError("Failed to read Authing group", "Authing returned an invalid or unsuccessful response")
		return
	}

	state.ID = types.StringValue(res.Data.Code)
	state.Code = types.StringValue(res.Data.Code)
	state.Name = types.StringValue(res.Data.Name)
	if res.Data.Type != "" {
		state.Type = types.StringValue(res.Data.Type)
	}
	state.Description = types.StringValue(res.Data.Description)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *GroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan GroupModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := &dto.UpdateGroupReqDto{
		Code:        plan.Code.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
	}

	res := r.client.UpdateGroup(updateReq)
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to update Authing group", errMsg)
		return
	}

	plan.Description = types.StringValue(plan.Description.ValueString())
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *GroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state GroupModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.DeleteGroupsBatch(&dto.DeleteGroupsReqDto{
		CodeList: []string{state.Code.ValueString()},
	})
	if res != nil && res.StatusCode == 404 {
		return
	}
	if res == nil || res.StatusCode != 200 || !res.Data.Success {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s success=%t", res.StatusCode, res.Message, res.Data.Success)
		}
		resp.Diagnostics.AddError("Failed to delete Authing group", errMsg)
		return
	}
}

func (r *GroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("code"), req, resp)
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
