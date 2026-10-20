data "authing_tenant_custom_field" "school" {
  tenant_id   = "tenant-id"
  target_type = "USER"
  key         = "school"
}

output "school_custom_field_label" {
  value = data.authing_tenant_custom_field.school.label
}
