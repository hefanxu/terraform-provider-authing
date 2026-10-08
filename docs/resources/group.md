---
page_title: "authing_group Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing user group.
---

# authing_group (Resource)

Manages an Authing user group.

## Example Usage

```terraform
resource "authing_group" "devs" {
  code        = "developers"
  name        = "Developers Team"
  type        = "static"
  description = "Group for software developers and engineers"
}
```

## Schema

### Required

- `code` (String, Forces New) Unique code / identifier for the group.
- `name` (String) Display name of the group.
- `type` (String, Forces New) Group type. Specify a type supported by your Authing environment; the OpenAPI contract gives `static` as an example but does not declare an enum or default. Changing it replaces the group.

### Optional

- `description` (String) Description of the group. When omitted, the provider sends an empty string on create/update to satisfy the API's required field.

### Read-Only

- `id` (String) The group code.

## Import

Groups can be imported using the group `code`:

```shell
terraform import authing_group.devs developers
```
