package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

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

// --- Organization Resource ---

var _ resource.Resource = &OrganizationResource{}
var _ resource.ResourceWithImportState = &OrganizationResource{}

func NewOrganizationResource() resource.Resource {
	return &OrganizationResource{}
}

type OrganizationResource struct {
	client *authingapi.Client
}

type OrganizationModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationCode types.String `tfsdk:"organization_code"`
	OrganizationName types.String `tfsdk:"organization_name"`
	Description      types.String `tfsdk:"description"`
}

func (r *OrganizationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *OrganizationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Authing Organization. Deletion is refused if child departments exist or their status cannot be confirmed; Authing deletes the entire organization tree.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_code": schema.StringAttribute{
				Required:    true,
				Description: "Unique code for the organization.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"organization_name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the organization.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Description of the organization.",
			},
		},
	}
}

func (r *OrganizationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *OrganizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := &dto.CreateOrganizationReqDto{
		OrganizationCode: plan.OrganizationCode.ValueString(),
		OrganizationName: plan.OrganizationName.ValueString(),
		Metadata:         map[string]any{},
	}
	if plan.OrganizationCode.IsNull() || plan.OrganizationCode.IsUnknown() || plan.OrganizationCode.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid organization code", "organization_code must be nonempty")
		return
	}
	if !plan.Description.IsNull() {
		createReq.Description = plan.Description.ValueString()
	}

	res := r.client.CreateOrganization(createReq)
	if res == nil || res.StatusCode != 200 || res.Data.OrganizationCode == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to create Authing organization", errMsg)
		return
	}

	plan.ID = types.StringValue(res.Data.OrganizationCode)
	plan.OrganizationCode = types.StringValue(res.Data.OrganizationCode)
	plan.OrganizationName = types.StringValue(res.Data.OrganizationName)
	if res.Data.Description != "" {
		plan.Description = types.StringValue(res.Data.Description)
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *OrganizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.GetOrganization(&dto.GetOrganizationDto{
		OrganizationCode: state.OrganizationCode.ValueString(),
	})
	if res != nil && res.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if res == nil || res.StatusCode != 200 || res.Data.OrganizationCode == "" {
		resp.Diagnostics.AddError("Failed to read Authing organization", "Authing returned an invalid or unsuccessful response")
		return
	}

	state.ID = types.StringValue(res.Data.OrganizationCode)
	state.OrganizationName = types.StringValue(res.Data.OrganizationName)
	if res.Data.Description != "" {
		state.Description = types.StringValue(res.Data.Description)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *OrganizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan OrganizationModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := &dto.UpdateOrganizationReqDto{
		OrganizationCode: plan.OrganizationCode.ValueString(),
		OrganizationName: plan.OrganizationName.ValueString(),
	}
	if !plan.Description.IsNull() {
		updateReq.Description = plan.Description.ValueString()
	}

	res := r.client.UpdateOrganization(updateReq)
	if res == nil || res.StatusCode != 200 || res.Data.OrganizationCode == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to update Authing organization", errMsg)
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *OrganizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state OrganizationModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	code := state.OrganizationCode.ValueString()
	if state.OrganizationCode.IsNull() || state.OrganizationCode.IsUnknown() || code == "" || !state.ID.IsNull() && !state.ID.IsUnknown() && state.ID.ValueString() != code {
		resp.Diagnostics.AddError("Invalid organization identity", "id and organization_code must identify the same nonempty organization")
		return
	}
	before, missing, err := r.organizationDeleteCheck(ctx, code)
	if err != nil {
		resp.Diagnostics.AddError("Check organization before delete failed", err.Error())
		return
	}
	if missing {
		return
	}
	if before.HasChildren == nil || *before.HasChildren {
		resp.Diagnostics.AddError("Unsafe organization deletion", "Authing deletes the entire organization tree; child-department status is unknown or children exist. Remove departments first.")
		return
	}
	res := r.client.DeleteOrganization(&dto.DeleteOrganizationReqDto{
		OrganizationCode: code,
	})
	if res == nil || res.StatusCode != 200 || !res.Data.Success {
		resp.Diagnostics.AddError("Failed to delete Authing organization", "Authing returned an invalid or unsuccessful response")
		return
	}
	_, missing, err = r.organizationDeleteCheck(ctx, code)
	if err != nil || !missing {
		resp.Diagnostics.AddError("Verify organization deletion failed", "Authing did not confirm absence of the exact organization")
	}
}

type organizationDeleteData struct {
	OrganizationCode string `json:"organizationCode"`
	HasChildren      *bool  `json:"hasChildren"`
}

// A raw envelope preserves the difference between absent and false hasChildren;
// the SDK's bool field erases that distinction.
func (r *OrganizationResource) organizationDeleteCheck(ctx context.Context, code string) (organizationDeleteData, bool, error) {
	var data organizationDeleteData
	body, err := r.client.SendHttpRequestContext(ctx, "/api/v3/get-organization", http.MethodGet, &dto.GetOrganizationDto{OrganizationCode: code})
	if err != nil {
		return data, false, fmt.Errorf("get-organization failed: %w", err)
	}
	var out struct {
		StatusCode int                     `json:"statusCode"`
		Data       *organizationDeleteData `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return data, false, fmt.Errorf("invalid get-organization response: %w", err)
	}
	if out.StatusCode == 404 {
		return data, true, nil
	}
	if out.StatusCode != 200 || out.Data == nil || out.Data.OrganizationCode != code {
		return data, false, fmt.Errorf("get-organization returned failure or mismatched organization code (status %d)", out.StatusCode)
	}
	return *out.Data, false, nil
}

func (r *OrganizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("organization_code"), req, resp)
}

// --- Department Resource ---

var _ resource.Resource = &DepartmentResource{}
var _ resource.ResourceWithImportState = &DepartmentResource{}

func NewDepartmentResource() resource.Resource {
	return &DepartmentResource{}
}

type DepartmentResource struct {
	client *authingapi.Client
}

type DepartmentModel struct {
	ID                 types.String `tfsdk:"id"`
	OrganizationCode   types.String `tfsdk:"organization_code"`
	Name               types.String `tfsdk:"name"`
	ParentDepartmentId types.String `tfsdk:"parent_department_id"`
	DepartmentId       types.String `tfsdk:"department_id"`
	Description        types.String `tfsdk:"description"`
}

func (r *DepartmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_department"
}

func (r *DepartmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Authing Department.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_code": schema.StringAttribute{
				Required:    true,
				Description: "Organization code this department belongs to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the department.",
			},
			"parent_department_id": schema.StringAttribute{
				Required:    true,
				Description: "Parent department ID (use 'root' for root level).",
			},
			"department_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Custom department ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Department description.",
			},
		},
	}
}

func (r *DepartmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *DepartmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DepartmentModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := &dto.CreateDepartmentReqDto{
		OrganizationCode:   plan.OrganizationCode.ValueString(),
		Name:               plan.Name.ValueString(),
		ParentDepartmentId: plan.ParentDepartmentId.ValueString(),
	}
	if !plan.DepartmentId.IsNull() {
		createReq.DepartmentIdType = plan.DepartmentId.ValueString()
	}
	if !plan.Description.IsNull() {
		createReq.Description = plan.Description.ValueString()
	}

	res := r.client.CreateDepartment(createReq)
	if res == nil || res.StatusCode != 200 || res.Data.DepartmentId == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to create Authing department", errMsg)
		return
	}

	plan.ID = types.StringValue(res.Data.DepartmentId)
	plan.DepartmentId = types.StringValue(res.Data.DepartmentId)
	plan.Name = types.StringValue(res.Data.Name)
	plan.ParentDepartmentId = types.StringValue(res.Data.ParentDepartmentId)
	if res.Data.Description != "" {
		plan.Description = types.StringValue(res.Data.Description)
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *DepartmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DepartmentModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.GetDepartment(&dto.GetDepartmentDto{
		OrganizationCode: state.OrganizationCode.ValueString(),
		DepartmentId:     state.ID.ValueString(),
	})
	if res != nil && res.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if res == nil || res.StatusCode != 200 || res.Data.DepartmentId == "" {
		resp.Diagnostics.AddError("Failed to read Authing department", "Authing returned an invalid or unsuccessful response")
		return
	}

	state.ID = types.StringValue(res.Data.DepartmentId)
	state.Name = types.StringValue(res.Data.Name)
	state.ParentDepartmentId = types.StringValue(res.Data.ParentDepartmentId)
	if res.Data.Description != "" {
		state.Description = types.StringValue(res.Data.Description)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *DepartmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan DepartmentModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := &dto.UpdateDepartmentReqDto{
		OrganizationCode: plan.OrganizationCode.ValueString(),
		DepartmentId:     plan.ID.ValueString(),
		Name:             plan.Name.ValueString(),
	}
	if !plan.ParentDepartmentId.IsNull() {
		updateReq.ParentDepartmentId = plan.ParentDepartmentId.ValueString()
	}
	if !plan.Description.IsNull() {
		updateReq.Description = plan.Description.ValueString()
	}

	res := r.client.UpdateDepartment(updateReq)
	if res == nil || res.StatusCode != 200 || res.Data.DepartmentId == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to update Authing department", errMsg)
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *DepartmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DepartmentModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.DeleteDepartment(&dto.DeleteDepartmentReqDto{
		OrganizationCode: state.OrganizationCode.ValueString(),
		DepartmentId:     state.ID.ValueString(),
	})
	if res == nil || res.StatusCode != 404 && (res.StatusCode != 200 || !res.Data.Success) {
		resp.Diagnostics.AddError("Failed to delete Authing department", "Authing returned an invalid or unsuccessful response")
	}
}

func (r *DepartmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// --- Department Member Resource ---

var _ resource.Resource = &DepartmentMemberResource{}

func NewDepartmentMemberResource() resource.Resource {
	return &DepartmentMemberResource{}
}

type DepartmentMemberResource struct {
	client *authingapi.Client
}

type DepartmentMemberModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationCode types.String `tfsdk:"organization_code"`
	DepartmentId     types.String `tfsdk:"department_id"`
	UserId           types.String `tfsdk:"user_id"`
}

func (r *DepartmentMemberResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_department_member"
}

func (r *DepartmentMemberResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Adds a user to an Authing department.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"organization_code": schema.StringAttribute{
				Required: true,
			},
			"department_id": schema.StringAttribute{
				Required: true,
			},
			"user_id": schema.StringAttribute{
				Required: true,
			},
		},
	}
}

func (r *DepartmentMemberResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *DepartmentMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DepartmentMemberModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.AddDepartmentMembers(&dto.AddDepartmentMembersReqDto{
		OrganizationCode: plan.OrganizationCode.ValueString(),
		DepartmentId:     plan.DepartmentId.ValueString(),
		UserIds:          []string{plan.UserId.ValueString()},
	})
	if res == nil || res.StatusCode != 200 || !res.Data.Success {
		resp.Diagnostics.AddError("Failed to add user to department", "Error response from Authing")
		return
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s:%s:%s", plan.OrganizationCode.ValueString(), plan.DepartmentId.ValueString(), plan.UserId.ValueString()))
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *DepartmentMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DepartmentMemberModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Inspect the raw envelope: a missing/partial list must not imply absence.
	const limit = 100
	for page, seen := 1, 0; ; page++ {
		body, err := r.client.SendHttpRequestContext(ctx, "/api/v3/list-department-members", http.MethodGet, &dto.ListDepartmentMembersDto{
			OrganizationCode: state.OrganizationCode.ValueString(),
			DepartmentId:     state.DepartmentId.ValueString(),
			Page:             page, Limit: limit,
		})
		var result struct {
			StatusCode int `json:"statusCode"`
			Data       *struct {
				TotalCount *int `json:"totalCount"`
				List       *[]struct {
					UserId string `json:"userId"`
				} `json:"list"`
			} `json:"data"`
		}
		if err != nil || json.Unmarshal(body, &result) != nil || result.StatusCode != 200 || result.Data == nil || result.Data.List == nil || result.Data.TotalCount != nil && *result.Data.TotalCount < 0 {
			resp.Diagnostics.AddError("Failed to read department member", "Authing returned an invalid or unsuccessful membership list")
			return
		}
		for _, user := range *result.Data.List {
			if user.UserId == "" {
				resp.Diagnostics.AddError("Failed to read department member", "Authing returned a membership entry without a user ID")
				return
			}
			if user.UserId == state.UserId.ValueString() {
				return
			}
		}
		seen += len(*result.Data.List)
		if result.Data.TotalCount != nil {
			if seen > *result.Data.TotalCount || seen < *result.Data.TotalCount && len(*result.Data.List) == 0 {
				resp.Diagnostics.AddError("Failed to read department member", "Authing returned an incomplete membership list")
				return
			}
			if seen == *result.Data.TotalCount {
				resp.State.RemoveResource(ctx)
				return
			}
		} else if len(*result.Data.List) < limit {
			resp.State.RemoveResource(ctx)
			return
		}
	}
}

func (r *DepartmentMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
}

func (r *DepartmentMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DepartmentMemberModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.RemoveDepartmentMembers(&dto.RemoveDepartmentMembersReqDto{
		OrganizationCode: state.OrganizationCode.ValueString(),
		DepartmentId:     state.DepartmentId.ValueString(),
		UserIds:          []string{state.UserId.ValueString()},
	})
	if res == nil || res.StatusCode != 404 && (res.StatusCode != 200 || !res.Data.Success) {
		resp.Diagnostics.AddError("Failed to remove department member", "Authing returned an invalid or unsuccessful response")
	}
}

// --- Post Resource (Job Title / Position) ---

var _ resource.Resource = &PostResource{}

func NewPostResource() resource.Resource {
	return &PostResource{}
}

type PostResource struct {
	client *authingapi.Client
}

type PostModel struct {
	ID          types.String `tfsdk:"id"`
	Code        types.String `tfsdk:"code"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

func (r *PostResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_post"
}

func (r *PostResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Authing Post / Position.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"code": schema.StringAttribute{
				Required:    true,
				Description: "Code for the post.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the post.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Description of the post.",
			},
		},
	}
}

func (r *PostResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *PostResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PostModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := &dto.CreatePostDto{
		Code: plan.Code.ValueString(),
		Name: plan.Name.ValueString(),
	}
	if !plan.Description.IsNull() {
		createReq.Description = plan.Description.ValueString()
	}

	res := r.client.CreatePost(createReq)
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to create Authing post", errMsg)
		return
	}

	plan.ID = types.StringValue(res.Data.Code)
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *PostResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PostModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// GetPost's SDK return type is CreatePostDto, which has no statusCode.
	// Decode the envelope directly so an API failure cannot look like absence.
	body, err := r.client.SendHttpRequestContext(ctx, "/api/v3/get-post", http.MethodGet, &dto.GetPostDto{Code: state.Code.ValueString()})
	var result struct {
		StatusCode int                `json:"statusCode"`
		Data       *dto.CreatePostDto `json:"data"`
	}
	if json.Unmarshal(body, &result) == nil && result.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil || result.StatusCode != 200 || result.Data == nil || result.Data.Code == "" {
		resp.Diagnostics.AddError("Failed to read Authing post", "Authing returned an invalid or unsuccessful response")
		return
	}
	res := result.Data

	state.ID = types.StringValue(res.Code)
	state.Name = types.StringValue(res.Name)
	if res.Description != "" {
		state.Description = types.StringValue(res.Description)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *PostResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PostModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := &dto.CreatePostDto{
		Code: plan.Code.ValueString(),
		Name: plan.Name.ValueString(),
	}
	if !plan.Description.IsNull() {
		updateReq.Description = plan.Description.ValueString()
	}

	res := r.client.UpdatePost(updateReq)
	if res == nil || res.StatusCode != 200 {
		resp.Diagnostics.AddError("Failed to update Authing post", "Error response from Authing")
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *PostResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PostModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.RemovePost(&dto.RemovePostDto{
		Code: state.Code.ValueString(),
	})
	if res == nil || res.StatusCode != 404 && res.StatusCode != 200 {
		resp.Diagnostics.AddError("Failed to remove Authing post", "Authing returned an invalid or unsuccessful response")
	}
}

// --- Data Sources for Org & Dept ---

var _ datasource.DataSource = &OrganizationDataSource{}

func NewOrganizationDataSource() datasource.DataSource {
	return &OrganizationDataSource{}
}

type OrganizationDataSource struct {
	client *authingapi.Client
}

func (d *OrganizationDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (d *OrganizationDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Fetches organization details by code.",
		Attributes: map[string]dschema.Attribute{
			"id": dschema.StringAttribute{
				Computed: true,
			},
			"organization_code": dschema.StringAttribute{
				Required: true,
			},
			"organization_name": dschema.StringAttribute{
				Computed: true,
			},
			"description": dschema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func (d *OrganizationDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *OrganizationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state OrganizationModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := d.client.GetOrganization(&dto.GetOrganizationDto{
		OrganizationCode: state.OrganizationCode.ValueString(),
	})
	if res == nil || res.StatusCode != 200 || res.Data.OrganizationCode == "" {
		resp.Diagnostics.AddError("Organization Not Found", "Failed to retrieve organization.")
		return
	}

	state.ID = types.StringValue(res.Data.OrganizationCode)
	state.OrganizationName = types.StringValue(res.Data.OrganizationName)
	if res.Data.Description != "" {
		state.Description = types.StringValue(res.Data.Description)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

var _ datasource.DataSource = &DepartmentDataSource{}

func NewDepartmentDataSource() datasource.DataSource {
	return &DepartmentDataSource{}
}

type DepartmentDataSource struct {
	client *authingapi.Client
}

func (d *DepartmentDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_department"
}

func (d *DepartmentDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Fetches department details.",
		Attributes: map[string]dschema.Attribute{
			"id": dschema.StringAttribute{
				Computed: true,
			},
			"organization_code": dschema.StringAttribute{
				Required: true,
			},
			"department_id": dschema.StringAttribute{
				Required: true,
			},
			"name": dschema.StringAttribute{
				Computed: true,
			},
			"parent_department_id": dschema.StringAttribute{
				Computed: true,
			},
			"description": dschema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func (d *DepartmentDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *DepartmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state DepartmentModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := d.client.GetDepartment(&dto.GetDepartmentDto{
		OrganizationCode: state.OrganizationCode.ValueString(),
		DepartmentId:     state.DepartmentId.ValueString(),
	})
	if res == nil || res.StatusCode != 200 || res.Data.DepartmentId == "" {
		resp.Diagnostics.AddError("Department Not Found", "Failed to retrieve department.")
		return
	}

	state.ID = types.StringValue(res.Data.DepartmentId)
	state.Name = types.StringValue(res.Data.Name)
	state.ParentDepartmentId = types.StringValue(res.Data.ParentDepartmentId)
	if res.Data.Description != "" {
		state.Description = types.StringValue(res.Data.Description)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
