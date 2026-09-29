---
page_title: "authing_department Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing Department.
---

# authing_department (Resource)

Manages an Authing Department inside an organization.

## Example Usage

```terraform
resource "authing_organization" "corp" {
  organization_code = "my_company"
  organization_name = "My Company Inc."
}

resource "authing_department" "rd" {
  organization_code    = authing_organization.corp.organization_code
  name                 = "Research & Development"
  parent_department_id = "root"
  description          = "Core engineering department"
}

resource "authing_department" "backend_team" {
  organization_code    = authing_organization.corp.organization_code
  name                 = "Backend Team"
  parent_department_id = authing_department.rd.id
}
```

## Schema

### Required

- `name` (String) Name of the department.
- `organization_code` (String, Forces New) Organization code this department belongs to.
- `parent_department_id` (String) Parent department ID (use `root` for root level).

### Optional

- `department_id` (String, Forces New) Custom department ID.
- `description` (String) Department description.

### Read-Only

- `id` (String) The department ID in Authing.

## Import

Departments can be imported using `id`:

```shell
terraform import authing_department.rd 6343bafc019xxxx889206c4c
```
