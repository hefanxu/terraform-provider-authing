---
page_title: "authing_global_mfa_settings Data Source - Authing"
subcategory: "Security"
description: |-
  Read enabled Authing global MFA factors.
---

# authing_global_mfa_settings (Data Source)

Reads `/api/v3/get-global-mfa-settings` without arguments or writes. The only exposed field is `enabledFactors` from `MFASettingsDto`. A returned empty array stays an empty list, while missing, null, malformed, or unknown factors fail the lookup. The source never stores raw responses or secrets.

This endpoint has no `tenantId` parameter or returned tenant identifier; a configured provider `tenant_id` header does **not** verify tenant scope. The data source cannot create, update, or delete MFA settings.

## Example Usage

```terraform
data "authing_global_mfa_settings" "current" {}

output "enabled_mfa_factors" {
  value = data.authing_global_mfa_settings.current.enabled_factors
}
```

## Attribute Reference

- `enabled_factors` (List of String) — Factors enabled in the API response order; possible values are `OTP`, `SMS`, `EMAIL`, and `FACE`.
