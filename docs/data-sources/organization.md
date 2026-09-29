---
page_title: "authing_organization Data Source - terraform-provider-authing"
subcategory: ""
description: |-
  Fetches details of an Authing organization by organization_code.
---

# authing_organization (Data Source)

Fetches details of an Authing organization by its code.

## Example Usage

```terraform
data "authing_organization" "company" {
  organization_code = "my_company"
}

output "org_name" {
  value = data.authing_organization.company.organization_name
}
```

## Schema

### Required

- `organization_code` (String) Organization code.

### Read-Only

- `description` (String) Organization description.
- `id` (String) Organization code.
- `organization_name` (String) Organization display name.
