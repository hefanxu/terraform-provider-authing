---
page_title: "authing_role_assignment Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Assigns an Authing Role to a user or department target.
---

# authing_role_assignment (Resource)

Assigns an Authing Role to a target subject (User or Department).

## Example Usage

### Assign to User

```terraform
resource "authing_role_assignment" "user_assign" {
  role_code   = "admin"
  namespace   = "cloud_platform"
  target_type = "USER"
  target_id   = authing_user.dev.id
}
```

### Assign to Department

```terraform
resource "authing_role_assignment" "dept_assign" {
  role_code   = "operator"
  namespace   = "cloud_platform"
  target_type = "DEPARTMENT"
  target_id   = authing_department.ops.id
}
```

## Schema

### Required

- `role_code` (String, Forces New) Role code.
- `target_id` (String, Forces New) User ID or Department ID.
- `target_type` (String, Forces New) Target type: `USER` or `DEPARTMENT`.

### Optional

- `namespace` (String, Forces New) Permission namespace code.

### Read-Only

- `id` (String) Combined identifier (`role_code:target_type:target_id`).
