data "authing_tenant_user" "member" {
  tenant_id    = "tenant-id"
  link_user_id = "userpool-user-id"
}

output "tenant_member_id" {
  value = data.authing_tenant_user.member.member_id
}
