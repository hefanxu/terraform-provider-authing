---
page_title: "authing_public_account Resource - terraform-provider-authing"
description: |-
  Manages a basic Authing public account.
---

# authing_public_account (Resource)

Creates, reads, updates, and deletes a public account by its stable Authing `userId`. **Do not manage the same ID with `authing_user`**: both resources own the account and can overwrite or delete it. Import an existing public account instead of creating a second one.

```terraform
resource "authing_public_account" "shared" {
  username = "shared-lobby"
  name     = "Lobby account"
  nickname = "Lobby"
  email    = "shared-lobby@example.com"
}
```

At least `username` or `email` must be nonempty at creation. The API also accepts phone-only accounts, but this basic resource does not expose phone. `username`, `name`, `nickname`, and `email` are optional/computed and refreshed from the public-account GET endpoint. Empty remote strings appear as null. This resource intentionally does not handle passwords, OTP, credentials, departments, bulk operations, kick, resignation, or user-to-public-account conversion. Use separate account/relationship management for those operations. Deletion permanently removes the public account, not just a link; the provider checks the exact ID before deletion and confirms absence afterward.

## Attributes

- `id` (String, computed): Authing public-account user ID.
- `username` (String, optional/computed): Unique username.
- `name` (String, optional/computed): Name.
- `nickname` (String, optional/computed): Nickname.
- `email` (String, optional/computed): Email address.

## Import

```shell
terraform import authing_public_account.shared public-account-user-id
```

Import and lookup use the Authing user ID (`userIdType=user_id`), not username or email. Explicit 404 removes a managed resource from state; failed or malformed lookups keep state and report an error. A successful write must return the same user ID on update and be confirmed by a follow-up GET before state is accepted. No live Authing tenant verification is included in local tests.
