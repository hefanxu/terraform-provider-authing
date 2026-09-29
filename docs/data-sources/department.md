---
page_title: "authing_department Data Source - terraform-provider-authing"
subcategory: ""
description: |-
  Fetches details of an Authing department.
---

# authing_department (Data Source)

Fetches details of an Authing department by organization code and department ID.

## Example Usage

```terraform
data "authing_department" "engineering" {
  organization_code = "my_company"
  department_id     = "root"
}

output "dept_name" {
  value = data.authing_department.engineering.name
}
```

## Schema

### Required

- `department_id` (String) Department ID.
- `organization_code` (String) Organization code.

### Read-Only

- `description` (String) Department description.
- `id` (String) Department ID.
- `name` (String) Department name.
- `parent_department_id` (String) Parent department ID.
