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

## Destructive sandbox acceptance

`TestDestructiveLiveResourceTrace` is opt-in, skipped in normal tests, and requires the same disposable sandbox, owner approval, `-authing-destructive-sandbox`, exact `AUTHING_ACCEPTANCE_CONFIRM=DESTRUCTIVE_SANDBOX`, and sandbox AK/SK environment variables as the role tracer. It creates its own random marked permission namespace and `BUTTON` resource with `actions = []`, checks apply → plan 0 → remote **description** drift → plan 2 → reconcile → plan 0, destroys the resource before its namespace, and independently GETs absence of each exact identity. Resource action drift is **not** tested: `authing_resource` Read does not refresh `actions`. Cleanup refuses a foreign identity, additional roles/resources/data resources, incomplete inventories, and any global data policy (the list endpoint cannot scope policies to one namespace); manual investigation is required if it refuses. The offline Terraform CLI/`httptest` tracer passes without Authing credentials but does not establish live-service compatibility. The live tracer has not been run.

<a id="nestedatt--actions"></a>
### Nested Schema for `actions`

Required:

- `name` (String) Action name (e.g. `read`, `write`, `execute`).

Optional:

- `description` (String) Action description.
