---
page_title: "authing_user Data Source - terraform-provider-authing"
subcategory: ""
description: |-
  Fetches details of an Authing user by user_id.
---

# authing_user (Data Source)

Fetches details of a single Authing user by user ID.

## Example Usage

```terraform
data "authing_user" "current" {
  user_id = "6319a1504f3xxxxf214dd5b7"
}

output "user_email" {
  value = data.authing_user.current.email
}
```

## Schema

### Required

- `user_id` (String) Authing User ID.

### Read-Only

- `email` (String) Email address.
- `email_verified` (Boolean) Email verification status.
- `external_id` (String) External ID.
- `gender` (String) Gender.
- `id` (String) Internal identifier.
- `nickname` (String) Nickname.
- `phone` (String) Phone number.
- `phone_verified` (Boolean) Phone verification status.
- `status` (String) User account status.
- `username` (String) Username.
