resource "authing_tenant" "example" {
  name          = "Example tenant"
  app_ids       = ["application-id"]
  description   = "Managed by Terraform"
  source_app_id = "application-id"
}
