package provider

import (
	"context"
	"fmt"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"github.com/Authing/authing-golang-sdk/v3/management"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &UserResource{}
var _ resource.ResourceWithImportState = &UserResource{}

func NewUserResource() resource.Resource {
	return &UserResource{}
}

type UserResource struct {
	client *management.ManagementClient
}

type UserModel struct {
	ID            types.String `tfsdk:"id"`
	Username      types.String `tfsdk:"username"`
	Email         types.String `tfsdk:"email"`
	Phone         types.String `tfsdk:"phone"`
	Nickname      types.String `tfsdk:"nickname"`
	Password      types.String `tfsdk:"password"`
	ExternalId    types.String `tfsdk:"external_id"`
	Status        types.String `tfsdk:"status"`
	Gender        types.String `tfsdk:"gender"`
	EmailVerified types.Bool   `tfsdk:"email_verified"`
	PhoneVerified types.Bool   `tfsdk:"phone_verified"`
}

func (r *UserResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *UserResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Authing User account.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The unique identifier (User ID) in Authing.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"username": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The unique username of the user.",
			},
			"email": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The email address of the user.",
			},
			"phone": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The phone number of the user.",
			},
			"nickname": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Display name or nickname of the user.",
			},
			"password": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Initial password for the user. Plain text or encrypted.",
			},
			"external_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "User ID in the external system.",
			},
			"status": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Account status: 'Activated', 'Suspended', 'Deactivated', etc.",
			},
			"gender": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Gender: 'M', 'F', 'U'.",
			},
			"email_verified": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the email is verified.",
			},
			"phone_verified": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the phone is verified.",
			},
		},
	}
}

func (r *UserResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*management.ManagementClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *management.ManagementClient")
		return
	}
	r.client = client
}

func (r *UserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan UserModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := &dto.CreateUserReqDto{}
	if !plan.Username.IsNull() {
		createReq.Username = plan.Username.ValueString()
	}
	if !plan.Email.IsNull() {
		createReq.Email = plan.Email.ValueString()
	}
	if !plan.Phone.IsNull() {
		createReq.Phone = plan.Phone.ValueString()
	}
	if !plan.Nickname.IsNull() {
		createReq.Nickname = plan.Nickname.ValueString()
	}
	if !plan.Password.IsNull() {
		createReq.Password = plan.Password.ValueString()
	}
	if !plan.ExternalId.IsNull() {
		createReq.ExternalId = plan.ExternalId.ValueString()
	}
	if !plan.Status.IsNull() {
		createReq.Status = plan.Status.ValueString()
	}
	if !plan.Gender.IsNull() {
		createReq.Gender = plan.Gender.ValueString()
	}
	if !plan.EmailVerified.IsNull() {
		createReq.EmailVerified = plan.EmailVerified.ValueBool()
	}
	if !plan.PhoneVerified.IsNull() {
		createReq.PhoneVerified = plan.PhoneVerified.ValueBool()
	}

	res := r.client.CreateUser(createReq)
	if res == nil || res.StatusCode != 200 || res.Data.UserId == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to create Authing user", errMsg)
		return
	}

	user := res.Data
	plan.ID = types.StringValue(user.UserId)
	if user.Username != "" {
		plan.Username = types.StringValue(user.Username)
	}
	if user.Email != "" {
		plan.Email = types.StringValue(user.Email)
	}
	if user.Phone != "" {
		plan.Phone = types.StringValue(user.Phone)
	}
	if user.Nickname != "" {
		plan.Nickname = types.StringValue(user.Nickname)
	}
	if user.ExternalId != "" {
		plan.ExternalId = types.StringValue(user.ExternalId)
	}
	if user.Status != "" {
		plan.Status = types.StringValue(user.Status)
	}
	if user.Gender != "" {
		plan.Gender = types.StringValue(user.Gender)
	}
	plan.EmailVerified = types.BoolValue(user.EmailVerified)
	plan.PhoneVerified = types.BoolValue(user.PhoneVerified)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *UserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state UserModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.GetUser(&dto.GetUserDto{
		UserId: state.ID.ValueString(),
	})
	if res == nil || res.StatusCode != 200 || res.Data.UserId == "" {
		resp.State.RemoveResource(ctx)
		return
	}

	user := res.Data
	if user.Username != "" {
		state.Username = types.StringValue(user.Username)
	} else {
		state.Username = types.StringNull()
	}
	if user.Email != "" {
		state.Email = types.StringValue(user.Email)
	} else {
		state.Email = types.StringNull()
	}
	if user.Phone != "" {
		state.Phone = types.StringValue(user.Phone)
	} else {
		state.Phone = types.StringNull()
	}
	if user.Nickname != "" {
		state.Nickname = types.StringValue(user.Nickname)
	} else {
		state.Nickname = types.StringNull()
	}
	if user.ExternalId != "" {
		state.ExternalId = types.StringValue(user.ExternalId)
	} else {
		state.ExternalId = types.StringNull()
	}
	if user.Status != "" {
		state.Status = types.StringValue(user.Status)
	}
	if user.Gender != "" {
		state.Gender = types.StringValue(user.Gender)
	}
	state.EmailVerified = types.BoolValue(user.EmailVerified)
	state.PhoneVerified = types.BoolValue(user.PhoneVerified)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *UserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan UserModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := &dto.UpdateUserReqDto{
		UserId: plan.ID.ValueString(),
	}
	if !plan.Username.IsNull() {
		updateReq.Username = plan.Username.ValueString()
	}
	if !plan.Nickname.IsNull() {
		updateReq.Nickname = plan.Nickname.ValueString()
	}
	if !plan.ExternalId.IsNull() {
		updateReq.ExternalId = plan.ExternalId.ValueString()
	}
	if !plan.Status.IsNull() {
		updateReq.Status = plan.Status.ValueString()
	}
	if !plan.Gender.IsNull() {
		updateReq.Gender = plan.Gender.ValueString()
	}

	res := r.client.UpdateUser(updateReq)
	if res == nil || res.StatusCode != 200 || res.Data.UserId == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to update Authing user", errMsg)
		return
	}

	user := res.Data
	if user.Username != "" {
		plan.Username = types.StringValue(user.Username)
	}
	if user.Nickname != "" {
		plan.Nickname = types.StringValue(user.Nickname)
	}
	if user.ExternalId != "" {
		plan.ExternalId = types.StringValue(user.ExternalId)
	}
	if user.Status != "" {
		plan.Status = types.StringValue(user.Status)
	}
	if user.Gender != "" {
		plan.Gender = types.StringValue(user.Gender)
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *UserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state UserModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.DeleteUsersBatch(&dto.DeleteUsersBatchDto{
		UserIds: []string{state.ID.ValueString()},
	})
	if res == nil || res.StatusCode != 200 {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to delete Authing user", errMsg)
		return
	}
}

func (r *UserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
