---
page_title: "authing_data_policy Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing Data Policy (数据策略).
---

# authing_data_policy (Resource)

Manages an Authing Data Policy (数据策略) with statement rules.

## Example Usage

```terraform
resource "authing_data_policy" "rd_policy" {
  policy_name = "RD Team Policy"
  description = "Access policy for engineering resources"

  statement_list = [
    {
      effect = "ALLOW"
      permissions = [
        "cloud_platform/api_user_management/read",
        "cloud_platform/api_user_management/create"
      ]
    },
    {
      effect = "DENY"
      permissions = [
        "cloud_platform/api_user_management/delete"
      ]
    }
  ]
}
```

## Schema

### Required

- `policy_name` (String) Name of the data policy.
- `statement_list` (Attributes List) List of statements defining ALLOW/DENY rules for data resources. (see [below for nested schema](#nestedatt--statement_list))

### Optional

- `description` (String) Description of the data policy.

### Read-Only

- `id` (String) The data policy ID.

<a id="nestedatt--statement_list"></a>
### Nested Schema for `statement_list`

Required:

- `effect` (String) Statement effect: `ALLOW` or `DENY`.
- `permissions` (List of String) List of resource action paths (e.g. `namespace/resource/action`).
