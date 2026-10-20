---
page_title: "authing_global_security_settings Data Source - Authing"
subcategory: "Security"
description: |-
  Read selected nonsecret Authing user-pool security settings.
---

# authing_global_security_settings (Data Source)

Reads `/api/v3/get-security-settings` without arguments or writes. Only two required Boolean fields from `SecuritySettingsDto` are stored. No origins, cookie settings, nested strategies, secrets, or raw API responses are exposed. Missing or malformed required fields and API errors fail the lookup rather than returning default flags.

This endpoint has no `tenantId` parameter or returned tenant identifier; a configured provider `tenant_id` header does **not** verify tenant scope. The data source is read-only; it cannot create, update, or delete security settings.

## Example Usage

```terraform
data "authing_global_security_settings" "current" {}

output "registration_disabled" {
  value = data.authing_global_security_settings.current.register_disabled
}
```

## Attribute Reference

- `register_disabled` (Boolean) — Whether user self-registration is disabled.
- `login_require_email_verified` (Boolean) — Whether an email address must be verified to log in with email.
