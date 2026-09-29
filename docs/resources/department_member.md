---
page_title: "authing_department_member Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Adds a user to an Authing department.
---

# authing_department_member (Resource)

Adds a user to an Authing department.

## Example Usage

```terraform
resource "authing_department_member" "membership" {
  organization_code = "my_company"
  department_id     = authing_department.rd.id
  user_id           = authing_user.employee.id
}
```

## Schema

### Required

- `organization_code` (String) Organization code.
- `department_id` (String) Department ID.
- `user_id` (String) User ID.

### Read-Only

- `id` (String) Combined identifier (`organization_code:department_id:user_id`).
