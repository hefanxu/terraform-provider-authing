resource "authing_tenant_organization" "engineering" {
  tenant_id         = "your-tenant-id"
  organization_code = "engineering"
  organization_name = "Engineering"
  description       = "Engineering organization"
}
