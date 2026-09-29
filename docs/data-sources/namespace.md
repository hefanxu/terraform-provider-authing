---
page_title: "authing_namespace Data Source - terraform-provider-authing"
subcategory: ""
description: |-
  Fetches details of an Authing permission namespace.
---

# authing_namespace (Data Source)

Fetches details of an Authing permission namespace by its code.

## Example Usage

```terraform
data "authing_namespace" "core" {
  code = "core_system"
}

output "namespace_name" {
  value = data.authing_namespace.core.name
}
```

## Schema

### Required

- `code` (String) Unique code of the permission namespace.

### Read-Only

- `description` (String) Description of the permission namespace.
- `id` (String) Permission namespace code.
- `name` (String) Name of the permission namespace.
