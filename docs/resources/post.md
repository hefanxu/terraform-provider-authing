---
page_title: "authing_post Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing Post / Job Position.
---

# authing_post (Resource)

Manages an Authing Post (岗位/职位).

## Example Usage

```terraform
resource "authing_post" "architect" {
  code        = "solutions_architect"
  name        = "Solutions Architect"
  description = "Principal solutions architect position"
}
```

## Schema

### Required

- `code` (String) Unique code for the post. Changing it replaces the post.
- `name` (String) Name of the post.

### Optional

- `description` (String) Description of the post.

### Read-Only

- `id` (String) The post code.
