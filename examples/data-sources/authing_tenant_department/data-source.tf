data "authing_tenant_department" "engineering" {
  tenant_id         = "tenant-id"
  organization_code = "engineering"
  department_id     = "system-department-id"
}

output "engineering_department_name" {
  value = data.authing_tenant_department.engineering.name
}
