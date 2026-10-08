package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"terraform-provider-authing/internal/authingapi"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// --- Application Resource ---

var _ resource.Resource = &ApplicationResource{}
var _ resource.ResourceWithImportState = &ApplicationResource{}

func NewApplicationResource() resource.Resource {
	return &ApplicationResource{}
}

type ApplicationResource struct {
	client *authingapi.Client
}

type ApplicationModel struct {
	ID                 types.String `tfsdk:"id"`
	AppId              types.String `tfsdk:"app_id"`
	AppName            types.String `tfsdk:"app_name"`
	AppType            types.String `tfsdk:"app_type"`
	RedirectUris       types.List   `tfsdk:"redirect_uris"`
	LogoutRedirectUris types.List   `tfsdk:"logout_redirect_uris"`
	InitLoginUrl       types.String `tfsdk:"init_login_url"`
	Description        types.String `tfsdk:"description"`
	AppIdentifier      types.String `tfsdk:"app_identifier"`
	AppLogo            types.String `tfsdk:"app_logo"`
	DefaultProtocol    types.String `tfsdk:"default_protocol"`
	SsoEnabled         types.Bool   `tfsdk:"sso_enabled"`
	PermissionStrategy types.String `tfsdk:"permission_strategy"`
}

func (r *ApplicationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application"
}

func (r *ApplicationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Authing Application (自建应用).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"app_id": schema.StringAttribute{
				Computed:    true,
				Description: "Authing Application ID.",
			},
			"app_name": schema.StringAttribute{
				Required:    true,
				Description: "Application name.",
			},
			"app_type": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Application type (e.g. 'web', 'spa', 'native', 'api').",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"redirect_uris": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Allowed redirect callback URIs.",
			},
			"logout_redirect_uris": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "Allowed logout redirect callback URIs.",
			},
			"init_login_url": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Initial login URL.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Application description.",
			},
			"app_identifier":   schema.StringAttribute{Optional: true, Computed: true, Description: "Unique application identifier."},
			"app_logo":         schema.StringAttribute{Optional: true, Computed: true, Description: "Application logo URL."},
			"default_protocol": schema.StringAttribute{Optional: true, Computed: true, Description: "Default application protocol (oidc, oauth, saml, cas, asa)."},
			"sso_enabled":      schema.BoolAttribute{Optional: true, Computed: true, Description: "Whether SSO is enabled."},
			"permission_strategy": schema.StringAttribute{
				Optional: true, Computed: true,
				Description: "Default application access authorization policy: ALLOW_ALL or DENY_ALL. Omission adopts Authing's value; no independent reset endpoint exists.",
				Validators:  []validator.String{applicationPermissionStrategyValidator{}},
			},
		},
	}
}

func (r *ApplicationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ApplicationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ApplicationModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	redirectUris := make([]string, 0)
	if !plan.RedirectUris.IsNull() && !plan.RedirectUris.IsUnknown() {
		resp.Diagnostics.Append(plan.RedirectUris.ElementsAs(ctx, &redirectUris, false)...)
	}

	logoutUris := make([]string, 0)
	if !plan.LogoutRedirectUris.IsNull() && !plan.LogoutRedirectUris.IsUnknown() {
		resp.Diagnostics.Append(plan.LogoutRedirectUris.ElementsAs(ctx, &logoutUris, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := &dto.CreateApplicationDto{
		AppName:            plan.AppName.ValueString(),
		RedirectUris:       redirectUris,
		LogoutRedirectUris: logoutUris,
	}
	if !plan.AppType.IsNull() && !plan.AppType.IsUnknown() {
		createReq.AppType = plan.AppType.ValueString()
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		createReq.AppDescription = plan.Description.ValueString()
	}
	if !plan.InitLoginUrl.IsNull() && !plan.InitLoginUrl.IsUnknown() {
		createReq.InitLoginUri = plan.InitLoginUrl.ValueString()
	}
	// The SDK DTO omits false booleans and empty strings, although these may
	// be explicitly configured. Preserve its other serialized fields below.
	var payload map[string]interface{}
	encoded, err := json.Marshal(createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Authing application", err.Error())
		return
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		resp.Diagnostics.AddError("Failed to create Authing application", err.Error())
		return
	}
	// DTO string fields use omitempty, but a configured empty value is distinct
	// from leaving an optional setting to Authing's defaults.
	if !plan.AppIdentifier.IsNull() && !plan.AppIdentifier.IsUnknown() {
		payload["appIdentifier"] = plan.AppIdentifier.ValueString()
	}
	if !plan.AppLogo.IsNull() && !plan.AppLogo.IsUnknown() {
		payload["appLogo"] = plan.AppLogo.ValueString()
	}
	if !plan.DefaultProtocol.IsNull() && !plan.DefaultProtocol.IsUnknown() {
		payload["defaultProtocol"] = plan.DefaultProtocol.ValueString()
	}
	if !plan.SsoEnabled.IsNull() && !plan.SsoEnabled.IsUnknown() {
		payload["ssoEnabled"] = plan.SsoEnabled.ValueBool()
	}
	body, err := r.client.SendHttpRequest("/api/v3/create-application", "POST", payload)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Authing application", err.Error())
		return
	}
	var result dto.CreateApplicationRespDto
	if err := json.Unmarshal(body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to create Authing application", err.Error())
		return
	}
	res := &result
	if res == nil || res.StatusCode != 200 || res.Data.AppId == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to create Authing application", errMsg)
		return
	}

	plan.ID = types.StringValue(res.Data.AppId)
	plan.AppId = types.StringValue(res.Data.AppId)
	if !plan.PermissionStrategy.IsNull() && !plan.PermissionStrategy.IsUnknown() {
		if err := r.updatePermissionStrategy(ctx, res.Data.AppId, plan.PermissionStrategy.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to set application permission strategy", fmt.Sprintf("Application %q was created but strategy update failed: %v. Import this ID before retrying to avoid duplicate creation.", res.Data.AppId, err))
			return
		}
	}
	strategy, err := r.getPermissionStrategy(ctx, res.Data.AppId)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read application permission strategy", fmt.Sprintf("Application %q was created but strategy readback failed: %v. Import this ID before retrying to avoid duplicate creation.", res.Data.AppId, err))
		return
	}
	if !plan.PermissionStrategy.IsNull() && !plan.PermissionStrategy.IsUnknown() && strategy != plan.PermissionStrategy.ValueString() {
		resp.Diagnostics.AddError("Failed to confirm application permission strategy", fmt.Sprintf("Application %q was created but strategy readback returned %q rather than %q. Import this ID before retrying to avoid duplicate creation.", res.Data.AppId, strategy, plan.PermissionStrategy.ValueString()))
		return
	}
	plan.PermissionStrategy = types.StringValue(strategy)
	// app_name is required configuration; do not overwrite it with a server-normalized
	// value during Create, which would produce an inconsistent Terraform plan.
	if plan.AppType.IsUnknown() || plan.AppType.IsNull() {
		plan.AppType = types.StringValue(res.Data.AppType)
	}
	if plan.Description.IsUnknown() || plan.Description.IsNull() {
		plan.Description = types.StringValue(res.Data.AppDescription)
	}
	if plan.InitLoginUrl.IsUnknown() || plan.InitLoginUrl.IsNull() {
		plan.InitLoginUrl = types.StringValue(res.Data.InitLoginUri)
	}
	if plan.AppIdentifier.IsUnknown() || plan.AppIdentifier.IsNull() {
		plan.AppIdentifier = types.StringValue(res.Data.AppIdentifier)
	}
	if plan.AppLogo.IsUnknown() || plan.AppLogo.IsNull() {
		plan.AppLogo = types.StringValue(res.Data.AppLogo)
	}
	if plan.DefaultProtocol.IsUnknown() || plan.DefaultProtocol.IsNull() {
		plan.DefaultProtocol = types.StringValue(res.Data.DefaultProtocol)
	}
	if plan.SsoEnabled.IsUnknown() || plan.SsoEnabled.IsNull() {
		plan.SsoEnabled = types.BoolValue(res.Data.SsoEnabled)
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *ApplicationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ApplicationModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.GetApplication(&dto.GetApplicationDto{
		AppId: state.AppId.ValueString(),
	})
	if res != nil && res.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if res == nil || res.StatusCode != 200 || res.Data.AppId == "" {
		resp.Diagnostics.AddError("Failed to read Authing application", applicationResponseError(res))
		return
	}
	strategy, err := r.getPermissionStrategy(ctx, res.Data.AppId)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read application permission strategy", err.Error())
		return
	}

	state.ID = types.StringValue(res.Data.AppId)
	state.PermissionStrategy = types.StringValue(strategy)
	state.AppId = types.StringValue(res.Data.AppId)
	state.AppName = types.StringValue(res.Data.AppName)
	state.AppType = types.StringValue(res.Data.AppType)
	state.Description = types.StringValue(res.Data.AppDescription)
	state.InitLoginUrl = types.StringValue(res.Data.InitLoginUri)
	state.AppIdentifier = types.StringValue(res.Data.AppIdentifier)
	state.AppLogo = types.StringValue(res.Data.AppLogo)
	state.DefaultProtocol = types.StringValue(res.Data.DefaultProtocol)
	state.SsoEnabled = types.BoolValue(res.Data.SsoEnabled)
	state.RedirectUris, diags = types.ListValueFrom(ctx, types.StringType, nonNilApplicationURIs(res.Data.RedirectUris))
	resp.Diagnostics.Append(diags...)
	state.LogoutRedirectUris, diags = types.ListValueFrom(ctx, types.StringType, nonNilApplicationURIs(res.Data.LogoutRedirectUris))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *ApplicationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ApplicationModel
	var state ApplicationModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	strategyChanged := !plan.PermissionStrategy.IsNull() && !plan.PermissionStrategy.IsUnknown() && plan.PermissionStrategy != state.PermissionStrategy
	otherPlan, otherState := plan, state
	otherPlan.PermissionStrategy, otherState.PermissionStrategy = types.StringNull(), types.StringNull()
	strategyOnly := strategyChanged && reflect.DeepEqual(otherPlan, otherState)
	if !plan.AppType.IsUnknown() && !plan.AppType.IsNull() && plan.AppType != state.AppType {
		resp.Diagnostics.AddError("Cannot update app_type", "Authing does not support changing app_type; replace the application instead.")
		return
	}
	appID := state.AppId.ValueString()
	if appID == "" {
		appID = state.ID.ValueString()
	}
	// SDK v3.0.15 has no UpdateApplication method or DTO; use its authenticated
	// transport with the official /api/v3/update-application request fields.
	payload := map[string]interface{}{"appId": appID, "appName": plan.AppName.ValueString()}
	if !plan.AppIdentifier.IsUnknown() && !plan.AppIdentifier.IsNull() {
		payload["appIdentifier"] = plan.AppIdentifier.ValueString()
	}
	if !plan.AppLogo.IsUnknown() && !plan.AppLogo.IsNull() {
		payload["appLogo"] = plan.AppLogo.ValueString()
	}
	if !plan.DefaultProtocol.IsUnknown() && !plan.DefaultProtocol.IsNull() {
		payload["defaultProtocol"] = plan.DefaultProtocol.ValueString()
	}
	if !plan.SsoEnabled.IsUnknown() && !plan.SsoEnabled.IsNull() {
		payload["ssoEnabled"] = plan.SsoEnabled.ValueBool()
	}
	if !plan.Description.IsUnknown() && !plan.Description.IsNull() {
		payload["appDescription"] = plan.Description.ValueString()
	}
	if !plan.InitLoginUrl.IsUnknown() && !plan.InitLoginUrl.IsNull() {
		payload["initLoginUri"] = plan.InitLoginUrl.ValueString()
	}
	if !plan.RedirectUris.IsUnknown() && !plan.RedirectUris.IsNull() {
		var uris []string
		resp.Diagnostics.Append(plan.RedirectUris.ElementsAs(ctx, &uris, false)...)
		payload["redirectUris"] = nonNilApplicationURIs(uris)
	}
	if !plan.LogoutRedirectUris.IsUnknown() && !plan.LogoutRedirectUris.IsNull() {
		var uris []string
		resp.Diagnostics.Append(plan.LogoutRedirectUris.ElementsAs(ctx, &uris, false)...)
		payload["logoutRedirectUris"] = nonNilApplicationURIs(uris)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if !strategyOnly {
		body, err := r.client.SendHttpRequest("/api/v3/update-application", "POST", payload)
		if err != nil {
			resp.Diagnostics.AddError("Failed to update Authing application", err.Error())
			return
		}
		var result dto.IsSuccessRespDto
		if err := json.Unmarshal(body, &result); err != nil {
			resp.Diagnostics.AddError("Failed to update Authing application", err.Error())
			return
		}
		if result.StatusCode != 200 || !result.Data.Success {
			resp.Diagnostics.AddError("Failed to update Authing application", fmt.Sprintf("code=%d msg=%s success=%t", result.StatusCode, result.Message, result.Data.Success))
			return
		}
		plan, err = r.confirmApplicationUpdate(ctx, appID, plan)
		if err != nil {
			resp.Diagnostics.AddError("Failed to confirm Authing application update", fmt.Sprintf("Application %q acknowledged the update but readback failed: %v. Prior state was retained.", appID, err))
			return
		}
	}
	if strategyChanged {
		if err := r.updatePermissionStrategy(ctx, appID, plan.PermissionStrategy.ValueString()); err != nil {
			resp.Diagnostics.AddError("Failed to update application permission strategy", err.Error())
			return
		}
		strategy, err := r.getPermissionStrategy(ctx, appID)
		if err != nil {
			resp.Diagnostics.AddError("Failed to confirm application permission strategy", err.Error())
			return
		}
		if strategy != plan.PermissionStrategy.ValueString() {
			resp.Diagnostics.AddError("Failed to confirm application permission strategy", fmt.Sprintf("App %q returned %q instead of %q", appID, strategy, plan.PermissionStrategy.ValueString()))
			return
		}
	} else {
		plan.PermissionStrategy = state.PermissionStrategy
	}
	plan.ID = types.StringValue(appID)
	plan.AppId = types.StringValue(appID)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *ApplicationResource) confirmApplicationUpdate(ctx context.Context, appID string, plan ApplicationModel) (ApplicationModel, error) {
	res := r.client.GetApplication(&dto.GetApplicationDto{AppId: appID})
	if res == nil || res.StatusCode != 200 || res.Data.AppId != appID {
		return plan, fmt.Errorf("invalid or unsuccessful GET /get-application response (expected app ID %q): %s", appID, applicationResponseError(res))
	}
	remote := res.Data
	for _, field := range []struct {
		name    string
		planned types.String
		actual  string
	}{
		{"app_name", plan.AppName, remote.AppName},
		{"app_type", plan.AppType, remote.AppType},
		{"app_identifier", plan.AppIdentifier, remote.AppIdentifier},
		{"app_logo", plan.AppLogo, remote.AppLogo},
		{"default_protocol", plan.DefaultProtocol, remote.DefaultProtocol},
		{"description", plan.Description, remote.AppDescription},
		{"init_login_url", plan.InitLoginUrl, remote.InitLoginUri},
	} {
		if !field.planned.IsNull() && !field.planned.IsUnknown() && field.planned.ValueString() != field.actual {
			return plan, fmt.Errorf("%s read back as %q instead of %q", field.name, field.actual, field.planned.ValueString())
		}
	}
	if !plan.SsoEnabled.IsNull() && !plan.SsoEnabled.IsUnknown() && plan.SsoEnabled.ValueBool() != remote.SsoEnabled {
		return plan, fmt.Errorf("sso_enabled read back as %t instead of %t", remote.SsoEnabled, plan.SsoEnabled.ValueBool())
	}
	for _, field := range []struct {
		name    string
		planned types.List
		actual  []string
	}{
		{"redirect_uris", plan.RedirectUris, remote.RedirectUris},
		{"logout_redirect_uris", plan.LogoutRedirectUris, remote.LogoutRedirectUris},
	} {
		if !field.planned.IsNull() && !field.planned.IsUnknown() {
			var expected []string
			if diags := field.planned.ElementsAs(ctx, &expected, false); diags.HasError() {
				return plan, fmt.Errorf("invalid planned %s: %v", field.name, diags)
			}
			if !reflect.DeepEqual(nonNilApplicationURIs(expected), nonNilApplicationURIs(field.actual)) {
				return plan, fmt.Errorf("%s read back differently from the plan", field.name)
			}
		}
	}
	if plan.AppType.IsNull() || plan.AppType.IsUnknown() {
		plan.AppType = types.StringValue(remote.AppType)
	}
	if plan.AppIdentifier.IsNull() || plan.AppIdentifier.IsUnknown() {
		plan.AppIdentifier = types.StringValue(remote.AppIdentifier)
	}
	if plan.AppLogo.IsNull() || plan.AppLogo.IsUnknown() {
		plan.AppLogo = types.StringValue(remote.AppLogo)
	}
	if plan.DefaultProtocol.IsNull() || plan.DefaultProtocol.IsUnknown() {
		plan.DefaultProtocol = types.StringValue(remote.DefaultProtocol)
	}
	if plan.Description.IsNull() || plan.Description.IsUnknown() {
		plan.Description = types.StringValue(remote.AppDescription)
	}
	if plan.InitLoginUrl.IsNull() || plan.InitLoginUrl.IsUnknown() {
		plan.InitLoginUrl = types.StringValue(remote.InitLoginUri)
	}
	if plan.SsoEnabled.IsNull() || plan.SsoEnabled.IsUnknown() {
		plan.SsoEnabled = types.BoolValue(remote.SsoEnabled)
	}
	if plan.RedirectUris.IsNull() || plan.RedirectUris.IsUnknown() {
		var diags diag.Diagnostics
		plan.RedirectUris, diags = types.ListValueFrom(ctx, types.StringType, nonNilApplicationURIs(remote.RedirectUris))
		if diags.HasError() {
			return plan, fmt.Errorf("invalid remote redirect_uris: %v", diags)
		}
	}
	if plan.LogoutRedirectUris.IsNull() || plan.LogoutRedirectUris.IsUnknown() {
		var diags diag.Diagnostics
		plan.LogoutRedirectUris, diags = types.ListValueFrom(ctx, types.StringType, nonNilApplicationURIs(remote.LogoutRedirectUris))
		if diags.HasError() {
			return plan, fmt.Errorf("invalid remote logout_redirect_uris: %v", diags)
		}
	}
	return plan, nil
}

func (r *ApplicationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ApplicationModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result := r.client.DeleteApplication(&dto.DeleteApplicationDto{
		AppId: state.AppId.ValueString(),
	})
	if result != nil && result.StatusCode == 404 {
		return
	}
	if result == nil {
		resp.Diagnostics.AddError("Failed to delete Authing application", "Empty or invalid API response")
		return
	}
	if result.StatusCode != 200 || !result.Data.Success {
		resp.Diagnostics.AddError("Failed to delete Authing application", fmt.Sprintf("code=%d msg=%s success=%t", result.StatusCode, result.Message, result.Data.Success))
	}
}

func (r *ApplicationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.Set(ctx, ApplicationModel{
		ID: types.StringValue(req.ID), AppId: types.StringValue(req.ID),
		AppName: types.StringNull(), AppType: types.StringNull(),
		Description: types.StringNull(), InitLoginUrl: types.StringNull(),
		RedirectUris: types.ListNull(types.StringType), LogoutRedirectUris: types.ListNull(types.StringType),
		AppIdentifier: types.StringNull(), AppLogo: types.StringNull(),
		DefaultProtocol: types.StringNull(), SsoEnabled: types.BoolNull(),
		PermissionStrategy: types.StringNull(),
	})...)
}

func nonNilApplicationURIs(uris []string) []string {
	if uris == nil {
		return []string{}
	}
	return uris
}

func applicationResponseError(res *dto.ApplicationSingleRespDto) string {
	if res == nil {
		return "Empty or invalid API response"
	}
	return fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
}

type applicationPermissionStrategyValidator struct{}

func (applicationPermissionStrategyValidator) Description(context.Context) string {
	return "Must be ALLOW_ALL or DENY_ALL."
}
func (v applicationPermissionStrategyValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (applicationPermissionStrategyValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !req.ConfigValue.IsNull() && !req.ConfigValue.IsUnknown() && !validPermissionStrategy(req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid permission strategy", "Expected ALLOW_ALL or DENY_ALL.")
	}
}

func validPermissionStrategy(s string) bool { return s == "ALLOW_ALL" || s == "DENY_ALL" }

func (r *ApplicationResource) getPermissionStrategy(ctx context.Context, appID string) (string, error) {
	body, err := r.client.SendHttpRequestContext(ctx, "/api/v3/get-application-permission-strategy", "GET", map[string]string{"appId": appID})
	if err != nil {
		return "", err
	}
	var result struct {
		StatusCode int    `json:"statusCode"`
		Message    string `json:"message"`
		Data       *struct {
			PermissionStrategy string `json:"permissionStrategy"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if result.StatusCode != 200 {
		return "", fmt.Errorf("code=%d msg=%s", result.StatusCode, result.Message)
	}
	if result.Data == nil || !validPermissionStrategy(result.Data.PermissionStrategy) {
		return "", fmt.Errorf("missing or invalid permissionStrategy in API response")
	}
	return result.Data.PermissionStrategy, nil
}

func (r *ApplicationResource) updatePermissionStrategy(ctx context.Context, appID, strategy string) error {
	body, err := r.client.SendHttpRequestContext(ctx, "/api/v3/update-application-permission-strategy", "POST", map[string]string{"appId": appID, "permissionStrategy": strategy})
	if err != nil {
		return err
	}
	var result struct {
		StatusCode int    `json:"statusCode"`
		Message    string `json:"message"`
		Data       *struct {
			Success bool `json:"success"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}
	if result.StatusCode != 200 || result.Data == nil || !result.Data.Success {
		return fmt.Errorf("code=%d msg=%s success=false", result.StatusCode, result.Message)
	}
	return nil
}

// --- Webhook Resource ---

var _ resource.Resource = &WebhookResource{}

func NewWebhookResource() resource.Resource {
	return &WebhookResource{}
}

type WebhookResource struct {
	client *authingapi.Client
}

type WebhookModel struct {
	ID          types.String `tfsdk:"id"`
	WebhookId   types.String `tfsdk:"webhook_id"`
	Name        types.String `tfsdk:"name"`
	Url         types.String `tfsdk:"url"`
	ContentType types.String `tfsdk:"content_type"`
	Secret      types.String `tfsdk:"secret"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	Events      types.List   `tfsdk:"events"`
}

func (r *WebhookResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (r *WebhookResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Authing Webhook event subscription.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"webhook_id": schema.StringAttribute{
				Computed: true,
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Webhook name.",
			},
			"url": schema.StringAttribute{
				Required:    true,
				Description: "Webhook callback URL endpoint.",
			},
			"content_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Payload format ('application/json', etc.).",
			},
			"secret": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Secret token for signature verification.",
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the webhook is enabled.",
			},
			"events": schema.ListAttribute{
				ElementType: types.StringType,
				Required:    true,
				Description: "List of event codes to subscribe to.",
			},
		},
	}
}

func (r *WebhookResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *WebhookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WebhookModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	events := make([]string, 0)
	plan.Events.ElementsAs(ctx, &events, false)

	contentType := "application/json"
	if !plan.ContentType.IsNull() && plan.ContentType.ValueString() != "" {
		contentType = plan.ContentType.ValueString()
	}

	createReq := &dto.CreateWebhookDto{
		Name:        plan.Name.ValueString(),
		Url:         plan.Url.ValueString(),
		Events:      events,
		ContentType: contentType,
	}
	if !plan.Secret.IsNull() {
		createReq.Secret = plan.Secret.ValueString()
	}
	if !plan.Enabled.IsNull() {
		createReq.Enabled = plan.Enabled.ValueBool()
	}

	// The SDK DTO's omitempty drops an explicitly configured false. Webhook
	// delivery must remain disabled even when the service default is true.
	payload := map[string]any{"name": createReq.Name, "url": createReq.Url, "events": events, "contentType": contentType}
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		payload["enabled"] = plan.Enabled.ValueBool()
	}
	if createReq.Secret != "" {
		payload["secret"] = createReq.Secret
	}
	body, err := r.client.SendHttpRequest("/api/v3/create-webhook", "POST", payload)
	var result dto.CreateWebhookRespDto
	if err != nil || json.Unmarshal(body, &result) != nil {
		resp.Diagnostics.AddError("Failed to create Authing webhook", "Invalid or unavailable API response")
		return
	}
	res := &result
	if res.StatusCode != 200 || res.Data.WebhookId == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to create Authing webhook", errMsg)
		return
	}

	plan.ID = types.StringValue(res.Data.WebhookId)
	plan.WebhookId = types.StringValue(res.Data.WebhookId)
	if plan.ContentType.IsUnknown() || plan.ContentType.IsNull() {
		plan.ContentType = types.StringValue(contentType)
	}
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *WebhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WebhookModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.GetWebhook(&dto.GetWebhookDto{
		WebhookId: state.WebhookId.ValueString(),
	})
	if res != nil && res.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if res == nil || res.StatusCode != 200 || res.Data.WebhookId == "" {
		resp.Diagnostics.AddError("Failed to read Authing webhook", "Authing returned an invalid or unsuccessful response")
		return
	}

	state.ID = types.StringValue(res.Data.WebhookId)
	state.WebhookId = types.StringValue(res.Data.WebhookId)
	state.Name = types.StringValue(res.Data.Name)
	state.Url = types.StringValue(res.Data.Url)
	state.Enabled = types.BoolValue(res.Data.Enabled)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *WebhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state WebhookModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.WebhookId.ValueString() == "" || state.ID.ValueString() != state.WebhookId.ValueString() {
		resp.Diagnostics.AddError("Failed to update Authing webhook", "Missing or mismatched webhook identity in state")
		return
	}

	events := make([]string, 0)
	plan.Events.ElementsAs(ctx, &events, false)

	updateReq := &dto.UpdateWebhookDto{
		WebhookId: state.WebhookId.ValueString(),
		Name:      plan.Name.ValueString(),
		Url:       plan.Url.ValueString(),
		Events:    events,
	}
	if !plan.Secret.IsNull() {
		updateReq.Secret = plan.Secret.ValueString()
	}
	if !plan.Enabled.IsNull() {
		updateReq.Enabled = plan.Enabled.ValueBool()
	}

	payload := map[string]any{"webhookId": updateReq.WebhookId, "name": updateReq.Name, "url": updateReq.Url, "events": events, "enabled": updateReq.Enabled}
	if updateReq.Secret != "" {
		payload["secret"] = updateReq.Secret
	}
	body, err := r.client.SendHttpRequest("/api/v3/update-webhook", "POST", payload)
	var result dto.UpdateWebhooksRespDto
	if err != nil || json.Unmarshal(body, &result) != nil {
		resp.Diagnostics.AddError("Failed to update Authing webhook", "Invalid or unavailable API response")
		return
	}
	res := &result
	if res.StatusCode != 200 {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to update Authing webhook", errMsg)
		return
	}

	plan.ID = state.ID
	plan.WebhookId = state.WebhookId
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *WebhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WebhookModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.DeleteWebhook(&dto.DeleteWebhookDto{
		WebhookIds: []string{state.WebhookId.ValueString()},
	})
	if res == nil || res.StatusCode != 404 && res.StatusCode != 200 {
		resp.Diagnostics.AddError("Failed to delete Authing webhook", "Authing returned an invalid or unsuccessful response")
	}
}

// --- ExtIdp Resource (External Identity Provider) ---

var _ resource.Resource = &ExtIdpResource{}
var _ resource.ResourceWithImportState = &ExtIdpResource{}

func NewExtIdpResource() resource.Resource {
	return &ExtIdpResource{}
}

type ExtIdpResource struct {
	client *authingapi.Client
}

type ExtIdpModel struct {
	ID       types.String `tfsdk:"id"`
	ExtIdpId types.String `tfsdk:"ext_idp_id"`
	Name     types.String `tfsdk:"name"`
	Type     types.String `tfsdk:"type"`
	TenantId types.String `tfsdk:"tenant_id"`
}

func (r *ExtIdpResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ext_idp"
}

func (r *ExtIdpResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Authing External Identity Provider (身份源).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"ext_idp_id": schema.StringAttribute{
				Computed: true,
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the identity provider.",
			},
			"type": schema.StringAttribute{
				Required:    true,
				Description: "Identity provider type: 'wechat', 'dingtalk', 'ldap', 'saml', 'oidc', etc.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tenant_id": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Tenant ID if multi-tenant. Changing it replaces the identity provider.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *ExtIdpResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ExtIdpResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ExtIdpModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := &dto.CreateExtIdpDto{
		Name: plan.Name.ValueString(),
		Type: plan.Type.ValueString(),
	}
	if !plan.TenantId.IsNull() {
		createReq.TenantId = plan.TenantId.ValueString()
	}

	res := r.client.CreateExtIdp(createReq)
	if res == nil || res.StatusCode != 200 || res.Data.Id == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to create external IdP", errMsg)
		return
	}
	if res.Data.TenantId != plan.TenantId.ValueString() {
		resp.Diagnostics.AddError("Failed to confirm external IdP scope", fmt.Sprintf("External IdP %q was created but returned tenant %q instead of %q. Import this ID in its actual tenant before retrying to avoid duplicate creation.", res.Data.Id, res.Data.TenantId, plan.TenantId.ValueString()))
		return
	}
	// Optional+Computed tenant_id is unknown on an omitted HCL attribute.
	// Never return that unknown after creation: read the exact created ID and
	// populate known values from Authing, including the verified empty scope.
	readback := r.client.GetExtIdp(&dto.GetExtIdpDto{Id: res.Data.Id, TenantId: res.Data.TenantId})
	if readback == nil || readback.StatusCode != 200 || readback.Data.Id != res.Data.Id || readback.Data.TenantId != res.Data.TenantId || readback.Data.Type != plan.Type.ValueString() || readback.Data.Name != plan.Name.ValueString() {
		resp.Diagnostics.AddError("Failed to confirm external IdP after create", fmt.Sprintf("External IdP %q was created but exact-ID readback did not confirm its configured fields; import this ID before retrying.", res.Data.Id))
		return
	}
	plan.ID = types.StringValue(readback.Data.Id)
	plan.ExtIdpId = types.StringValue(readback.Data.Id)
	plan.TenantId = types.StringValue(readback.Data.TenantId)
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *ExtIdpResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ExtIdpModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.getScopedExtIdp(&state)
	if res != nil && res.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if res == nil || res.StatusCode != 200 || res.Data.Id == "" {
		resp.Diagnostics.AddError("Failed to read external IdP", "Authing returned an invalid or unsuccessful response")
		return
	}
	if err := extIdpIdentityError(&state, &res.Data); err != nil {
		resp.Diagnostics.AddError("External IdP identity mismatch", err.Error())
		return
	}

	state.ID = types.StringValue(res.Data.Id)
	state.Name = types.StringValue(res.Data.Name)
	state.Type = types.StringValue(res.Data.Type)
	state.TenantId = types.StringValue(res.Data.TenantId)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *ExtIdpResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ExtIdpModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.ID.IsNull() || state.ID.IsUnknown() || state.ExtIdpId.IsNull() || state.ExtIdpId.IsUnknown() || state.ID.ValueString() == "" || state.ID != state.ExtIdpId ||
		!plan.TenantId.IsUnknown() && plan.TenantId != state.TenantId || plan.Type != state.Type ||
		!plan.ExtIdpId.IsUnknown() && plan.ExtIdpId != state.ExtIdpId ||
		!plan.ID.IsUnknown() && plan.ID != state.ID {
		resp.Diagnostics.AddError("Cannot update external IdP identity", "tenant_id and type are immutable; replace the identity provider instead.")
		return
	}
	if err := r.confirmExtIdp(&state); err != nil {
		resp.Diagnostics.AddError("Failed to confirm external IdP before update", err.Error())
		return
	}

	res := r.client.UpdateExtIdp(&dto.UpdateExtIdpDto{
		Id: state.ExtIdpId.ValueString(), Name: plan.Name.ValueString(), TenantId: state.TenantId.ValueString(),
	})
	if res == nil || res.StatusCode != 200 {
		resp.Diagnostics.AddError("Failed to update external IdP", "Error response from Authing")
		return
	}
	readback := r.getScopedExtIdp(&state)
	if err := confirmExtIdpResponse(&state, readback); err != nil {
		resp.Diagnostics.AddError("Failed to confirm external IdP after update", err.Error())
		return
	}
	if readback.Data.Name != plan.Name.ValueString() {
		resp.Diagnostics.AddError("Failed to confirm external IdP after update", fmt.Sprintf("IdP %q read back name %q instead of %q; prior state was retained.", state.ExtIdpId.ValueString(), readback.Data.Name, plan.Name.ValueString()))
		return
	}

	plan.ID = types.StringValue(readback.Data.Id)
	plan.ExtIdpId = types.StringValue(readback.Data.Id)
	plan.TenantId = types.StringValue(readback.Data.TenantId)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ExtIdpResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ExtIdpModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	preflight := r.getScopedExtIdp(&state)
	if preflight != nil && preflight.StatusCode == 404 {
		return // Already absent in the requested scope.
	}
	if err := confirmExtIdpResponse(&state, preflight); err != nil {
		resp.Diagnostics.AddError("Failed to confirm external IdP before delete", err.Error())
		return
	}

	res := r.client.DeleteExtIdp(&dto.DeleteExtIdpDto{
		Id: state.ExtIdpId.ValueString(), TenantId: state.TenantId.ValueString(),
	})
	if res == nil || res.StatusCode != 404 && (res.StatusCode != 200 || !res.Data.Success) {
		resp.Diagnostics.AddError("Failed to delete external IdP", "Authing returned an invalid or unsuccessful response")
	}
}

func (r *ExtIdpResource) getScopedExtIdp(state *ExtIdpModel) *dto.ExtIdpDetailSingleRespDto {
	return r.client.GetExtIdp(&dto.GetExtIdpDto{Id: state.ExtIdpId.ValueString(), TenantId: state.TenantId.ValueString()})
}

func extIdpIdentityError(state *ExtIdpModel, remote *dto.ExtIdpDetail) error {
	if remote.Id != state.ExtIdpId.ValueString() || remote.TenantId != state.TenantId.ValueString() {
		return fmt.Errorf("requested IdP %q in tenant %q but received IdP %q in tenant %q", state.ExtIdpId.ValueString(), state.TenantId.ValueString(), remote.Id, remote.TenantId)
	}
	if !state.Type.IsNull() && !state.Type.IsUnknown() && state.Type.ValueString() != remote.Type {
		return fmt.Errorf("IdP %q type changed from %q to %q", remote.Id, state.Type.ValueString(), remote.Type)
	}
	if remote.Type == "" {
		return fmt.Errorf("IdP %q response omitted type", remote.Id)
	}
	return nil
}

func (r *ExtIdpResource) confirmExtIdp(state *ExtIdpModel) error {
	return confirmExtIdpResponse(state, r.getScopedExtIdp(state))
}

func confirmExtIdpResponse(state *ExtIdpModel, res *dto.ExtIdpDetailSingleRespDto) error {
	if res == nil || res.StatusCode != 200 || res.Data.Id == "" {
		if res == nil {
			return fmt.Errorf("empty or invalid API response")
		}
		return fmt.Errorf("code=%d msg=%s", res.StatusCode, res.Message)
	}
	return extIdpIdentityError(state, &res.Data)
}

// Import accepts an unscoped ID or tenant_id:ext_idp_id for a tenant-scoped IdP.
func (r *ExtIdpResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, ":")
	if req.ID == "" || len(parts) > 2 || len(parts) == 2 && (parts[0] == "" || parts[1] == "") {
		resp.Diagnostics.AddError("Invalid external IdP import ID", "Expected an ID or tenant_id:ext_idp_id with nonempty components.")
		return
	}
	id, tenant := parts[0], types.StringNull()
	if len(parts) == 2 {
		id, tenant = parts[1], types.StringValue(parts[0])
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, ExtIdpModel{
		ID: types.StringValue(id), ExtIdpId: types.StringValue(id),
		TenantId: tenant, Type: types.StringNull(), Name: types.StringNull(),
	})...)
}

// --- Pipeline Function Resource ---

var _ resource.Resource = &PipelineFunctionResource{}

func NewPipelineFunctionResource() resource.Resource {
	return &PipelineFunctionResource{}
}

type PipelineFunctionResource struct {
	client *authingapi.Client
}

type PipelineFunctionModel struct {
	ID              types.String `tfsdk:"id"`
	FuncId          types.String `tfsdk:"func_id"`
	FuncName        types.String `tfsdk:"func_name"`
	FuncDescription types.String `tfsdk:"func_description"`
	Scene           types.String `tfsdk:"scene"`
	SourceCode      types.String `tfsdk:"source_code"`
	IsAsynchronous  types.Bool   `tfsdk:"is_asynchronous"`
}

func (r *PipelineFunctionResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipeline_function"
}

func (r *PipelineFunctionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Authing Pipeline serverless extension function.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"func_id": schema.StringAttribute{
				Computed: true,
			},
			"func_name": schema.StringAttribute{
				Required:    true,
				Description: "Pipeline function name.",
			},
			"func_description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Pipeline function description.",
			},
			"scene": schema.StringAttribute{
				Required:    true,
				Description: "Trigger scene: 'PRE_REGISTER', 'POST_REGISTER', 'PRE_AUTHENTICATION', 'POST_AUTHENTICATION', 'PRE_OIDC_ID_TOKEN_ISSUED', 'PRE_OIDC_ACCESS_TOKEN_ISSUED', 'PRE_COMPLETE_USER_INFO'.",
			},
			"source_code": schema.StringAttribute{
				Required:    true,
				Description: "JavaScript source code of the pipeline function.",
			},
			"is_asynchronous": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the pipeline function runs asynchronously.",
			},
		},
	}
}

func (r *PipelineFunctionResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *PipelineFunctionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PipelineFunctionModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := &dto.CreatePipelineFunctionDto{
		FuncName:   plan.FuncName.ValueString(),
		Scene:      plan.Scene.ValueString(),
		SourceCode: plan.SourceCode.ValueString(),
	}
	if !plan.FuncDescription.IsNull() {
		createReq.FuncDescription = plan.FuncDescription.ValueString()
	}
	if !plan.IsAsynchronous.IsNull() {
		createReq.IsAsynchronous = plan.IsAsynchronous.ValueBool()
	}

	res := r.client.CreatePipelineFunction(createReq)
	if res == nil || res.StatusCode != 200 || res.Data.FuncId == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to create pipeline function", errMsg)
		return
	}

	plan.ID = types.StringValue(res.Data.FuncId)
	plan.FuncId = types.StringValue(res.Data.FuncId)
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *PipelineFunctionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PipelineFunctionModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.GetPipelineFunction(&dto.GetPipelineFunctionDto{
		FuncId: state.FuncId.ValueString(),
	})
	if res != nil && res.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		return
	}
	if res == nil || res.StatusCode != 200 || res.Data.FuncId == "" {
		resp.Diagnostics.AddError("Failed to read Authing pipeline function", "Authing returned an invalid or unsuccessful response")
		return
	}

	state.ID = types.StringValue(res.Data.FuncId)
	state.FuncName = types.StringValue(res.Data.FuncName)
	if res.Data.FuncDescription != "" {
		state.FuncDescription = types.StringValue(res.Data.FuncDescription)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *PipelineFunctionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PipelineFunctionModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := &dto.UpdatePipelineFunctionDto{
		FuncId:     plan.FuncId.ValueString(),
		FuncName:   plan.FuncName.ValueString(),
		SourceCode: plan.SourceCode.ValueString(),
	}
	if !plan.FuncDescription.IsNull() {
		updateReq.FuncDescription = plan.FuncDescription.ValueString()
	}
	if !plan.IsAsynchronous.IsNull() {
		updateReq.IsAsynchronous = plan.IsAsynchronous.ValueBool()
	}

	res := r.client.UpdatePipelineFunction(updateReq)
	if res == nil || res.StatusCode != 200 {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to update pipeline function", errMsg)
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *PipelineFunctionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PipelineFunctionModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.DeletePipelineFunction(&dto.DeletePipelineFunctionDto{
		FuncId: state.FuncId.ValueString(),
	})
	if res == nil || res.StatusCode != 404 && res.StatusCode != 200 {
		resp.Diagnostics.AddError("Failed to delete Authing pipeline function", "Authing returned an invalid or unsuccessful response")
	}
}

// --- Application Data Source ---

var _ datasource.DataSource = &ApplicationDataSource{}

func NewApplicationDataSource() datasource.DataSource {
	return &ApplicationDataSource{}
}

type ApplicationDataSource struct {
	client *authingapi.Client
}

type ApplicationDataSourceModel struct {
	ID           types.String `tfsdk:"id"`
	AppId        types.String `tfsdk:"app_id"`
	AppName      types.String `tfsdk:"app_name"`
	Description  types.String `tfsdk:"description"`
	InitLoginUrl types.String `tfsdk:"init_login_url"`
}

func (d *ApplicationDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application"
}

func (d *ApplicationDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Fetches application details by App ID.",
		Attributes: map[string]dschema.Attribute{
			"id": dschema.StringAttribute{
				Computed: true,
			},
			"app_id": dschema.StringAttribute{
				Required: true,
			},
			"app_name": dschema.StringAttribute{
				Computed: true,
			},
			"description": dschema.StringAttribute{
				Computed: true,
			},
			"init_login_url": dschema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func (d *ApplicationDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ApplicationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ApplicationDataSourceModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := d.client.GetApplication(&dto.GetApplicationDto{
		AppId: state.AppId.ValueString(),
	})
	if res == nil || res.StatusCode != 200 || res.Data.AppId == "" {
		resp.Diagnostics.AddError("Application Not Found", "Failed to retrieve application.")
		return
	}

	state.ID = types.StringValue(res.Data.AppId)
	state.AppName = types.StringValue(res.Data.AppName)
	if res.Data.AppDescription != "" {
		state.Description = types.StringValue(res.Data.AppDescription)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
