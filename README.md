# OpenTofu / Terraform Provider for Authing

A Terraform / OpenTofu provider for managing [Authing](https://www.authing.cn) IAM Identity Cloud Platform resources.

Built with [Terraform Plugin Framework](https://github.com/hashicorp/terraform-plugin-framework). Management API requests use this provider's own `net/http` client, **not** the Authing SDK transport. Authing SDK v3 DTO types are currently retained for request/response compatibility.

The direct client obtains a management access token using AK/SK, caches it until near expiry, and sends requests over HTTPS with normal certificate verification. `host` permits plain HTTP only for a loopback development server; redirects are refused so credentials and bearer tokens are not forwarded to another host. No real Authing tenant integration test has been performed yet.

---

## Supported Capabilities

### 1. Identity & Users (用户管理)
- **`authing_user`** (Resource): Create, update, delete and import users.
- **`authing_user`** (Data Source): Look up a single user by User ID.
- **`authing_users`** (Data Source): Search and query user lists by keywords.
- **`authing_public_account`** (Resource / Data Source): Manage and look up basic public-account details. Do not manage the same user ID as `authing_user`.

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
- **`authing_data_resource`** (Resource / Data Source): Manage STRING, ARRAY and validated TREE data resources; existing extensions can be read, but parent updates are blocked while extensions exist.
- **`authing_data_resource_extension_field`** (Data Source): Read a TREE extension definition by exact key. A parent data resource can refresh with extensions present, but updates remain blocked to avoid dropping them.
- **`authing_data_policy_assignment`** (Resource): Authorize one policy for one subject, with paginated readback and targeted revocation.
- **`authing_invitation_policy`** (Resource): Manage persistent invitation-policy settings; sending an invitation is not a resource.
- **`authing_invitation_roster`** (Resource): Manage persistent invitation rosters and their optional policy association; sending invitations is excluded.
- **`authing_invitation_invitee`** (Resource): Manage a persistent invitee roster entry without sending an invitation; names, email addresses and phone numbers remain in Terraform state.
- **`authing_data_object_row`** (Data Source): Read a data-object row by model and row ID. Cell values are sensitive and remain in Terraform state; row writes await a verified field-ID/key contract.

### 5. Applications & Integration (应用与集成)
- **`authing_application`** (Resource / Data Source): Self-built applications, OAuth/OIDC redirect URLs.
- **`authing_application.permission_strategy`** (Attribute): Manage the persistent default access strategy within the application lifecycle.
- **`authing_ext_idp`** (Resource): External enterprise identity providers (SAML, OIDC, WeChat, DingTalk, LDAP).
- **`authing_ext_idp_connection`** (Data Source): Read allowlisted connection metadata by parent and connection ID; secret-containing fields are not exposed.
- **`authing_global_security_settings`** / **`authing_global_mfa_settings`** (Data Sources): Read nonsecret user-pool security flags and enabled MFA factors. Neither endpoint verifies tenant scope or offers managed lifecycle operations.
- **`authing_webhook`** (Resource): Webhook subscriptions for identity event streams.
- **`authing_pipeline_function`** (Resource): Serverless pipeline extension functions (pre-register, post-auth, etc.).
- **`authing_auth_flow_function`** (Resource): Manage AuthFlowFunction source and settings; source is sensitive but still stored in Terraform state.
- **`authing_application_subject_auth`** (Data Source): Read an application's authorization details for a subject.
- **`authing_custom_domain`** (Resource): Manage the user-pool custom-domain name and public DNS status without storing certificate private keys.

### 6. Devices (终端)
- **`authing_device_status`** (Data Source): Read terminal status. The OpenAPI contains an untagged `add-device` (and a deprecated `create-device`) endpoint, but they have no declared management security scheme and the management API does not expose a full device detail readback; this provider intentionally does not model complete device CRUD yet.
- **`authing_device_exclusive_rule_settings`** / **`authing_device_exclusive_valid_scope_settings`** (Data Sources): Read nonsecret device-exclusivity rules and app scope; no unsupported singleton destroy semantics.

### 7. Multi-tenancy (多租户)
- **`authing_tenant`** (Resource / Data Source): Manage tenants and their complete associated application ID set.
- **`authing_tenant_membership`** (Resource): Attach an existing user-pool user to a tenant without deleting the user on detach.
- **`authing_tenant_admin`** (Resource): Grant or revoke tenant administrator privilege for an existing member.
- **`authing_tenant_user`** (Data Source): Look up a tenant member by exactly one identifier without placing password or salt in state.
- **`authing_tenant_organization`** (Resource): Manage an organization with explicit tenant scope and refuse deletion when its child departments are present or unknown.
- **`authing_tenant_custom_field`** (Data Source): Look up a tenant-scoped custom-field definition; writes are deferred until safe round-trip semantics are established.
- **`authing_tenant_department`** (Data Source): Read a department with explicit tenant and organization queries; the department response has no tenant ID, so this is not proof of tenant ownership. Tenant department writes remain unsupported pending cross-tenant isolation verification.

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
  type        = "static" # Authing's documented example; select the type supported by your tenant.
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

## Opt-in Authing sandbox read-only acceptance

The separate **Authing sandbox acceptance** GitHub Actions workflow (`.github/workflows/authing-acceptance.yml`) runs **only** from a manual `workflow_dispatch`. It does not run on push or pull requests. Create the `authing-sandbox` GitHub Environment and configure its environment-scoped secrets `AUTHING_ACCESS_KEY_ID` and `AUTHING_ACCESS_KEY_SECRET` with credentials for an isolated sandbox user pool. Optionally configure the environment variable `AUTHING_HOST` for a custom HTTPS Authing API host (otherwise the provider uses `https://api.authing.cn`). Do not commit, paste, or log credential values. Restrict access to the environment with GitHub environment protection rules as appropriate.

In GitHub **Actions → Authing sandbox acceptance → Run workflow**, select the branch with the workflow and type the exact confirmation `READ_ONLY_SANDBOX`. Anything else skips the job; the test additionally requires its explicit `-authing-read-only-sandbox` flag, the exact confirmation, and both credentials. The workflow verifies the provider with checksum-pinned Terraform, runs offline/mock tests, then runs `terraform plan` on only `data.authing_global_security_settings.sandbox` via a temporary dev override. The authenticated provider may POST to obtain a management token, then GET `/api/v3/get-security-settings`; it never applies or destroys, changes user-pool data, or writes a Terraform state file. Terraform output is suppressed to avoid credential disclosure. A successful plan validates this single lookup against the live sandbox API, **not** CRUD behavior, tenant scope, or other data sources.

Offline check: `python3 scripts/protocol_smoke.py && go test -count=1 ./scripts/acceptance` (the live test skips without the flag). Never run the live test with production credentials.

## Opt-in destructive sandbox acceptance

The **same manual-only workflow** accepts an exact `DESTRUCTIVE_SANDBOX` confirmation and one `test_case` choice from the workflow's allowlist. It executes **only the selected case** on `refs/heads/feat/management-api-coverage`; other jobs are skipped. The selector verifies the exact Go test exists, rejects unknown choices and suppresses untrusted failure output. `authing-sandbox` is restricted to this branch and requires approval from the repository owner before credentials become available. The test itself additionally requires `-authing-destructive-sandbox` and the exact confirmation; ordinary `go test ./...` skips live access. Never use production credentials or a shared production pool. The read-only job and its `READ_ONLY_SANDBOX` gate remain independent. See `docs/acceptance-matrix.csv`: listing a resource there is a gap inventory, **not evidence of a passing live run**. Organization deletion may cascade; role/resource fixtures refuse namespace cleanup when global data policies exist, which can leave test objects for manual review. Select these cases only after reviewing their exact safeguards. The application permission-strategy case has passed live create → strategy drift detection/reconciliation → normal destroy in an isolated pool; the app-name update path is still unverified because the live `/update-application` mutation returned `200` with `success=false`. An earlier sandbox run also returned `402/4021` plan/billing errors; that gate was not seen in the later successful create, and the reason it changed is unverified. Do not generalize the strategy pass to every exposed application field or point destructive tests at production.

The group tracer generates a unique `hermesacc-` code and name, checks the code is absent, then builds the provider into a temporary directory and uses Terraform `dev_overrides`. It executes apply → no-change plan → direct Management API `UpdateGroup` drift → changed plan → apply → no-change plan → destroy → API GET (404). The temporary HCL contains only code/name/description/type, never credentials; Terraform state and provider binaries are temporary. On failure it attempts bounded, conditional cleanup **only** for the exact code plus matching ownership description, group type, and expected name. If ownership or absence cannot be proved, it refuses to delete and reports the code for manual inspection. Terraform output and raw API diagnostics are discarded. Crashes, timeouts, authorization failures, and eventual-consistency races can still leave a sandbox group behind; inspect the reported code before manual cleanup. This tests one group lifecycle, not imports, other resources, or production readiness. A successful local mock run does not establish live tenant acceptance.

For offline verification, run `go test -count=1 -run '^(TestDestructiveGuard|TestMockDestructive.*)$' ./scripts/acceptance`. The live command is intentionally not shown as a routine local check; use the protected workflow after review.

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
