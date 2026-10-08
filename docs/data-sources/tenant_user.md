# authing_tenant_user

Looks up one existing tenant member using `GET /api/v3/get-tenant-user`. This data source is read-only and always requires an explicit `tenant_id`; the provider's default tenant does not substitute for it. Specify exactly one of `link_user_id` or `member_id` (nonempty). The returned tenant ID and requested identifier must match exactly. A missing, malformed, or mismatched response fails the read rather than returning another tenant's member.

Only `member_id`, `link_user_id`, `is_tenant_admin`, `username`, `name`, `nickname`, and `photo` are returned. Optional display fields are null when absent. **Password and salt are never mapped to Terraform state, diagnostics, or logs.** Other profile fields are intentionally omitted. Terraform state may still contain the listed identity and display values; protect state access accordingly.

## Example

```hcl
data "authing_tenant_user" "member" {
  tenant_id    = "tenant-id"
  link_user_id = "userpool-user-id"
}

output "membership_id" {
  value = data.authing_tenant_user.member.member_id
}
```

To look up by membership ID instead, set `member_id` and omit `link_user_id`.

## Arguments

- `tenant_id` (required): Explicit tenant ID.
- `link_user_id` (optional/computed): Linked userpool user ID; mutually exclusive with `member_id` as an input.
- `member_id` (optional/computed): Tenant membership ID; mutually exclusive with `link_user_id` as an input.

## Computed attributes

- `member_id`, `link_user_id`: Both identities from the verified response.
- `is_tenant_admin`: Tenant administrator status.
- `username`, `name`, `nickname`, `photo`: Nonsecret display fields, null if absent.
