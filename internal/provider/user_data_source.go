package provider

import (
	"context"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"github.com/Authing/authing-golang-sdk/v3/management"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &UserDataSource{}

func NewUserDataSource() datasource.DataSource {
	return &UserDataSource{}
}

type UserDataSource struct {
	client *management.ManagementClient
}

type UserDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	UserId        types.String `tfsdk:"user_id"`
	Username      types.String `tfsdk:"username"`
	Email         types.String `tfsdk:"email"`
	Phone         types.String `tfsdk:"phone"`
	Nickname      types.String `tfsdk:"nickname"`
	ExternalId    types.String `tfsdk:"external_id"`
	Status        types.String `tfsdk:"status"`
	Gender        types.String `tfsdk:"gender"`
	EmailVerified types.Bool   `tfsdk:"email_verified"`
	PhoneVerified types.Bool   `tfsdk:"phone_verified"`
}

func (d *UserDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (d *UserDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches information about a single Authing User.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Internal identifier.",
			},
			"user_id": schema.StringAttribute{
				Required:    true,
				Description: "Authing User ID.",
			},
			"username": schema.StringAttribute{
				Computed:    true,
				Description: "Username.",
			},
			"email": schema.StringAttribute{
				Computed:    true,
				Description: "Email address.",
			},
			"phone": schema.StringAttribute{
				Computed:    true,
				Description: "Phone number.",
			},
			"nickname": schema.StringAttribute{
				Computed:    true,
				Description: "Nickname.",
			},
			"external_id": schema.StringAttribute{
				Computed:    true,
				Description: "External ID.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "User account status.",
			},
			"gender": schema.StringAttribute{
				Computed:    true,
				Description: "Gender.",
			},
			"email_verified": schema.BoolAttribute{
				Computed:    true,
				Description: "Email verification status.",
			},
			"phone_verified": schema.BoolAttribute{
				Computed:    true,
				Description: "Phone verification status.",
			},
		},
	}
}

func (d *UserDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *UserDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state UserDataSourceModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	res := d.client.GetUser(&dto.GetUserDto{
		UserId: state.UserId.ValueString(),
	})
	if res == nil || res.StatusCode != 200 || res.Data.UserId == "" {
		resp.Diagnostics.AddError("Authing User Not Found", "Failed to retrieve user by ID.")
		return
	}

	user := res.Data
	state.ID = types.StringValue(user.UserId)
	state.UserId = types.StringValue(user.UserId)
	if user.Username != "" {
		state.Username = types.StringValue(user.Username)
	}
	if user.Email != "" {
		state.Email = types.StringValue(user.Email)
	}
	if user.Phone != "" {
		state.Phone = types.StringValue(user.Phone)
	}
	if user.Nickname != "" {
		state.Nickname = types.StringValue(user.Nickname)
	}
	if user.ExternalId != "" {
		state.ExternalId = types.StringValue(user.ExternalId)
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

// Users (List) Data Source
var _ datasource.DataSource = &UsersDataSource{}

func NewUsersDataSource() datasource.DataSource {
	return &UsersDataSource{}
}

type UsersDataSource struct {
	client *management.ManagementClient
}

type UsersDataSourceModel struct {
	ID       types.String          `tfsdk:"id"`
	Keywords types.String          `tfsdk:"keywords"`
	Users    []UserDataSourceModel `tfsdk:"users"`
}

func (d *UsersDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_users"
}

func (d *UsersDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists Authing users by search filter or pagination.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"keywords": schema.StringAttribute{
				Optional:    true,
				Description: "Keywords for searching users.",
			},
			"users": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed: true,
						},
						"user_id": schema.StringAttribute{
							Computed: true,
						},
						"username": schema.StringAttribute{
							Computed: true,
						},
						"email": schema.StringAttribute{
							Computed: true,
						},
						"phone": schema.StringAttribute{
							Computed: true,
						},
						"nickname": schema.StringAttribute{
							Computed: true,
						},
						"external_id": schema.StringAttribute{
							Computed: true,
						},
						"status": schema.StringAttribute{
							Computed: true,
						},
						"gender": schema.StringAttribute{
							Computed: true,
						},
						"email_verified": schema.BoolAttribute{
							Computed: true,
						},
						"phone_verified": schema.BoolAttribute{
							Computed: true,
						},
					},
				},
			},
		},
	}
}

func (d *UsersDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *UsersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state UsersDataSourceModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	listReq := &dto.ListUsersRequestDto{}
	if !state.Keywords.IsNull() && state.Keywords.ValueString() != "" {
		listReq.Keywords = state.Keywords.ValueString()
	}

	res := d.client.ListUsers(listReq)
	if res == nil || res.StatusCode != 200 {
		resp.Diagnostics.AddError("Failed to list Authing users", "Error getting user list")
		return
	}

	state.ID = types.StringValue("users_list")
	state.Users = make([]UserDataSourceModel, 0, len(res.Data.List))
	for _, u := range res.Data.List {
		item := UserDataSourceModel{
			ID:            types.StringValue(u.UserId),
			UserId:        types.StringValue(u.UserId),
			Status:        types.StringValue(u.Status),
			Gender:        types.StringValue(u.Gender),
			EmailVerified: types.BoolValue(u.EmailVerified),
			PhoneVerified: types.BoolValue(u.PhoneVerified),
		}
		if u.Username != "" {
			item.Username = types.StringValue(u.Username)
		}
		if u.Email != "" {
			item.Email = types.StringValue(u.Email)
		}
		if u.Phone != "" {
			item.Phone = types.StringValue(u.Phone)
		}
		if u.Nickname != "" {
			item.Nickname = types.StringValue(u.Nickname)
		}
		if u.ExternalId != "" {
			item.ExternalId = types.StringValue(u.ExternalId)
		}
		state.Users = append(state.Users, item)
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
