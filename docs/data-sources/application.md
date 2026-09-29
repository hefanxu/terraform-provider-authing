---
page_title: "authing_application Data Source - terraform-provider-authing"
subcategory: ""
description: |-
  Fetches details of an Authing application by app_id.
---

# authing_application (Data Source)

Fetches details of an Authing Application by its App ID.

## Example Usage

```terraform
data "authing_application" "portal" {
  app_id = "6343b98b7cfxxx9366e9b7c"
}

output "portal_name" {
  value = data.authing_application.portal.app_name
}
```

## Schema

### Required

- `app_id` (String) Authing Application ID.

### Read-Only

- `app_name` (String) Application name.
- `description` (String) Application description.
- `id` (String) Application ID.
- `init_login_url` (String) Initial login URL.
