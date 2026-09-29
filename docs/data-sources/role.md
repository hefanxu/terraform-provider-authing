---
page_title: "authing_role Data Source - terraform-provider-authing"
subcategory: ""
description: |-
  Fetches details of an Authing role.
---

# authing_role (Data Source)

Fetches details of an Authing role by code and namespace.

## Example Usage

```terraform
data "authing_role" "admin" {
  code      = "admin"
  namespace = "core_system"
}

output "role_desc" {
  value = data.authing_role.admin.description
}
```

## Schema

### Required

- `code` (String) Role code.

### Optional

- `namespace` (String) Permission namespace code.

### Read-Only

- `description` (String) Role description.
- `id` (String) Role code.
