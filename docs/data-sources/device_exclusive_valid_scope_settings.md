---
page_title: "authing_device_exclusive_valid_scope_settings Data Source - Authing"
subcategory: "Security"
description: |-
  Read application IDs covered by device-exclusive settings.
---

# authing_device_exclusive_valid_scope_settings (Data Source)

Reads `/api/v3/get-device-exclusive-valid-scope-settings` without arguments or writes. Returns the application IDs from the complete response array, in API order. Empty scope is an empty list. Each entry must have an application ID and a valid `createdAt` timestamp; duplicate IDs, malformed entries, and API errors fail the lookup. Names, logos, and raw API responses are not stored.

The endpoint has no `tenantId` query parameter or returned tenant identity. A configured provider `tenant_id` header does **not** verify tenant scope. This data source cannot change scope; save/remove semantics have not been established.

## Example Usage

```terraform
data "authing_device_exclusive_valid_scope_settings" "current" {}

output "device_exclusive_app_ids" {
  value = data.authing_device_exclusive_valid_scope_settings.current.app_ids
}
```

## Attribute Reference

- `app_ids` (List of String) — In-scope application IDs in API order.
