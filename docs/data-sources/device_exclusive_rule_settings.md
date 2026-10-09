---
page_title: "authing_device_exclusive_rule_settings Data Source - Authing"
subcategory: "Security"
description: |-
  Read nonsecret device-exclusive rule settings.
---

# authing_device_exclusive_rule_settings (Data Source)

Reads `/api/v3/get-device-exclusive-rule-settings` without arguments or writes. Only the rule and its optional numeric limits are stored. The rule can be `disable`, `condition:device`, or `condition:ip`. A limit is null when its corresponding rule object is absent; the active rule requires its limit. Malformed returned settings and API errors fail the lookup. No raw response is stored.

The endpoint has no `tenantId` query parameter or returned tenant identity. A configured provider `tenant_id` header does **not** verify tenant scope. This data source does not manage rules; update/delete semantics have not been established.

## Example Usage

```terraform
data "authing_device_exclusive_rule_settings" "current" {}

output "device_exclusive_rule" {
  value = data.authing_device_exclusive_rule_settings.current.rule
}
```

## Attribute Reference

- `rule` (String) — Current rule.
- `max_online_devices` (Number) — Device limit, or null if `deviceRule` is absent.
- `max_online_ips` (Number) — IP limit, or null if `ipRule` is absent.
