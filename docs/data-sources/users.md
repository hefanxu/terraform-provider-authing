---
page_title: "authing_users Data Source - terraform-provider-authing"
subcategory: ""
description: |-
  Lists and searches Authing users.
---

# authing_users (Data Source)

Lists and searches Authing users by search keywords.

## Example Usage

```terraform
data "authing_users" "engineers" {
  keywords = "engineer"
}

output "matched_user_ids" {
  value = [for u in data.authing_users.engineers.users : u.user_id]
}
```

## Schema

### Optional

- `keywords` (String) Keywords for searching users.

### Read-Only

- `id` (String) Identifier for the data source query.
- `users` (Attributes List) Matched users list. (see [below for nested schema](#nestedatt--users))

<a id="nestedatt--users"></a>
### Nested Schema for `users`

Read-Only:

- `email` (String) Email address.
- `email_verified` (Boolean) Email verification status.
- `external_id` (String) External ID.
- `gender` (String) Gender.
- `id` (String) User ID.
- `nickname` (String) Nickname.
- `phone` (String) Phone number.
- `phone_verified` (Boolean) Phone verification status.
- `status` (String) User account status.
- `user_id` (String) User ID in Authing.
- `username` (String) Username.
