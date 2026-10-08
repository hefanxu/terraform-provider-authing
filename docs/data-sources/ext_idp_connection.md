---
page_title: "authing_ext_idp_connection Data Source - Authing"
subcategory: "Identity Providers"
description: |-
  Look up an external identity provider connection without storing connection configuration secrets.
---

# authing_ext_idp_connection (Data Source)

Reads a connection by listing `/api/v3/list-ext-idp-conns?id=<ext_idp_id>` and requiring an exact match on both the returned `id` and `extIdpId`. Authing does not expose a single-connection GET endpoint in the current Management API schema. A missing, duplicate, or mismatched connection fails the lookup rather than returning another connection.

The API response includes `fields`, which can contain credentials such as `client_secret`. This data source deliberately **does not expose or store `fields`** in Terraform state, diagnostics, or provider logs. Only the metadata below is returned. Do not infer that a connection is tenant-scoped: the list endpoint has no `tenantId` query parameter or verified tenant identifier in its response. Configuring the provider's `tenant_id` header does **not** establish a verified tenant-scoped lookup; do not use this data source as proof of tenant ownership or isolation.

## Example Usage

```terraform
data "authing_ext_idp_connection" "corporate" {
  ext_idp_id    = "6318061be13c0ce6a64093e5"
  connection_id = "60b49eb83fd80adb96f26e68"
}

output "corporate_connection_type" {
  value = data.authing_ext_idp_connection.corporate.type
}
```

## Argument Reference

- `ext_idp_id` (Required, String) — Parent external identity provider ID.
- `connection_id` (Required, String) — Connection ID in that identity provider.

## Attribute Reference

- `type` (String) — Connection type, or null when not returned.
- `identifier` (String) — Connection identifier, or null when not returned.
- `display_name` (String) — Display name, or null when not returned.
- `logo` (String) — Logo URL, or null when not returned.
- `login_only` (Boolean) — Login-only flag, or null when not returned.
