# Grant an existing tenant member administrator privilege.
resource "authing_tenant_admin" "alice" {
  tenant_id    = authing_tenant_membership.alice.tenant_id
  link_user_id = authing_tenant_membership.alice.link_user_id
}
