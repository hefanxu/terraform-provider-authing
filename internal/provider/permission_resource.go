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

// --- Permission Namespace Resource ---

var _ resource.Resource = &NamespaceResource{}
var _ resource.ResourceWithImportState = &NamespaceResource{}

func NewNamespaceResource() resource.Resource {
	return &NamespaceResource{}
}

type NamespaceResource struct {
	client *authingapi.Client
}

type NamespaceModel struct {
	ID          types.String `tfsdk:"id"`
	Code        types.String `tfsdk:"code"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

func (r *NamespaceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_namespace"
}

func (r *NamespaceResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Authing Permission Namespace (权限空间).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"code": schema.StringAttribute{
				Required:    true,
				Description: "Unique code for the permission namespace.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name of the permission namespace.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Description of the permission namespace.",
			},
		},
	}
}

func (r *NamespaceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *NamespaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan NamespaceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := &dto.CreatePermissionNamespaceDto{
		Code: plan.Code.ValueString(),
		Name: plan.Name.ValueString(),
	}
	if !plan.Description.IsNull() {
		createReq.Description = plan.Description.ValueString()
	}

	res := r.client.CreatePermissionNamespace(createReq)
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to create permission namespace", errMsg)
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

func (r *NamespaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state NamespaceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{
		Code: state.Code.ValueString(),
	})
	if res != nil && res.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		resp.Diagnostics.AddError("Failed to read Authing namespace", "Authing returned an invalid or unsuccessful response")
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

func (r *NamespaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan NamespaceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := &dto.UpdatePermissionNamespaceDto{
		Code: plan.Code.ValueString(),
		Name: plan.Name.ValueString(),
	}
	if !plan.Description.IsNull() {
		updateReq.Description = plan.Description.ValueString()
	}

	res := r.client.UpdatePermissionNamespace(updateReq)
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to update permission namespace", errMsg)
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *NamespaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state NamespaceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.DeletePermissionNamespace(&dto.DeletePermissionNamespaceDto{
		Code: state.Code.ValueString(),
	})
	if res == nil || res.StatusCode != 404 && (res.StatusCode != 200 || !res.Data.Success) {
		resp.Diagnostics.AddError("Failed to delete Authing namespace", "Authing returned an invalid or unsuccessful response")
	}
}

func (r *NamespaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("code"), req, resp)
}

// --- Role Resource ---

var _ resource.Resource = &RoleResource{}
var _ resource.ResourceWithImportState = &RoleResource{}

func NewRoleResource() resource.Resource {
	return &RoleResource{}
}

type RoleResource struct {
	client *authingapi.Client
}

type RoleModel struct {
	ID          types.String `tfsdk:"id"`
	Code        types.String `tfsdk:"code"`
	Name        types.String `tfsdk:"name"`
	Namespace   types.String `tfsdk:"namespace"`
	Description types.String `tfsdk:"description"`
}

func (r *RoleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (r *RoleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Authing Role.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"code": schema.StringAttribute{
				Required:    true,
				Description: "Unique code for the role within the namespace.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Name of the role.",
			},
			"namespace": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Permission namespace code (default is 'default').",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Description of the role.",
			},
		},
	}
}

func (r *RoleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *RoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RoleModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := &dto.CreateRoleDto{
		Code: plan.Code.ValueString(),
	}
	if !plan.Name.IsNull() {
		createReq.Name = plan.Name.ValueString()
	}
	if !plan.Namespace.IsNull() {
		createReq.Namespace = plan.Namespace.ValueString()
	}
	if !plan.Description.IsNull() {
		createReq.Description = plan.Description.ValueString()
	}

	res := r.client.CreateRole(createReq)
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to create Authing role", errMsg)
		return
	}

	plan.ID = types.StringValue(res.Data.Code)
	plan.Code = types.StringValue(res.Data.Code)
	if res.Data.Namespace != "" {
		plan.Namespace = types.StringValue(res.Data.Namespace)
	}
	if res.Data.Description != "" {
		plan.Description = types.StringValue(res.Data.Description)
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *RoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RoleModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getReq := &dto.GetRoleDto{
		Code: state.Code.ValueString(),
	}
	if !state.Namespace.IsNull() {
		getReq.Namespace = state.Namespace.ValueString()
	}

	res := r.client.GetRole(getReq)
	if res != nil && res.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		resp.Diagnostics.AddError("Failed to read Authing role", "Authing returned an invalid or unsuccessful response")
		return
	}

	state.ID = types.StringValue(res.Data.Code)
	if res.Data.Description != "" {
		state.Description = types.StringValue(res.Data.Description)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *RoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan RoleModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := &dto.UpdateRoleDto{
		Code:    plan.Code.ValueString(),
		NewCode: plan.Code.ValueString(),
		Name:    plan.Name.ValueString(),
	}
	if updateReq.Name == "" {
		updateReq.Name = plan.Code.ValueString()
	}
	if !plan.Namespace.IsNull() {
		updateReq.Namespace = plan.Namespace.ValueString()
	}
	if !plan.Description.IsNull() {
		updateReq.Description = plan.Description.ValueString()
	}

	res := r.client.UpdateRole(updateReq)
	if res == nil || res.StatusCode != 200 {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to update Authing role", errMsg)
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *RoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state RoleModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	delReq := &dto.DeleteRoleDto{
		CodeList: []string{state.Code.ValueString()},
	}
	if !state.Namespace.IsNull() {
		delReq.Namespace = state.Namespace.ValueString()
	}

	res := r.client.DeleteRolesBatch(delReq)
	if res == nil || res.StatusCode != 404 && (res.StatusCode != 200 || !res.Data.Success) {
		resp.Diagnostics.AddError("Failed to delete Authing role", "Authing returned an invalid or unsuccessful response")
	}
}

func (r *RoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("code"), req, resp)
}

// --- Role Assignment Resource ---

var _ resource.Resource = &RoleAssignmentResource{}

func NewRoleAssignmentResource() resource.Resource {
	return &RoleAssignmentResource{}
}

type RoleAssignmentResource struct {
	client *authingapi.Client
}

type RoleAssignmentModel struct {
	ID         types.String `tfsdk:"id"`
	RoleCode   types.String `tfsdk:"role_code"`
	Namespace  types.String `tfsdk:"namespace"`
	TargetType types.String `tfsdk:"target_type"` // USER, DEPARTMENT
	TargetId   types.String `tfsdk:"target_id"`
}

func (r *RoleAssignmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_assignment"
}

func (r *RoleAssignmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Assigns an Authing Role to a user or department target.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"role_code": schema.StringAttribute{
				Required:    true,
				Description: "Role code.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"namespace": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Permission namespace code.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"target_type": schema.StringAttribute{
				Required:    true,
				Description: "Target type: 'USER' or 'DEPARTMENT'.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"target_id": schema.StringAttribute{
				Required:    true,
				Description: "User ID or Department ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *RoleAssignmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *RoleAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RoleAssignmentModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	assignReq := &dto.AssignRoleDto{
		Code: plan.RoleCode.ValueString(),
		Targets: []dto.TargetDto{
			{
				TargetType:       plan.TargetType.ValueString(),
				TargetIdentifier: plan.TargetId.ValueString(),
			},
		},
	}
	if !plan.Namespace.IsNull() {
		assignReq.Namespace = plan.Namespace.ValueString()
	}

	res := r.client.AssignRole(assignReq)
	if res == nil || res.StatusCode != 200 {
		resp.Diagnostics.AddError("Failed to assign role", "Error response from Authing")
		return
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s:%s:%s", plan.RoleCode.ValueString(), plan.TargetType.ValueString(), plan.TargetId.ValueString()))
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *RoleAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RoleAssignmentModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil || state.RoleCode.IsNull() || state.RoleCode.IsUnknown() || state.RoleCode.ValueString() == "" ||
		state.TargetId.IsNull() || state.TargetId.IsUnknown() || state.TargetId.ValueString() == "" ||
		state.Namespace.IsUnknown() {
		resp.Diagnostics.AddError("Failed to read role assignment", "Missing client or invalid assignment identity in state")
		return
	}

	// These endpoints return *direct* grants. get-user-roles can include roles
	// inherited from departments and cannot prove this assignment still exists.
	endpoint, key := "", ""
	switch state.TargetType.ValueString() {
	case "USER":
		endpoint, key = "/api/v3/list-role-members", "userId"
	case "DEPARTMENT":
		endpoint, key = "/api/v3/list-role-departments", "id"
	default:
		resp.Diagnostics.AddError("Failed to read role assignment", "Unsupported target type: cannot verify a direct assignment")
		return
	}

	const limit = 50 // Maximum documented page size for both direct lists.
	for page, seen := 1, 0; ; page++ {
		query := map[string]any{"code": state.RoleCode.ValueString(), "page": page, "limit": limit}
		if !state.Namespace.IsNull() {
			query["namespace"] = state.Namespace.ValueString()
		}
		body, err := r.client.SendHttpRequestContext(ctx, endpoint, http.MethodGet, query)
		if err != nil {
			resp.Diagnostics.AddError("Failed to read role assignment", "Authing direct-assignment request failed: "+err.Error())
			return
		}
		var envelope struct {
			StatusCode int `json:"statusCode"`
			Data       *struct {
				TotalCount *int              `json:"totalCount"`
				List       []json.RawMessage `json:"list"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &envelope) != nil {
			resp.Diagnostics.AddError("Failed to read role assignment", "Invalid Authing direct-assignment response")
			return
		}
		if envelope.StatusCode == http.StatusNotFound && page == 1 {
			resp.State.RemoveResource(ctx)
			return
		}
		if envelope.StatusCode != http.StatusOK || envelope.Data == nil || envelope.Data.TotalCount == nil || *envelope.Data.TotalCount < 0 || envelope.Data.List == nil {
			resp.Diagnostics.AddError("Failed to read role assignment", "Incomplete or unsuccessful Authing direct-assignment response")
			return
		}
		found := false
		for _, entry := range envelope.Data.List {
			var identity map[string]json.RawMessage
			if json.Unmarshal(entry, &identity) != nil {
				resp.Diagnostics.AddError("Failed to read role assignment", "Invalid direct-assignment entry")
				return
			}
			var id string
			if json.Unmarshal(identity[key], &id) != nil || id == "" {
				resp.Diagnostics.AddError("Failed to read role assignment", "Direct-assignment entry lacks a target ID")
				return
			}
			if id == state.TargetId.ValueString() {
				found = true
			}
		}
		seen += len(envelope.Data.List)
		if seen > *envelope.Data.TotalCount || seen < *envelope.Data.TotalCount && len(envelope.Data.List) < limit {
			resp.Diagnostics.AddError("Failed to read role assignment", "Incomplete direct-assignment pagination")
			return
		}
		if found {
			return // Leave the exact stored identity and namespace unchanged.
		}
		if seen == *envelope.Data.TotalCount {
			resp.State.RemoveResource(ctx)
			return
		}
	}
}

func (r *RoleAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
}

func (r *RoleAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state RoleAssignmentModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	revokeReq := &dto.RevokeRoleDto{
		Code: state.RoleCode.ValueString(),
		Targets: []dto.TargetDto{
			{
				TargetType:       state.TargetType.ValueString(),
				TargetIdentifier: state.TargetId.ValueString(),
			},
		},
	}
	if !state.Namespace.IsNull() {
		revokeReq.Namespace = state.Namespace.ValueString()
	}

	res := r.client.RevokeRole(revokeReq)
	if res == nil || res.StatusCode != 404 && (res.StatusCode != 200 || !res.Data.Success) {
		resp.Diagnostics.AddError("Failed to revoke Authing role", "Authing returned an invalid or unsuccessful response")
	}
}

// --- Authing Resource (ACL / RBAC Resource Definition) ---

var _ resource.Resource = &ResourceResource{}

func NewResourceResource() resource.Resource {
	return &ResourceResource{}
}

type ResourceResource struct {
	client *authingapi.Client
}

type ResourceActionModel struct {
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

type ResourceModel struct {
	ID          types.String          `tfsdk:"id"`
	Code        types.String          `tfsdk:"code"`
	Type        types.String          `tfsdk:"type"`
	Namespace   types.String          `tfsdk:"namespace"`
	Description types.String          `tfsdk:"description"`
	Actions     []ResourceActionModel `tfsdk:"actions"`
}

func (r *ResourceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource"
}

func (r *ResourceResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Authing Resource (API, DATA, UI, BUTTON, MENU).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"code": schema.StringAttribute{
				Required:    true,
				Description: "Resource unique code.",
			},
			"type": schema.StringAttribute{
				Required:    true,
				Description: "Resource type: 'DATA', 'API', 'MENU', 'BUTTON', 'UI'.",
			},
			"namespace": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Permission namespace code.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Resource description.",
			},
			"actions": schema.ListNestedAttribute{
				Optional:    true,
				Description: "List of supported actions for this resource.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required: true,
						},
						"description": schema.StringAttribute{
							Optional: true,
						},
					},
				},
			},
		},
	}
}

func (r *ResourceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ResourceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	actions := make([]dto.ResourceAction, 0, len(plan.Actions))
	for _, a := range plan.Actions {
		actions = append(actions, dto.ResourceAction{
			Name:        a.Name.ValueString(),
			Description: a.Description.ValueString(),
		})
	}

	createReq := &dto.CreateResourceDto{
		Code:    plan.Code.ValueString(),
		Type:    plan.Type.ValueString(),
		Actions: actions,
	}
	if !plan.Namespace.IsNull() {
		createReq.Namespace = plan.Namespace.ValueString()
	}
	if !plan.Description.IsNull() {
		createReq.Description = plan.Description.ValueString()
	}

	res := r.client.CreateResource(createReq)
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to create Authing resource", errMsg)
		return
	}

	plan.ID = types.StringValue(res.Data.Code)
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *ResourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getReq := &dto.GetResourceDto{
		Code: state.Code.ValueString(),
	}
	if !state.Namespace.IsNull() {
		getReq.Namespace = state.Namespace.ValueString()
	}

	res := r.client.GetResource(getReq)
	if res != nil && res.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		resp.Diagnostics.AddError("Failed to read Authing resource", "Authing returned an invalid or unsuccessful response")
		return
	}

	state.ID = types.StringValue(res.Data.Code)
	state.Type = types.StringValue(res.Data.Type)
	if res.Data.Description != "" {
		state.Description = types.StringValue(res.Data.Description)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *ResourceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	actions := make([]dto.ResourceAction, 0, len(plan.Actions))
	for _, a := range plan.Actions {
		actions = append(actions, dto.ResourceAction{
			Name:        a.Name.ValueString(),
			Description: a.Description.ValueString(),
		})
	}

	updateReq := &dto.UpdateResourceDto{
		Code:    plan.Code.ValueString(),
		Type:    plan.Type.ValueString(),
		Actions: actions,
	}
	if !plan.Namespace.IsNull() {
		updateReq.Namespace = plan.Namespace.ValueString()
	}
	if !plan.Description.IsNull() {
		updateReq.Description = plan.Description.ValueString()
	}

	res := r.client.UpdateResource(updateReq)
	if res == nil || res.StatusCode != 200 {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to update Authing resource", errMsg)
		return
	}

	// The ID is the stable resource code; it must be known after an update.
	plan.ID = types.StringValue(plan.Code.ValueString())
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *ResourceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	delReq := &dto.DeleteResourceDto{
		Code: state.Code.ValueString(),
	}
	if !state.Namespace.IsNull() {
		delReq.Namespace = state.Namespace.ValueString()
	}

	res := r.client.DeleteResource(delReq)
	if res == nil || res.StatusCode != 404 && (res.StatusCode != 200 || !res.Data.Success) {
		resp.Diagnostics.AddError("Failed to delete Authing resource", "Authing returned an invalid or unsuccessful response")
	}
}

// --- Data Policy Resource ---

var _ resource.Resource = &DataPolicyResource{}

func NewDataPolicyResource() resource.Resource {
	return &DataPolicyResource{}
}

type DataPolicyResource struct {
	client *authingapi.Client
}

type StatementModel struct {
	Effect      types.String   `tfsdk:"effect"`
	Permissions []types.String `tfsdk:"permissions"`
}

type DataPolicyModel struct {
	ID            types.String     `tfsdk:"id"`
	PolicyName    types.String     `tfsdk:"policy_name"`
	Description   types.String     `tfsdk:"description"`
	StatementList []StatementModel `tfsdk:"statement_list"`
}

func (r *DataPolicyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_policy"
}

func (r *DataPolicyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Authing Data Policy (数据策略).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"policy_name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the data policy.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Description of the data policy.",
			},
			"statement_list": schema.ListNestedAttribute{
				Required:    true,
				Description: "List of statements defining ALLOW/DENY rules for data resources.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"effect": schema.StringAttribute{
							Required:    true,
							Description: "'ALLOW' or 'DENY'.",
						},
						"permissions": schema.ListAttribute{
							ElementType: types.StringType,
							Required:    true,
							Description: "List of resource permissions, e.g. 'namespace/resource/action'.",
						},
					},
				},
			},
		},
	}
}

func (r *DataPolicyResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *DataPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DataPolicyModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	stmtList := make([]dto.DataStatementPermissionDto, 0, len(plan.StatementList))
	for _, s := range plan.StatementList {
		perms := make([]string, 0, len(s.Permissions))
		for _, p := range s.Permissions {
			perms = append(perms, p.ValueString())
		}
		stmtList = append(stmtList, dto.DataStatementPermissionDto{
			Effect:      s.Effect.ValueString(),
			Permissions: perms,
		})
	}

	createReq := &dto.CreateDataPolicyDto{
		PolicyName:    plan.PolicyName.ValueString(),
		StatementList: stmtList,
	}
	if !plan.Description.IsNull() {
		createReq.Description = plan.Description.ValueString()
	}

	res := r.client.CreateDataPolicy(createReq)
	if res == nil || res.StatusCode != 200 || res.Data.PolicyId == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to create data policy", errMsg)
		return
	}

	plan.ID = types.StringValue(res.Data.PolicyId)
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *DataPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DataPolicyModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.GetDataPolicy(&dto.GetDataPolicyDto{
		PolicyId: state.ID.ValueString(),
	})
	if res != nil && res.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if res == nil || res.StatusCode != 200 || res.Data.PolicyId == "" {
		resp.Diagnostics.AddError("Failed to read Authing data policy", "Authing returned an invalid or unsuccessful response")
		return
	}

	state.PolicyName = types.StringValue(res.Data.PolicyName)
	if res.Data.Description != "" {
		state.Description = types.StringValue(res.Data.Description)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *DataPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan DataPolicyModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	stmtList := make([]dto.DataStatementPermissionDto, 0, len(plan.StatementList))
	for _, s := range plan.StatementList {
		perms := make([]string, 0, len(s.Permissions))
		for _, p := range s.Permissions {
			perms = append(perms, p.ValueString())
		}
		stmtList = append(stmtList, dto.DataStatementPermissionDto{
			Effect:      s.Effect.ValueString(),
			Permissions: perms,
		})
	}

	updateReq := &dto.UpdateDataPolicyDto{
		PolicyId:      plan.ID.ValueString(),
		PolicyName:    plan.PolicyName.ValueString(),
		StatementList: stmtList,
	}
	if !plan.Description.IsNull() {
		updateReq.Description = plan.Description.ValueString()
	}

	res := r.client.UpdateDataPolicy(updateReq)
	if res == nil || res.StatusCode != 200 {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to update data policy", errMsg)
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *DataPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DataPolicyModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.DeleteDataPolicy(&dto.DeleteDataPolicyDto{
		PolicyId: state.ID.ValueString(),
	})
	if res == nil || res.StatusCode != 404 && res.StatusCode != 200 {
		resp.Diagnostics.AddError("Failed to delete Authing data policy", "Authing returned an invalid or unsuccessful response")
	}
}

// --- Data Sources for Namespace, Role & Resource ---

var _ datasource.DataSource = &NamespaceDataSource{}

func NewNamespaceDataSource() datasource.DataSource {
	return &NamespaceDataSource{}
}

type NamespaceDataSource struct {
	client *authingapi.Client
}

func (d *NamespaceDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_namespace"
}

func (d *NamespaceDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Fetches permission namespace details.",
		Attributes: map[string]dschema.Attribute{
			"id": dschema.StringAttribute{
				Computed: true,
			},
			"code": dschema.StringAttribute{
				Required: true,
			},
			"name": dschema.StringAttribute{
				Computed: true,
			},
			"description": dschema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func (d *NamespaceDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *NamespaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state NamespaceModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := d.client.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{
		Code: state.Code.ValueString(),
	})
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		resp.Diagnostics.AddError("Permission Namespace Not Found", "Failed to retrieve namespace.")
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

var _ datasource.DataSource = &RoleDataSource{}

func NewRoleDataSource() datasource.DataSource {
	return &RoleDataSource{}
}

type RoleDataSource struct {
	client *authingapi.Client
}

type RoleDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Code        types.String `tfsdk:"code"`
	Namespace   types.String `tfsdk:"namespace"`
	Description types.String `tfsdk:"description"`
}

func (d *RoleDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (d *RoleDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Fetches role details.",
		Attributes: map[string]dschema.Attribute{
			"id": dschema.StringAttribute{
				Computed: true,
			},
			"code": dschema.StringAttribute{
				Required: true,
			},
			"namespace": dschema.StringAttribute{
				Optional: true,
			},
			"description": dschema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func (d *RoleDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *RoleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state RoleDataSourceModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getReq := &dto.GetRoleDto{
		Code: state.Code.ValueString(),
	}
	if !state.Namespace.IsNull() {
		getReq.Namespace = state.Namespace.ValueString()
	}

	res := d.client.GetRole(getReq)
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		resp.Diagnostics.AddError("Role Not Found", "Failed to retrieve role.")
		return
	}

	state.ID = types.StringValue(res.Data.Code)
	if res.Data.Description != "" {
		state.Description = types.StringValue(res.Data.Description)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

var _ datasource.DataSource = &ResourceDataSource{}

func NewResourceDataSource() datasource.DataSource {
	return &ResourceDataSource{}
}

type ResourceDataSource struct {
	client *authingapi.Client
}

type ResourceDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Code        types.String `tfsdk:"code"`
	Namespace   types.String `tfsdk:"namespace"`
	Type        types.String `tfsdk:"type"`
	Description types.String `tfsdk:"description"`
}

func (d *ResourceDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource"
}

func (d *ResourceDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Fetches resource details.",
		Attributes: map[string]dschema.Attribute{
			"id": dschema.StringAttribute{
				Computed: true,
			},
			"code": dschema.StringAttribute{
				Required: true,
			},
			"namespace": dschema.StringAttribute{
				Optional: true,
			},
			"type": dschema.StringAttribute{
				Computed: true,
			},
			"description": dschema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func (d *ResourceDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ResourceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ResourceDataSourceModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	getReq := &dto.GetResourceDto{
		Code: state.Code.ValueString(),
	}
	if !state.Namespace.IsNull() {
		getReq.Namespace = state.Namespace.ValueString()
	}

	res := d.client.GetResource(getReq)
	if res == nil || res.StatusCode != 200 || res.Data.Code == "" {
		resp.Diagnostics.AddError("Resource Not Found", "Failed to retrieve resource.")
		return
	}

	state.ID = types.StringValue(res.Data.Code)
	state.Type = types.StringValue(res.Data.Type)
	if res.Data.Description != "" {
		state.Description = types.StringValue(res.Data.Description)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
