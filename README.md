# OpenTofu / Terraform Provider for Authing

A Terraform / OpenTofu provider for managing [Authing](https://www.authing.cn) IAM Identity Cloud Platform resources.

Built with [Terraform Plugin Framework](https://github.com/hashicorp/terraform-plugin-framework) and the [Authing Golang SDK v3](https://github.com/Authing/authing-golang-sdk).

---

## Supported Capabilities

### 1. Identity & Users (用户管理)
- **`authing_user`** (Resource): Create, update, delete and import users.
- **`authing_user`** (Data Source): Look up a single user by User ID.
- **`authing_users`** (Data Source): Search and query user lists by keywords.

### 2. User Groups (用户分组)
- **`authing_group`** (Resource / Data Source): Create and manage user groups.
- **`authing_group_member`** (Resource): Add and remove users to/from groups.

### 3. Organization & Departments (组织机构与部门)
- **`authing_organization`** (Resource / Data Source): Create and manage organizational trees.
- **`authing_department`** (Resource / Data Source): Create hierarchical department structures.
- **`authing_department_member`** (Resource): Bind users to departments.
- **`authing_post`** (Resource): Job titles and positions.

### 4. Permission Management & RBAC (权限与角色)
- **`authing_namespace`** (Resource / Data Source): Permission namespaces (权限空间).
- **`authing_role`** (Resource / Data Source): Roles within a namespace.
- **`authing_role_assignment`** (Resource): Assign roles to users or departments.
- **`authing_resource`** (Resource / Data Source): Define API, DATA, UI, BUTTON, and MENU resources with actions.
- **`authing_data_policy`** (Resource): Fine-grained data access policies (ALLOW/DENY statement lists).

### 5. Applications & Integration (应用与集成)
- **`authing_application`** (Resource / Data Source): Self-built applications, OAuth/OIDC redirect URLs.
- **`authing_ext_idp`** (Resource): External enterprise identity providers (SAML, OIDC, WeChat, DingTalk, LDAP).
- **`authing_webhook`** (Resource): Webhook subscriptions for identity event streams.
- **`authing_pipeline_function`** (Resource): Serverless pipeline extension functions (pre-register, post-auth, etc.).

---

## Example Usage

```hcl
terraform {
  required_providers {
    authing = {
      source  = "hefanxu/authing"
      version = "~> 1.0.0"
    }
  }
}

provider "authing" {
  access_key_id     = "YOUR_AUTHING_USERPOOL_ID"
  access_key_secret = "YOUR_AUTHING_USERPOOL_SECRET"
  # host            = "https://api.authing.cn" # Optional custom domain
}

# 1. Create a user
resource "authing_user" "developer" {
  username = "dev_zhang"
  email    = "zhang@example.com"
  nickname = "San Zhang"
  status   = "Activated"
  gender   = "M"
}

# 2. Create a user group and add user
resource "authing_group" "backend_team" {
  code        = "backend_devs"
  name        = "Backend Development Team"
  description = "Engineers working on backend services"
}

resource "authing_group_member" "dev_member" {
  group_code = authing_group.backend_team.code
  user_id    = authing_user.developer.id
}

# 3. Create a permission namespace and role
resource "authing_namespace" "core_system" {
  code        = "core_system"
  name        = "Core System Permissions"
  description = "Permissions for core platform"
}

resource "authing_role" "system_admin" {
  code        = "admin"
  name        = "System Administrator"
  namespace   = authing_namespace.core_system.code
  description = "Full access to core system"
}

resource "authing_role_assignment" "admin_assign" {
  role_code   = authing_role.system_admin.code
  namespace   = authing_namespace.core_system.code
  target_type = "USER"
  target_id   = authing_user.developer.id
}

# 4. Create an Application
resource "authing_application" "portal_app" {
  app_name      = "Company Portal"
  redirect_uris = ["https://portal.example.com/callback"]
  description   = "Internal employee dashboard portal"
}
```

---

## Local Development & Debugging

Compile the binary:
```powershell
go build -o terraform-provider-authing.exe .
```

Configure `.tofurc` or `terraform.rc`:
```hcl
provider_installation {
  dev_overrides {
    "hefanxu/authing" = "D:/dev/shcgravity/terraform-provider-authing"
  }
  direct {}
}
```
