package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

var _ resource.Resource = &UserResource{}
var _ resource.ResourceWithImportState = &UserResource{}

func NewUserResource() resource.Resource {
	return &UserResource{}
}

type UserResource struct {
	client *authingapi.Client
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
	client, ok := req.ProviderData.(*authingapi.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *authingapi.Client")
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
	if res != nil && res.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if res == nil || res.StatusCode != 200 || res.Data.UserId != state.ID.ValueString() {
		detail := "Invalid or unavailable response"
		if res != nil {
			detail = fmt.Sprintf("code=%d msg=%s userId=%q", res.StatusCode, res.Message, res.Data.UserId)
		}
		resp.Diagnostics.AddError("Failed to read Authing user", detail)
		return
	}

	userModelFromRemote(&state, res.Data)
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

// userModelFromRemote maps a confirmed GET response, retaining the write-only password.
func userModelFromRemote(state *UserModel, user dto.UserDto) {
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
}

func (r *UserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan UserModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The SDK DTO uses omitempty on bools, which silently drops an explicit false.
	updateReq := map[string]any{"userId": plan.ID.ValueString()}
	for _, field := range []struct {
		key   string
		value types.String
	}{
		{"username", plan.Username}, {"email", plan.Email}, {"phone", plan.Phone},
		{"nickname", plan.Nickname}, {"externalId", plan.ExternalId},
		{"status", plan.Status}, {"gender", plan.Gender},
	} {
		if !field.value.IsNull() && !field.value.IsUnknown() {
			updateReq[field.key] = field.value.ValueString()
		}
	}
	if !plan.EmailVerified.IsNull() && !plan.EmailVerified.IsUnknown() {
		updateReq["emailVerified"] = plan.EmailVerified.ValueBool()
	}
	if !plan.PhoneVerified.IsNull() && !plan.PhoneVerified.IsUnknown() {
		updateReq["phoneVerified"] = plan.PhoneVerified.ValueBool()
	}

	body, err := r.client.SendHttpRequestContext(ctx, "/api/v3/update-user", "POST", updateReq)
	var result dto.UserSingleRespDto
	if err != nil || json.Unmarshal(body, &result) != nil || result.StatusCode != 200 || result.Data.UserId != plan.ID.ValueString() {
		detail := "Invalid or unavailable response"
		if err != nil {
			detail = err.Error()
		} else if result.StatusCode != 0 {
			detail = fmt.Sprintf("code=%d msg=%s userId=%q", result.StatusCode, result.Message, result.Data.UserId)
		}
		resp.Diagnostics.AddError("Failed to update Authing user", detail)
		return
	}

	// An update acknowledgement is not proof that Authing applied each field.
	// Read once, check the response and field presence before committing state.
	body, err = r.client.SendHttpRequestContext(ctx, "/api/v3/get-user", "GET", &dto.GetUserDto{UserId: plan.ID.ValueString()})
	var readback struct {
		StatusCode int             `json:"statusCode"`
		Data       json.RawMessage `json:"data"`
	}
	var user dto.UserDto
	var fields map[string]json.RawMessage
	if err != nil || json.Unmarshal(body, &readback) != nil || readback.StatusCode != 200 ||
		json.Unmarshal(readback.Data, &fields) != nil || json.Unmarshal(readback.Data, &user) != nil || user.UserId != plan.ID.ValueString() {
		resp.Diagnostics.AddError("Failed to verify Authing user update", "GET readback failed or returned a different user")
		return
	}
	for key := range updateReq {
		if key == "userId" {
			continue
		}
		value, ok := fields[key]
		if !ok || string(value) == "null" {
			resp.Diagnostics.AddError("Failed to verify Authing user update", fmt.Sprintf("GET readback omitted configured %s", key))
			return
		}
	}
	var actual UserModel
	diags = req.State.Get(ctx, &actual)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	userModelFromRemote(&actual, user)
	for _, field := range []struct {
		name      string
		want, got types.String
	}{
		{"username", plan.Username, actual.Username}, {"email", plan.Email, actual.Email},
		{"phone", plan.Phone, actual.Phone}, {"nickname", plan.Nickname, actual.Nickname},
		{"external_id", plan.ExternalId, actual.ExternalId},
		{"status", plan.Status, actual.Status}, {"gender", plan.Gender, actual.Gender},
	} {
		if !field.want.IsNull() && !field.want.IsUnknown() && !field.want.Equal(field.got) {
			resp.Diagnostics.AddError("Failed to verify Authing user update", fmt.Sprintf("Readback did not match configured %s", field.name))
			return
		}
	}
	if !plan.EmailVerified.IsNull() && !plan.EmailVerified.IsUnknown() && !plan.EmailVerified.Equal(actual.EmailVerified) ||
		!plan.PhoneVerified.IsNull() && !plan.PhoneVerified.IsUnknown() && !plan.PhoneVerified.Equal(actual.PhoneVerified) {
		resp.Diagnostics.AddError("Failed to verify Authing user update", "Readback did not match configured verification flags")
		return
	}
	diags = resp.State.Set(ctx, &actual)
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
