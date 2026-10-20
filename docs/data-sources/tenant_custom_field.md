# authing_tenant_custom_field (data source)

Reads one custom field definition in an **explicit tenant**. It matches both `target_type` and `key` in the list returned by `GET /api/v3/get-custom-fields`; missing or ambiguous definitions fail rather than selecting another field. It never reads the unscoped user-pool list.

```hcl
data "authing_tenant_custom_field" "school" {
  tenant_id   = "tenant-id"
  target_type = "USER"
  key         = "school"
}

output "school_label" {
  value = data.authing_tenant_custom_field.school.label
}
```

## Arguments

- `tenant_id` (required): Nonempty tenant ID; provider-level tenant defaults are not used.
- `target_type` (required): `USER`, `ROLE`, or `DEPARTMENT`. The get endpoint explicitly does not support `GROUP`.
- `key` (required): Exact custom field key.

## Attributes

- `data_type`, `label`, `description`: Data type, display label, and optional description.
- `user_editable`, `visible_in_admin_console`, `visible_in_user_center`: Boolean visibility/editability flags; absent flags return null, not guessed defaults.

Definitions explicitly marked encrypted in the response are rejected; no field values, options, i18n, uniqueness, or advanced configuration are exposed. If the API omits the optional `encrypted` flag, encryption status cannot be inferred. Missing fields and API failures return errors.

## Why read-only?

Authing documents `set-custom-fields` as an upsert per `key`, not as a collection replacement. However, its write DTO accepts `validateRules`, `appIds`, and `desensitization`, none of which the list response returns. The API does not specify whether omitting these fields during an upsert preserves their values; a Terraform update could silently reset settings it cannot inspect. Deleting a definition can also remove associated user/department/role data. Until safe partial-update or complete readback semantics are documented and verified, the provider intentionally does not offer a tenant custom field resource. No create, update, delete, or import operations are available.
