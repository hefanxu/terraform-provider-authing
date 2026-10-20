# authing_tenant_department (data source)

Read-only lookup of a department using an explicit tenant, organization code, and system department ID.

```hcl
data "authing_tenant_department" "engineering" {
  tenant_id         = "tenant-id"
  organization_code = "engineering"
  department_id     = "system-department-id"
}

output "department_name" {
  value = data.authing_tenant_department.engineering.name
}
```

The provider first calls `get-organization` with `tenantId` and `organizationCode` and requires the returned tenant ID and organization code to match. It then calls `get-department` with `tenantId`, `organizationCode`, `departmentId`, and `departmentIdType=department_id`, requiring the returned organization code and department ID to match. Failed, missing, or mismatched responses are errors, not absence. `id` encodes the three requested lookup keys, **not a verified tenant identity for the returned department**.

**Contract limitation:** Authing's `DepartmentDto` does not return `tenantId`; its parent department also lacks independently verifiable tenant ownership. A matching organization lookup cannot prove that the department response belongs to the same tenant if department routing ignores `tenantId` or an organization code is reused across tenants. Therefore this lookup is not an ownership attestation. No `authing_tenant_department` resource (create/update/delete/import) is exposed; destructive management needs a live cross-tenant isolation test or an API contract returning a tenant-bound department identity. The existing unscoped `authing_department` resource is separate and should not be used as a tenant-scoped substitute.
