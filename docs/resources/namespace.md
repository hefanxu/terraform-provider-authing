---
page_title: "authing_namespace Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing Permission Namespace (权限空间).
---

# authing_namespace (Resource)

Manages an Authing Permission Namespace (权限空间/权限分组).

## Example Usage

```terraform
resource "authing_namespace" "platform" {
  code        = "cloud_platform"
  name        = "Cloud Platform Permissions"
  description = "Permissions and roles for the cloud management platform"
}
```

## Schema

### Required

- `code` (String, Forces New) Unique code for the permission namespace.
- `name` (String) Display name of the permission namespace.

### Optional

- `description` (String) Description of the permission namespace.

### Read-Only

- `id` (String) The permission namespace code.

## Import

Permission namespaces can be imported using `code`:

```shell
terraform import authing_namespace.platform cloud_platform
```
