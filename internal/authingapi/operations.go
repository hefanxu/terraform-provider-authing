// Code generated from the Management V3 endpoint signatures (SDK v3.0.15).
// DTOs are retained for response compatibility; requests use only the direct HTTP client.
package authingapi

import (
	"encoding/json"
	"net/http"

	"github.com/Authing/authing-golang-sdk/v3/dto"
)

type operation struct{ path, method string }

// operations is the auditable set of provider-facing API endpoints.
var operations = map[string]operation{
	"AddDepartmentMembers":      {"/api/v3/add-department-members", http.MethodPost},
	"AddGroupMembers":           {"/api/v3/add-group-members", http.MethodPost},
	"AssignRole":                {"/api/v3/assign-role", http.MethodPost},
	"CreateApplication":         {"/api/v3/create-application", http.MethodPost},
	"CreateDataPolicy":          {"/api/v3/create-data-policy", http.MethodPost},
	"CreateDepartment":          {"/api/v3/create-department", http.MethodPost},
	"CreateExtIdp":              {"/api/v3/create-ext-idp", http.MethodPost},
	"CreateGroup":               {"/api/v3/create-group", http.MethodPost},
	"CreateModel":               {"/api/v3/metadata/create-model", http.MethodPost},
	"CreateOrganization":        {"/api/v3/create-organization", http.MethodPost},
	"CreatePermissionNamespace": {"/api/v3/create-permission-namespace", http.MethodPost},
	"CreatePipelineFunction":    {"/api/v3/create-pipeline-function", http.MethodPost},
	"CreatePost":                {"/api/v3/create-post", http.MethodPost},
	"CreateResource":            {"/api/v3/create-resource", http.MethodPost},
	"CreateRole":                {"/api/v3/create-role", http.MethodPost},
	"CreateUser":                {"/api/v3/create-user", http.MethodPost},
	"CreateWebhook":             {"/api/v3/create-webhook", http.MethodPost},
	"DeleteApplication":         {"/api/v3/delete-application", http.MethodPost},
	"DeleteDataPolicy":          {"/api/v3/delete-data-policy", http.MethodPost},
	"DeleteDepartment":          {"/api/v3/delete-department", http.MethodPost},
	"DeleteExtIdp":              {"/api/v3/delete-ext-idp", http.MethodPost},
	"DeleteGroupsBatch":         {"/api/v3/delete-groups-batch", http.MethodPost},
	"DeleteOrganization":        {"/api/v3/delete-organization", http.MethodPost},
	"DeletePermissionNamespace": {"/api/v3/delete-permission-namespace", http.MethodPost},
	"DeletePipelineFunction":    {"/api/v3/delete-pipeline-function", http.MethodPost},
	"DeleteResource":            {"/api/v3/delete-resource", http.MethodPost},
	"DeleteRolesBatch":          {"/api/v3/delete-roles-batch", http.MethodPost},
	"DeleteUsersBatch":          {"/api/v3/delete-users-batch", http.MethodPost},
	"DeleteWebhook":             {"/api/v3/delete-webhook", http.MethodPost},
	"GetApplication":            {"/api/v3/get-application", http.MethodGet},
	"GetDataPolicy":             {"/api/v3/get-data-policy", http.MethodGet},
	"GetDepartment":             {"/api/v3/get-department", http.MethodGet},
	"GetExtIdp":                 {"/api/v3/get-ext-idp", http.MethodGet},
	"GetGroup":                  {"/api/v3/get-group", http.MethodGet},
	"GetModel":                  {"/api/v3/metadata/get-model", http.MethodGet},
	"GetOrganization":           {"/api/v3/get-organization", http.MethodGet},
	"GetPermissionNamespace":    {"/api/v3/get-permission-namespace", http.MethodGet},
	"GetPipelineFunction":       {"/api/v3/get-pipeline-function", http.MethodGet},
	"GetPost":                   {"/api/v3/get-post", http.MethodGet},
	"GetResource":               {"/api/v3/get-resource", http.MethodGet},
	"GetRole":                   {"/api/v3/get-role", http.MethodGet},
	"GetUser":                   {"/api/v3/get-user", http.MethodGet},
	"GetUserGroups":             {"/api/v3/get-user-groups", http.MethodGet},
	"GetWebhook":                {"/api/v3/get-webhook", http.MethodGet},
	"ListUsers":                 {"/api/v3/list-users", http.MethodPost},
	"RemoveDepartmentMembers":   {"/api/v3/remove-department-members", http.MethodPost},
	"RemoveGroupMembers":        {"/api/v3/remove-group-members", http.MethodPost},
	"RemoveModel":               {"/api/v3/metadata/remove-model", http.MethodPost},
	"RemovePost":                {"/api/v3/remove-post", http.MethodPost},
	"RevokeRole":                {"/api/v3/revoke-role", http.MethodPost},
	"UpdateDataPolicy":          {"/api/v3/update-data-policy", http.MethodPost},
	"UpdateDepartment":          {"/api/v3/update-department", http.MethodPost},
	"UpdateExtIdp":              {"/api/v3/update-ext-idp", http.MethodPost},
	"UpdateGroup":               {"/api/v3/update-group", http.MethodPost},
	"UpdateOrganization":        {"/api/v3/update-organization", http.MethodPost},
	"UpdatePermissionNamespace": {"/api/v3/update-permission-namespace", http.MethodPost},
	"UpdatePipelineFunction":    {"/api/v3/update-pipeline-function", http.MethodPost},
	"UpdatePost":                {"/api/v3/update-post", http.MethodPost},
	"UpdateResource":            {"/api/v3/update-resource", http.MethodPost},
	"UpdateRole":                {"/api/v3/update-role", http.MethodPost},
	"UpdateUser":                {"/api/v3/update-user", http.MethodPost},
	"UpdateWebhook":             {"/api/v3/update-webhook", http.MethodPost},
}

// decodeOperation keeps a valid Authing 404 body so Read can distinguish
// a deleted object from transport failure. Other HTTP failures have no
// trustworthy typed result. Business statusCode in successful HTTP responses
// is deliberately returned unchanged for provider-side handling.
func decodeOperation[T any](c *Client, name string, payload any) *T {
	op := operations[name]
	body, err := c.SendHttpRequest(op.path, op.method, payload)
	var envelope struct {
		StatusCode *int `json:"statusCode"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.StatusCode == nil {
		return nil
	}
	if err != nil && *envelope.StatusCode != http.StatusNotFound {
		return nil
	}
	var response T
	if json.Unmarshal(body, &response) != nil {
		return nil
	}
	return &response
}

func (c *Client) AddDepartmentMembers(reqDto *dto.AddDepartmentMembersReqDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "AddDepartmentMembers", reqDto)
}

func (c *Client) AddGroupMembers(reqDto *dto.AddGroupMembersReqDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "AddGroupMembers", reqDto)
}

func (c *Client) AssignRole(reqDto *dto.AssignRoleDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "AssignRole", reqDto)
}

func (c *Client) CreateApplication(reqDto *dto.CreateApplicationDto) *dto.CreateApplicationRespDto {
	return decodeOperation[dto.CreateApplicationRespDto](c, "CreateApplication", reqDto)
}

func (c *Client) CreateDataPolicy(reqDto *dto.CreateDataPolicyDto) *dto.CreateDataPolicyResponseDto {
	return decodeOperation[dto.CreateDataPolicyResponseDto](c, "CreateDataPolicy", reqDto)
}

func (c *Client) CreateDepartment(reqDto *dto.CreateDepartmentReqDto) *dto.DepartmentSingleRespDto {
	return decodeOperation[dto.DepartmentSingleRespDto](c, "CreateDepartment", reqDto)
}

func (c *Client) CreateExtIdp(reqDto *dto.CreateExtIdpDto) *dto.ExtIdpSingleRespDto {
	return decodeOperation[dto.ExtIdpSingleRespDto](c, "CreateExtIdp", reqDto)
}

func (c *Client) CreateGroup(reqDto *dto.CreateGroupReqDto) *dto.GroupSingleRespDto {
	return decodeOperation[dto.GroupSingleRespDto](c, "CreateGroup", reqDto)
}

func (c *Client) CreateModel(reqDto *dto.CreateFunctionModelDto) *dto.FunctionModelResDto {
	return decodeOperation[dto.FunctionModelResDto](c, "CreateModel", reqDto)
}

func (c *Client) CreateOrganization(reqDto *dto.CreateOrganizationReqDto) *dto.OrganizationSingleRespDto {
	return decodeOperation[dto.OrganizationSingleRespDto](c, "CreateOrganization", reqDto)
}

func (c *Client) CreatePermissionNamespace(reqDto *dto.CreatePermissionNamespaceDto) *dto.CreatePermissionNamespaceResponseDto {
	return decodeOperation[dto.CreatePermissionNamespaceResponseDto](c, "CreatePermissionNamespace", reqDto)
}

func (c *Client) CreatePipelineFunction(reqDto *dto.CreatePipelineFunctionDto) *dto.PipelineFunctionSingleRespDto {
	return decodeOperation[dto.PipelineFunctionSingleRespDto](c, "CreatePipelineFunction", reqDto)
}

func (c *Client) CreatePost(reqDto *dto.CreatePostDto) *dto.CreatePostRespDto {
	return decodeOperation[dto.CreatePostRespDto](c, "CreatePost", reqDto)
}

func (c *Client) CreateResource(reqDto *dto.CreateResourceDto) *dto.ResourceRespDto {
	return decodeOperation[dto.ResourceRespDto](c, "CreateResource", reqDto)
}

func (c *Client) CreateRole(reqDto *dto.CreateRoleDto) *dto.RoleSingleRespDto {
	return decodeOperation[dto.RoleSingleRespDto](c, "CreateRole", reqDto)
}

func (c *Client) CreateUser(reqDto *dto.CreateUserReqDto) *dto.UserSingleRespDto {
	return decodeOperation[dto.UserSingleRespDto](c, "CreateUser", reqDto)
}

func (c *Client) CreateWebhook(reqDto *dto.CreateWebhookDto) *dto.CreateWebhookRespDto {
	return decodeOperation[dto.CreateWebhookRespDto](c, "CreateWebhook", reqDto)
}

func (c *Client) DeleteApplication(reqDto *dto.DeleteApplicationDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "DeleteApplication", reqDto)
}

func (c *Client) DeleteDataPolicy(reqDto *dto.DeleteDataPolicyDto) *dto.CommonResponseDto {
	return decodeOperation[dto.CommonResponseDto](c, "DeleteDataPolicy", reqDto)
}

func (c *Client) DeleteDepartment(reqDto *dto.DeleteDepartmentReqDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "DeleteDepartment", reqDto)
}

func (c *Client) DeleteExtIdp(reqDto *dto.DeleteExtIdpDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "DeleteExtIdp", reqDto)
}

func (c *Client) DeleteGroupsBatch(reqDto *dto.DeleteGroupsReqDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "DeleteGroupsBatch", reqDto)
}

func (c *Client) DeleteOrganization(reqDto *dto.DeleteOrganizationReqDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "DeleteOrganization", reqDto)
}

func (c *Client) DeletePermissionNamespace(reqDto *dto.DeletePermissionNamespaceDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "DeletePermissionNamespace", reqDto)
}

func (c *Client) DeletePipelineFunction(reqDto *dto.DeletePipelineFunctionDto) *dto.CommonResponseDto {
	return decodeOperation[dto.CommonResponseDto](c, "DeletePipelineFunction", reqDto)
}

func (c *Client) DeleteResource(reqDto *dto.DeleteResourceDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "DeleteResource", reqDto)
}

func (c *Client) DeleteRolesBatch(reqDto *dto.DeleteRoleDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "DeleteRolesBatch", reqDto)
}

func (c *Client) DeleteUsersBatch(reqDto *dto.DeleteUsersBatchDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "DeleteUsersBatch", reqDto)
}

func (c *Client) DeleteWebhook(reqDto *dto.DeleteWebhookDto) *dto.DeleteWebhookRespDto {
	return decodeOperation[dto.DeleteWebhookRespDto](c, "DeleteWebhook", reqDto)
}

func (c *Client) GetApplication(reqDto *dto.GetApplicationDto) *dto.ApplicationSingleRespDto {
	return decodeOperation[dto.ApplicationSingleRespDto](c, "GetApplication", reqDto)
}

func (c *Client) GetDataPolicy(reqDto *dto.GetDataPolicyDto) *dto.GetDataPolicyResponseDto {
	return decodeOperation[dto.GetDataPolicyResponseDto](c, "GetDataPolicy", reqDto)
}

func (c *Client) GetDepartment(reqDto *dto.GetDepartmentDto) *dto.DepartmentSingleRespDto {
	return decodeOperation[dto.DepartmentSingleRespDto](c, "GetDepartment", reqDto)
}

func (c *Client) GetExtIdp(reqDto *dto.GetExtIdpDto) *dto.ExtIdpDetailSingleRespDto {
	return decodeOperation[dto.ExtIdpDetailSingleRespDto](c, "GetExtIdp", reqDto)
}

func (c *Client) GetGroup(reqDto *dto.GetGroupDto) *dto.GroupSingleRespDto {
	return decodeOperation[dto.GroupSingleRespDto](c, "GetGroup", reqDto)
}

func (c *Client) GetModel(reqDto *dto.GetModelDto) *dto.FunctionModelResDto {
	return decodeOperation[dto.FunctionModelResDto](c, "GetModel", reqDto)
}

func (c *Client) GetOrganization(reqDto *dto.GetOrganizationDto) *dto.OrganizationSingleRespDto {
	return decodeOperation[dto.OrganizationSingleRespDto](c, "GetOrganization", reqDto)
}

func (c *Client) GetPermissionNamespace(reqDto *dto.GetPermissionNamespaceDto) *dto.GetPermissionNamespaceResponseDto {
	return decodeOperation[dto.GetPermissionNamespaceResponseDto](c, "GetPermissionNamespace", reqDto)
}

func (c *Client) GetPipelineFunction(reqDto *dto.GetPipelineFunctionDto) *dto.PipelineFunctionSingleRespDto {
	return decodeOperation[dto.PipelineFunctionSingleRespDto](c, "GetPipelineFunction", reqDto)
}

func (c *Client) GetPost(reqDto *dto.GetPostDto) *dto.CreatePostDto {
	return decodeOperation[dto.CreatePostDto](c, "GetPost", reqDto)
}

func (c *Client) GetResource(reqDto *dto.GetResourceDto) *dto.ResourceRespDto {
	return decodeOperation[dto.ResourceRespDto](c, "GetResource", reqDto)
}

func (c *Client) GetRole(reqDto *dto.GetRoleDto) *dto.RoleSingleRespDto {
	return decodeOperation[dto.RoleSingleRespDto](c, "GetRole", reqDto)
}

func (c *Client) GetUser(reqDto *dto.GetUserDto) *dto.UserSingleRespDto {
	return decodeOperation[dto.UserSingleRespDto](c, "GetUser", reqDto)
}

func (c *Client) GetUserGroups(reqDto *dto.GetUserGroupsDto) *dto.GroupPaginatedRespDto {
	return decodeOperation[dto.GroupPaginatedRespDto](c, "GetUserGroups", reqDto)
}

func (c *Client) GetWebhook(reqDto *dto.GetWebhookDto) *dto.GetWebhookRespDto {
	return decodeOperation[dto.GetWebhookRespDto](c, "GetWebhook", reqDto)
}

func (c *Client) ListUsers(reqDto *dto.ListUsersRequestDto) *dto.UserPaginatedRespDto {
	return decodeOperation[dto.UserPaginatedRespDto](c, "ListUsers", reqDto)
}

func (c *Client) RemoveDepartmentMembers(reqDto *dto.RemoveDepartmentMembersReqDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "RemoveDepartmentMembers", reqDto)
}

func (c *Client) RemoveGroupMembers(reqDto *dto.RemoveGroupMembersReqDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "RemoveGroupMembers", reqDto)
}

func (c *Client) RemoveModel(reqDto *dto.FunctionModelIdDto) *dto.CommonResponseDto {
	return decodeOperation[dto.CommonResponseDto](c, "RemoveModel", reqDto)
}

func (c *Client) RemovePost(reqDto *dto.RemovePostDto) *dto.CommonResponseDto {
	return decodeOperation[dto.CommonResponseDto](c, "RemovePost", reqDto)
}

func (c *Client) RevokeRole(reqDto *dto.RevokeRoleDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "RevokeRole", reqDto)
}

func (c *Client) UpdateDataPolicy(reqDto *dto.UpdateDataPolicyDto) *dto.UpdateDataPolicyResponseDto {
	return decodeOperation[dto.UpdateDataPolicyResponseDto](c, "UpdateDataPolicy", reqDto)
}

func (c *Client) UpdateDepartment(reqDto *dto.UpdateDepartmentReqDto) *dto.DepartmentSingleRespDto {
	return decodeOperation[dto.DepartmentSingleRespDto](c, "UpdateDepartment", reqDto)
}

func (c *Client) UpdateExtIdp(reqDto *dto.UpdateExtIdpDto) *dto.ExtIdpSingleRespDto {
	return decodeOperation[dto.ExtIdpSingleRespDto](c, "UpdateExtIdp", reqDto)
}

func (c *Client) UpdateGroup(reqDto *dto.UpdateGroupReqDto) *dto.GroupSingleRespDto {
	return decodeOperation[dto.GroupSingleRespDto](c, "UpdateGroup", reqDto)
}

func (c *Client) UpdateOrganization(reqDto *dto.UpdateOrganizationReqDto) *dto.OrganizationSingleRespDto {
	return decodeOperation[dto.OrganizationSingleRespDto](c, "UpdateOrganization", reqDto)
}

func (c *Client) UpdatePermissionNamespace(reqDto *dto.UpdatePermissionNamespaceDto) *dto.UpdatePermissionNamespaceResponseDto {
	return decodeOperation[dto.UpdatePermissionNamespaceResponseDto](c, "UpdatePermissionNamespace", reqDto)
}

func (c *Client) UpdatePipelineFunction(reqDto *dto.UpdatePipelineFunctionDto) *dto.PipelineFunctionSingleRespDto {
	return decodeOperation[dto.PipelineFunctionSingleRespDto](c, "UpdatePipelineFunction", reqDto)
}

func (c *Client) UpdatePost(reqDto *dto.CreatePostDto) *dto.CreatePostRespDto {
	return decodeOperation[dto.CreatePostRespDto](c, "UpdatePost", reqDto)
}

func (c *Client) UpdateResource(reqDto *dto.UpdateResourceDto) *dto.ResourceRespDto {
	return decodeOperation[dto.ResourceRespDto](c, "UpdateResource", reqDto)
}

func (c *Client) UpdateRole(reqDto *dto.UpdateRoleDto) *dto.IsSuccessRespDto {
	return decodeOperation[dto.IsSuccessRespDto](c, "UpdateRole", reqDto)
}

func (c *Client) UpdateUser(reqDto *dto.UpdateUserReqDto) *dto.UserSingleRespDto {
	return decodeOperation[dto.UserSingleRespDto](c, "UpdateUser", reqDto)
}

func (c *Client) UpdateWebhook(reqDto *dto.UpdateWebhookDto) *dto.UpdateWebhooksRespDto {
	return decodeOperation[dto.UpdateWebhooksRespDto](c, "UpdateWebhook", reqDto)
}
