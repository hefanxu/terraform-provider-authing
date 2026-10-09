resource "authing_organization" "acme" {
  organization_code = "acme_corp"
  organization_name = "Acme Corporation"
  description       = "Headquarters organization tree"
}

# Delete child departments in a prior apply before destroying this root:
# Authing's organization deletion removes the entire tree, so this provider refuses it while children exist.

resource "authing_department" "tech" {
  organization_code    = authing_organization.acme.organization_code
  name                 = "Technology Department"
  parent_department_id = "root"
}

resource "authing_department" "cloud_infra" {
  organization_code    = authing_organization.acme.organization_code
  name                 = "Cloud Infrastructure Team"
  parent_department_id = authing_department.tech.id
}

resource "authing_post" "lead_engineer" {
  code        = "lead_engineer"
  name        = "Lead Engineer"
  description = "Technical lead position"
}

resource "authing_department_member" "member" {
  organization_code = authing_organization.acme.organization_code
  department_id     = authing_department.cloud_infra.id
  user_id           = "6319a1504f3xxxxf214dd5b7"
}
