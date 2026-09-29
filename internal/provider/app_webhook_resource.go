package provider

import (
	"context"
	"fmt"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"github.com/Authing/authing-golang-sdk/v3/management"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// --- Application Resource ---

var _ resource.Resource = &ApplicationResource{}
var _ resource.ResourceWithImportState = &ApplicationResource{}

func NewApplicationResource() resource.Resource {
	return &ApplicationResource{}
}

type ApplicationResource struct {
	client *management.ManagementClient
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
				Optional:    true,
				Computed:    true,
				Description: "Application type (e.g. 'web', 'spa', 'native', 'api').",
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
		},
	}
}

func (r *ApplicationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ApplicationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ApplicationModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	redirectUris := make([]string, 0)
	if !plan.RedirectUris.IsNull() {
		plan.RedirectUris.ElementsAs(ctx, &redirectUris, false)
	}

	logoutUris := make([]string, 0)
	if !plan.LogoutRedirectUris.IsNull() {
		plan.LogoutRedirectUris.ElementsAs(ctx, &logoutUris, false)
	}

	createReq := &dto.CreateApplicationDto{
		AppName:            plan.AppName.ValueString(),
		RedirectUris:       redirectUris,
		LogoutRedirectUris: logoutUris,
	}
	if !plan.Description.IsNull() {
		createReq.AppDescription = plan.Description.ValueString()
	}
	if !plan.InitLoginUrl.IsNull() {
		createReq.InitLoginUri = plan.InitLoginUrl.ValueString()
	}

	res := r.client.CreateApplication(createReq)
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
	plan.AppName = types.StringValue(res.Data.AppName)
	if res.Data.AppDescription != "" {
		plan.Description = types.StringValue(res.Data.AppDescription)
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
	if res == nil || res.StatusCode != 200 || res.Data.AppId == "" {
		resp.State.RemoveResource(ctx)
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

func (r *ApplicationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ApplicationModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *ApplicationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ApplicationModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	_ = r.client.DeleteApplication(&dto.DeleteApplicationDto{
		AppId: state.AppId.ValueString(),
	})
}

func (r *ApplicationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("app_id"), req, resp)
}

// --- Webhook Resource ---

var _ resource.Resource = &WebhookResource{}

func NewWebhookResource() resource.Resource {
	return &WebhookResource{}
}

type WebhookResource struct {
	client *management.ManagementClient
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
	client, ok := req.ProviderData.(*management.ManagementClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *management.ManagementClient")
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

	res := r.client.CreateWebhook(createReq)
	if res == nil || res.StatusCode != 200 || res.Data.WebhookId == "" {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to create Authing webhook", errMsg)
		return
	}

	plan.ID = types.StringValue(res.Data.WebhookId)
	plan.WebhookId = types.StringValue(res.Data.WebhookId)
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
	if res == nil || res.StatusCode != 200 || res.Data.WebhookId == "" {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(res.Data.WebhookId)
	state.Name = types.StringValue(res.Data.Name)
	state.Url = types.StringValue(res.Data.Url)
	state.Enabled = types.BoolValue(res.Data.Enabled)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *WebhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WebhookModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	events := make([]string, 0)
	plan.Events.ElementsAs(ctx, &events, false)

	updateReq := &dto.UpdateWebhookDto{
		WebhookId: plan.WebhookId.ValueString(),
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

	res := r.client.UpdateWebhook(updateReq)
	if res == nil || res.StatusCode != 200 {
		errMsg := "Unknown error"
		if res != nil {
			errMsg = fmt.Sprintf("code=%d msg=%s", res.StatusCode, res.Message)
		}
		resp.Diagnostics.AddError("Failed to update Authing webhook", errMsg)
		return
	}

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

	_ = r.client.DeleteWebhook(&dto.DeleteWebhookDto{
		WebhookIds: []string{state.WebhookId.ValueString()},
	})
}

// --- ExtIdp Resource (External Identity Provider) ---

var _ resource.Resource = &ExtIdpResource{}

func NewExtIdpResource() resource.Resource {
	return &ExtIdpResource{}
}

type ExtIdpResource struct {
	client *management.ManagementClient
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
				Optional:    true,
				Computed:    true,
				Description: "Tenant ID if multi-tenant.",
			},
		},
	}
}

func (r *ExtIdpResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

	plan.ID = types.StringValue(res.Data.Id)
	plan.ExtIdpId = types.StringValue(res.Data.Id)
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

	res := r.client.GetExtIdp(&dto.GetExtIdpDto{
		Id: state.ExtIdpId.ValueString(),
	})
	if res == nil || res.StatusCode != 200 || res.Data.Id == "" {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(res.Data.Id)
	state.Name = types.StringValue(res.Data.Name)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *ExtIdpResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ExtIdpModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := r.client.UpdateExtIdp(&dto.UpdateExtIdpDto{
		Id:   plan.ExtIdpId.ValueString(),
		Name: plan.Name.ValueString(),
	})
	if res == nil || res.StatusCode != 200 {
		resp.Diagnostics.AddError("Failed to update external IdP", "Error response from Authing")
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *ExtIdpResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ExtIdpModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	_ = r.client.DeleteExtIdp(&dto.DeleteExtIdpDto{
		Id: state.ExtIdpId.ValueString(),
	})
}

// --- Pipeline Function Resource ---

var _ resource.Resource = &PipelineFunctionResource{}

func NewPipelineFunctionResource() resource.Resource {
	return &PipelineFunctionResource{}
}

type PipelineFunctionResource struct {
	client *management.ManagementClient
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
	client, ok := req.ProviderData.(*management.ManagementClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *management.ManagementClient")
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
	if res == nil || res.StatusCode != 200 || res.Data.FuncId == "" {
		resp.State.RemoveResource(ctx)
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

	_ = r.client.DeletePipelineFunction(&dto.DeletePipelineFunctionDto{
		FuncId: state.FuncId.ValueString(),
	})
}

// --- Application Data Source ---

var _ datasource.DataSource = &ApplicationDataSource{}

func NewApplicationDataSource() datasource.DataSource {
	return &ApplicationDataSource{}
}

type ApplicationDataSource struct {
	client *management.ManagementClient
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
	client, ok := req.ProviderData.(*management.ManagementClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", "Expected *management.ManagementClient")
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
