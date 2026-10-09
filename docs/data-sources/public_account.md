# authing_public_account (Data Source)

Looks up one public account by its Authing user ID. A missing account or an API failure is an error.

```terraform
data "authing_public_account" "shared" {
  user_id = "public-account-user-id"
}

output "shared_account_name" {
  value = data.authing_public_account.shared.name
}
```

`user_id` is required. `id`, `username`, `name`, `nickname`, and `email` are computed. The lookup uses `userIdType=user_id` and rejects a response for another ID.
