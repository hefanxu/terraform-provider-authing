---
page_title: "authing_resource Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing Resource (API, DATA, UI, BUTTON, MENU).
---

# authing_resource (Resource)

Manages an Authing Resource (API, DATA, UI, BUTTON, MENU) and its allowed actions.

## Example Usage

```terraform
resource "authing_resource" "user_api" {
  code        = "api_user_management"
  type        = "API"
  namespace   = "cloud_platform"
  description = "User management API endpoints"

  actions = [
    {
      name        = "create"
      description = "Create user permission"
    },
    {
      name        = "read"
      description = "Read user permission"
    },
    {
      name        = "delete"
      description = "Delete user permission"
    }
  ]
}
```

## Schema

### Required

- `code` (String) Resource unique code.
- `type` (String) Resource type: `API`, `DATA`, `MENU`, `BUTTON`, `UI`.

### Optional

- `actions` (Attributes List) List of supported actions for this resource. (see [below for nested schema](#nestedatt--actions))
- `description` (String) Resource description.
- `namespace` (String) Permission namespace code.

### Read-Only

- `id` (String) The resource code.

<a id="nestedatt--actions"></a>
### Nested Schema for `actions`

Required:

- `name` (String) Action name (e.g. `read`, `write`, `execute`).

Optional:

- `description` (String) Action description.
