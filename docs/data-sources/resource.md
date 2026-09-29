---
page_title: "authing_resource Data Source - terraform-provider-authing"
subcategory: ""
description: |-
  Fetches details of an Authing permission resource.
---

# authing_resource (Data Source)

Fetches details of an Authing resource by code and namespace.

## Example Usage

```terraform
data "authing_resource" "api" {
  code      = "api_user_management"
  namespace = "core_system"
}

output "resource_type" {
  value = data.authing_resource.api.type
}
```

## Schema

### Required

- `code` (String) Resource unique code.

### Optional

- `namespace` (String) Permission namespace code.

### Read-Only

- `description` (String) Resource description.
- `id` (String) Resource code.
- `type` (String) Resource type (`API`, `DATA`, `MENU`, `BUTTON`, `UI`).
