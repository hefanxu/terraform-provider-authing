---
page_title: "authing_organization Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing Organization.
---

# authing_organization (Resource)

Manages an Authing Organization tree root.

**Destructive deletion safeguard:** Authing's delete endpoint removes the entire organization tree. This resource checks the exact organization immediately before deletion and refuses to delete when it has child departments or `hasChildren` is unavailable. Remove child departments first. A failed lookup or unconfirmed deletion also fails the operation.

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

- `organization_code` (String, Forces New) Unique code for the organization. Required by the create API.
- `organization_name` (String) Name of the organization.

### Optional

- `description` (String) Description of the organization.

Creation sends an empty `metadata` object, which the API requires. Custom organization metadata is not managed by this resource.

### Read-Only

- `id` (String) The organization code.

## Import

Organizations can be imported using `organization_code`:

```shell
terraform import authing_organization.corp my_company
```
