package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

var _ provider.Provider = &AuthingProvider{}

type AuthingProvider struct {
	version string
}

type AuthingProviderModel struct {
	AccessKeyId     types.String `tfsdk:"access_key_id"`
	AccessKeySecret types.String `tfsdk:"access_key_secret"`
	Host            types.String `tfsdk:"host"`
	TenantId        types.String `tfsdk:"tenant_id"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &AuthingProvider{
			version: version,
		}
	}
}

func (p *AuthingProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "authing"
	resp.Version = p.version
}

func (p *AuthingProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "OpenTofu / Terraform Provider for Authing IAM Platform.",
		Attributes: map[string]schema.Attribute{
			"access_key_id": schema.StringAttribute{
				Optional:    true,
				Description: "Authing Access Key ID (User Pool ID). Can also be sourced from AUTHING_ACCESS_KEY_ID or AUTHING_USERPOOL_ID environment variable.",
			},
			"access_key_secret": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Authing Access Key Secret (User Pool Secret). Can also be sourced from AUTHING_ACCESS_KEY_SECRET or AUTHING_USERPOOL_SECRET environment variable.",
			},
			"host": schema.StringAttribute{
				Optional:    true,
				Description: "Authing API Host URL. Defaults to https://api.authing.cn. Can also be set via AUTHING_HOST environment variable.",
			},
			"tenant_id": schema.StringAttribute{
				Optional:    true,
				Description: "Authing Tenant ID for multi-tenant environments. Can also be set via AUTHING_TENANT_ID environment variable.",
			},
		},
	}
}

func (p *AuthingProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config AuthingProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	ak := os.Getenv("AUTHING_ACCESS_KEY_ID")
	if ak == "" {
		ak = os.Getenv("AUTHING_USERPOOL_ID")
	}
	if !config.AccessKeyId.IsNull() && config.AccessKeyId.ValueString() != "" {
		ak = config.AccessKeyId.ValueString()
	}

	sk := os.Getenv("AUTHING_ACCESS_KEY_SECRET")
	if sk == "" {
		sk = os.Getenv("AUTHING_USERPOOL_SECRET")
	}
	if !config.AccessKeySecret.IsNull() && config.AccessKeySecret.ValueString() != "" {
		sk = config.AccessKeySecret.ValueString()
	}

	host := "https://api.authing.cn"
	if envHost := os.Getenv("AUTHING_HOST"); envHost != "" {
		host = envHost
	}
	if !config.Host.IsNull() && config.Host.ValueString() != "" {
		host = config.Host.ValueString()
	}

	tenantId := os.Getenv("AUTHING_TENANT_ID")
	if !config.TenantId.IsNull() && config.TenantId.ValueString() != "" {
		tenantId = config.TenantId.ValueString()
	}

	if ak == "" || sk == "" {
		resp.Diagnostics.AddError(
			"Missing Authing API Credentials",
			"Both access_key_id and access_key_secret must be set in the provider configuration or via environment variables (AUTHING_ACCESS_KEY_ID / AUTHING_ACCESS_KEY_SECRET).",
		)
		return
	}

	opts := authingapi.Options{
		AccessKeyID:     ak,
		AccessKeySecret: sk,
		Host:            host,
		TenantID:        tenantId,
	}

	client, err := authingapi.NewClient(opts)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create Authing Management Client",
			fmt.Sprintf("Failed to initialize Authing API client: %s", err.Error()),
		)
		return
	}

	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *AuthingProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		// Identity & Users
		NewUserResource,
		NewPublicAccountResource,
		// Groups & Organizations & Departments & Posts
		NewGroupResource,
		NewDepartmentResource,
		NewOrganizationResource,
		NewPostResource,
		// Permission Namespaces & Roles & Resources & Policies
		NewNamespaceResource,
		NewRoleResource,
		NewResourceResource,
		NewDataPolicyResource,
		NewInvitationPolicyResource,
		NewInvitationRosterResource,
		NewDataPolicyAssignmentResource,
		NewTenantMembershipResource,
		NewTenantAdminResource,
		NewRoleAssignmentResource,
		NewGroupMemberResource,
		NewDepartmentMemberResource,
		// Applications & ExtIdp & Webhooks
		NewApplicationResource,
		NewExtIdpResource,
		NewWebhookResource,
		NewPipelineFunctionResource,
		NewAuthFlowFunctionResource,
		NewDataResourceResource,
		NewTenantResource,
		NewDataObjectResource,
		NewDataObjectFieldResource,
	}
}

func (p *AuthingProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewUserDataSource,
		NewPublicAccountDataSource,
		NewUsersDataSource,
		NewGroupDataSource,
		NewDepartmentDataSource,
		NewOrganizationDataSource,
		NewNamespaceDataSource,
		NewRoleDataSource,
		NewResourceDataSource,
		NewDataResourceDataSource,
		NewTenantDataSource,
		NewApplicationDataSource,
		NewDeviceStatusDataSource,
		NewApplicationSubjectAuthDataSource,
	}
}
