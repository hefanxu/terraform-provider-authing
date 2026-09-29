---
page_title: "authing_organization Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing Organization.
---

# authing_organization (Resource)

Manages an Authing Organization tree root.

## Example Usage

```terraform
resource "authing_organization" "corp" {
  organization_code = "my_company"
  organization_name = "My Company Inc."
  description       = "Primary company organization tree"
}
```

## Schema

### Required

- `organization_name` (String) Name of the organization.

### Optional

- `organization_code` (String, Forces New) Unique code for the organization.
- `description` (String) Description of the organization.

### Read-Only

- `id` (String) The organization code.

## Import

Organizations can be imported using `organization_code`:

```shell
terraform import authing_organization.corp my_company
```
