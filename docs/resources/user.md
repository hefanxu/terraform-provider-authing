---
page_title: "authing_user Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing user account.
---

# authing_user (Resource)

Manages an Authing user account.

## Example Usage

```terraform
resource "authing_user" "example" {
  username       = "dev_zhang"
  email          = "zhang@example.com"
  phone          = "13800138000"
  nickname       = "San Zhang"
  password       = "SecureP@ssw0rd!"
  status         = "Activated"
  gender         = "M"
  email_verified = true
  phone_verified = true
}
```

## Schema

### Optional

- `username` (String) The unique username of the user.
- `email` (String) The email address of the user.
- `phone` (String) The phone number of the user.
- `nickname` (String) Display name or nickname of the user.
- `password` (String, Sensitive) Initial password for the user.
- `external_id` (String) User ID in the external system.
- `status` (String) Account status: `Activated`, `Suspended`, `Deactivated`.
- `gender` (String) Gender: `M` (Male), `F` (Female), `U` (Unknown).
- `email_verified` (Boolean) Whether the email is verified.
- `phone_verified` (Boolean) Whether the phone is verified.

### Read-Only

- `id` (String) The unique identifier (User ID) in Authing.

## Import

Users can be imported using the Authing `user_id`:

```shell
terraform import authing_user.example 6319a1504f3xxxxf214dd5b7
```
