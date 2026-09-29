resource "authing_namespace" "core" {
  code        = "core_iam"
  name        = "Core IAM Permission Space"
  description = "RBAC and PBAC space for core systems"
}

resource "authing_role" "platform_admin" {
  code        = "platform_admin"
  name        = "Platform Administrator"
  namespace   = authing_namespace.core.code
  description = "Full control role"
}

resource "authing_resource" "audit_api" {
  code        = "audit_api"
  type        = "API"
  namespace   = authing_namespace.core.code
  description = "Audit logs API"

  actions = [
    {
      name        = "read"
      description = "Read audit logs"
    },
    {
      name        = "export"
      description = "Export audit log records"
    }
  ]
}

resource "authing_data_policy" "audit_policy" {
  policy_name = "Audit Team Policy"
  description = "Permissions to view and export audit resources"

  statement_list = [
    {
      effect = "ALLOW"
      permissions = [
        "core_iam/audit_api/read",
        "core_iam/audit_api/export"
      ]
    }
  ]
}

resource "authing_role_assignment" "admin_assign" {
  role_code   = authing_role.platform_admin.code
  namespace   = authing_namespace.core.code
  target_type = "USER"
  target_id   = "6319a1504f3xxxxf214dd5b7"
}
