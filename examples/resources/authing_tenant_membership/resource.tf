# The linked user must already exist in the userpool.
resource "authing_tenant_membership" "alice" {
  tenant_id    = "tenant-123"
  link_user_id = authing_user.alice.id
}
