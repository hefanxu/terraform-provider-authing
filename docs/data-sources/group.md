---
page_title: "authing_group Data Source - terraform-provider-authing"
subcategory: ""
description: |-
  Fetches details of an Authing user group.
---

# authing_group (Data Source)

Fetches details of an Authing user group by its unique code.

## Example Usage

```terraform
data "authing_group" "admins" {
  code = "administrators"
}

output "group_name" {
  value = data.authing_group.admins.name
}
```

## Schema

### Required

- `code` (String) The group code.

### Read-Only

- `description` (String) Group description.
- `id` (String) The group code.
- `name` (String) Group name.
