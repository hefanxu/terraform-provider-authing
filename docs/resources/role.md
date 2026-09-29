---
page_title: "authing_role Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing Role.
---

# authing_role (Resource)

Manages an Authing Role within a permission namespace.

## Example Usage

```terraform
resource "authing_namespace" "platform" {
  code = "cloud_platform"
  name = "Cloud Platform"
}

resource "authing_role" "admin" {
  code        = "admin"
  name        = "Administrator"
  namespace   = authing_namespace.platform.code
  description = "Full administrative access"
}
```

## Schema

### Required

- `code` (String, Forces New) Unique code for the role within the namespace.

### Optional

- `description` (String) Description of the role.
- `name` (String) Name of the role.
- `namespace` (String, Forces New) Permission namespace code (defaults to `default`).

### Read-Only

- `id` (String) The role code.

## Import

Roles can be imported using `code`:

```shell
terraform import authing_role.admin admin
```
