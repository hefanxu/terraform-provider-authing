package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

var _ resource.Resource = &PublicAccountResource{}
var _ resource.ResourceWithImportState = &PublicAccountResource{}

func NewPublicAccountResource() resource.Resource { return &PublicAccountResource{} }

type PublicAccountResource struct{ client *authingapi.Client }
type PublicAccountModel struct {
	ID       types.String `tfsdk:"id"`
	Username types.String `tfsdk:"username"`
	Name     types.String `tfsdk:"name"`
	Nickname types.String `tfsdk:"nickname"`
	Email    types.String `tfsdk:"email"`
}

func (r *PublicAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_public_account"
}
func (r *PublicAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Manages a public account. Do not manage the same Authing user ID with authing_user.", Attributes: map[string]schema.Attribute{
		"id":       schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"username": schema.StringAttribute{Optional: true, Computed: true},
		"name":     schema.StringAttribute{Optional: true, Computed: true},
		"nickname": schema.StringAttribute{Optional: true, Computed: true},
		"email":    schema.StringAttribute{Optional: true, Computed: true},
	}}
}
func (r *PublicAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*authingapi.Client)
	if !ok {
		resp.Diagnostics.AddError("Invalid client", "Expected *authingapi.Client")
		return
	}
	r.client = c
}

type publicEnvelope struct {
	StatusCode int             `json:"statusCode"`
	Data       json.RawMessage `json:"data"`
}
type publicRemote struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
}

func publicSend(ctx context.Context, c *authingapi.Client, endpoint, method string, body any) (publicEnvelope, error) {
	var out publicEnvelope
	if c == nil {
		return out, errors.New("Authing client not configured")
	}
	raw, err := c.SendHttpRequestContext(ctx, endpoint, method, body)
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, errors.New("invalid Authing response")
	}
	return out, nil
}
func publicGet(ctx context.Context, c *authingapi.Client, id string) (publicRemote, bool, error) {
	var v publicRemote
	if id == "" {
		return v, false, errors.New("empty public account ID")
	}
	out, err := publicSend(ctx, c, "/api/v3/get-public-account", http.MethodGet, map[string]string{"userId": id, "userIdType": "user_id"})
	if err != nil {
		return v, false, err
	}
	if out.StatusCode == 404 {
		return v, false, nil
	}
	if out.StatusCode != 200 {
		return v, false, fmt.Errorf("Authing status %d", out.StatusCode)
	}
	if json.Unmarshal(out.Data, &v) != nil || v.UserID != id {
		return v, false, errors.New("get-public-account returned missing or mismatched userId")
	}
	return v, true, nil
}
func publicString(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
func publicApply(m *PublicAccountModel, v publicRemote) {
	m.ID = types.StringValue(v.UserID)
	m.Username = publicString(v.Username)
	m.Name = publicString(v.Name)
	m.Nickname = publicString(v.Nickname)
	m.Email = publicString(v.Email)
}
func publicBody(m PublicAccountModel) map[string]any {
	b := map[string]any{}
	for k, v := range map[string]types.String{"username": m.Username, "name": m.Name, "nickname": m.Nickname, "email": m.Email} {
		if !v.IsNull() && !v.IsUnknown() {
			b[k] = v.ValueString()
		}
	}
	return b
}
func publicWrite(ctx context.Context, c *authingapi.Client, endpoint string, body map[string]any) (string, error) {
	out, err := publicSend(ctx, c, endpoint, http.MethodPost, body)
	if err != nil {
		return "", err
	}
	if out.StatusCode != 200 {
		return "", fmt.Errorf("Authing status %d", out.StatusCode)
	}
	var v publicRemote
	if json.Unmarshal(out.Data, &v) != nil || v.UserID == "" {
		return "", errors.New("write response has no userId")
	}
	return v.UserID, nil
}
func (r *PublicAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m PublicAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := publicValidateFields(m); err != nil {
		resp.Diagnostics.AddError("Invalid public account configuration", err.Error())
		return
	}
	if m.Username.ValueString() == "" && m.Email.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid public account", "username or email must be set")
		return
	}
	id, err := publicWrite(ctx, r.client, "/api/v3/create-public-account", publicBody(m))
	if err != nil {
		resp.Diagnostics.AddError("Create public account failed", err.Error())
		return
	}
	v, found, err := publicGet(ctx, r.client, id)
	if err != nil || !found {
		resp.Diagnostics.AddError("Verify public account creation failed", fmt.Sprintf("Authing created public account %q but readback failed (%s). Import that ID before retrying to avoid creating a duplicate.", id, publicVerifyError(err)))
		return
	}
	if err := publicVerifyConfigured(m, v); err != nil {
		resp.Diagnostics.AddError("Verify public account creation failed", fmt.Sprintf("Authing created public account %q but %s. Import that ID before retrying.", id, err))
		return
	}
	publicApply(&m, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func publicValidateFields(m PublicAccountModel) error {
	for name, v := range map[string]types.String{"username": m.Username, "name": m.Name, "nickname": m.Nickname, "email": m.Email} {
		if !v.IsNull() && !v.IsUnknown() && v.ValueString() == "" {
			return fmt.Errorf("%s must be nonempty when configured; omit it to use the computed remote value", name)
		}
	}
	return nil
}
func publicVerifyConfigured(m PublicAccountModel, v publicRemote) error {
	for _, field := range []struct {
		name    string
		planned types.String
		remote  string
	}{
		{"username", m.Username, v.Username}, {"name", m.Name, v.Name}, {"nickname", m.Nickname, v.Nickname}, {"email", m.Email, v.Email},
	} {
		if !field.planned.IsNull() && !field.planned.IsUnknown() && !field.planned.Equal(publicString(field.remote)) {
			return fmt.Errorf("configured %s was not confirmed by exact-ID GET", field.name)
		}
	}
	return nil
}
func publicVerifyError(err error) string {
	if err != nil {
		return err.Error()
	}
	return "Public account was not found during verification"
}
func (r *PublicAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m PublicAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, found, err := publicGet(ctx, r.client, m.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Read public account failed", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	publicApply(&m, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *PublicAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m PublicAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := publicValidateFields(m); err != nil {
		resp.Diagnostics.AddError("Invalid public account configuration", err.Error())
		return
	}
	id := m.ID.ValueString()
	if id == "" {
		resp.Diagnostics.AddError("Invalid public account", "Missing user ID")
		return
	}
	body := publicBody(m)
	body["userId"] = id
	returned, err := publicWrite(ctx, r.client, "/api/v3/update-public-account", body)
	if err != nil {
		resp.Diagnostics.AddError("Update public account failed", err.Error())
		return
	}
	if returned != id {
		resp.Diagnostics.AddError("Update public account failed", "Authing returned a different userId")
		return
	}
	v, found, err := publicGet(ctx, r.client, id)
	if err != nil || !found {
		resp.Diagnostics.AddError("Verify public account update failed", publicVerifyError(err))
		return
	}
	if err := publicVerifyConfigured(m, v); err != nil {
		resp.Diagnostics.AddError("Verify public account update failed", err.Error())
		return
	}
	publicApply(&m, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *PublicAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m PublicAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := m.ID.ValueString()
	_, found, err := publicGet(ctx, r.client, id)
	if err != nil {
		resp.Diagnostics.AddError("Check public account before deletion failed", err.Error())
		return
	}
	if !found {
		return
	}
	out, err := publicSend(ctx, r.client, "/api/v3/delete-public-accounts-batch", http.MethodPost, map[string]any{"userIds": []string{id}})
	if err != nil {
		resp.Diagnostics.AddError("Delete public account failed", err.Error())
		return
	}
	if out.StatusCode != 200 || !membershipSuccess(out.Data) {
		resp.Diagnostics.AddError("Delete public account failed", fmt.Sprintf("Authing status %d or data.success not true", out.StatusCode))
		return
	}
	_, found, err = publicGet(ctx, r.client, id)
	if err != nil {
		resp.Diagnostics.AddError("Verify public account deletion failed", err.Error())
		return
	}
	if found {
		resp.Diagnostics.AddError("Verify public account deletion failed", "Public account still exists after deletion")
		return
	}
}
func (r *PublicAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" || strings.ContainsAny(req.ID, " 	\r\n:/") {
		resp.Diagnostics.AddError("Invalid public account import ID", "Use one canonical userId, without whitespace, scope prefixes or path separators.")
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
