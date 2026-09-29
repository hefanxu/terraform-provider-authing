package provider

import (
	"context"
	"fmt"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
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
				Description: "Description of the group.",
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

	createReq := &dto.CreateGroupReqDto{
		Code: plan.Code.ValueString(),
		Name: plan.Name.ValueString(),
	}
	if !plan.Description.IsNull() {
		createReq.Description = plan.Description.ValueString()
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
	if res.Data.Description != "" {
		plan.Description = types.StringValue(res.Data.Description)
	}

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
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(res.Data.Code)
	state.Code = types.StringValue(res.Data.Code)
	state.Name = types.StringValue(res.Data.Name)
	if res.Data.Description != "" {
		state.Description = types.StringValue(res.Data.Description)
	} else {
		state.Description = types.StringNull()
	}

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
		Code: plan.Code.ValueString(),
		Name: plan.Name.ValueString(),
	}
	if !plan.Description.IsNull() {
		updateReq.Description = plan.Description.ValueString()
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
	if res == nil || res.StatusCode != 200 {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
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

	res := r.client.GetUserGroups(&dto.GetUserGroupsDto{
		UserId: state.UserId.ValueString(),
	})
	if res == nil || res.StatusCode != 200 {
		resp.State.RemoveResource(ctx)
		return
	}

	found := false
	for _, g := range res.Data.List {
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

	_ = r.client.RemoveGroupMembers(&dto.RemoveGroupMembersReqDto{
		Code:    state.GroupCode.ValueString(),
		UserIds: []string{state.UserId.ValueString()},
	})
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
	if res.Data.Description != "" {
		state.Description = types.StringValue(res.Data.Description)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
