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

Import and lookup use the Authing user ID (`userIdType=user_id`), not username or email. Empty IDs, whitespace, scope prefixes and path separators are rejected without normalizing the supplied token. Explicit business 404 removes a managed resource from state; 403/422/5xx, transport failures, malformed envelopes and mismatched identities keep state and report an error. A successful write must return the same user ID on update and be confirmed by a follow-up exact-ID GET, including every known configured field, before state is accepted. A post-create readback failure reports the returned ID for operator import, but it is not a creating-state pin and does not authorize name-based cleanup.

## Supported configuration scope

The refreshed [Management OpenAPI](https://api.authing.cn/openapi-json) advertises more account fields than this resource exposes. [The contract inventory](../public-account-contract.json) records the fetched specification SHA-256, complete create/update/read field names and the explicit supported subset. **Complete acceptance here means the existing schema below, not all OpenAPI account fields.**

| Schema field | Configuration and lifecycle |
|---|---|
| `id` | Computed canonical `userId`; stable across updates and import. Not configurable. |
| `username` | Optional/computed; nonempty configured values update in place and are read back. |
| `name` | Optional/computed; nonempty configured values update in place and are read back. |
| `nickname` | Optional/computed; nonempty configured values update in place and are read back. |
| `email` | Optional/computed; nonempty configured values update in place and are read back. Use lowercase addresses: the API describes email as case-insensitive; the provider requires exact configured readback. |

No exposed configurable field is immutable or replacement-only. Omitted fields are computed, not a request to clear remote data. Explicit empty strings are rejected before writes because GET maps remote empty strings to null; clearing a field to empty is **not supported**. Removing a field from HCL relinquishes its configuration without clearing the remote value. Imported state can read all four fields even when HCL omits them.

The resource has no tenant selector and does not assert tenant-scoped behavior. Phone-only creation, other profile fields, status/security settings, custom data, metadata, account relationships and every password/OTP/rotation/kick/resignation/conversion action remain outside this schema. No mutable supported field is treated as write-only or silently retained instead of read back.

## Acceptance contract

The manual `public_account` tracer requires a fresh creating workspace and generated `hermesacc-` identity. It checks exact generated username absence before writes; creates with all four fields; pins the creating Terraform state ID; verifies exact-ID GET; explicitly changes all four HCL values and verifies plan 2/apply/GET/plan 0; restores original values; imports the pinned ID into a fresh independent state and verifies plan 0; injects all-four-field remote drift and confirms GET before plan 2; reconciles and verifies every field; then destroys and requires exact-ID business 404 plus deferred cleanup confirmation. Only the creating workspace supplies cleanup authority; imported or discovery-derived IDs never replace it. The outer runner validates the ordered stage evidence and emits only generated codes, closed phase/cleanup labels and a SHA-256 of the creating-state ID. Failure injection through real Terraform CLI and the outer sanitizer covers unknown create identity, reconciliation failure and incomplete cleanup. See [live evidence](../acceptance-evidence.md) for actual tested commits and run results; offline tests alone are not a live pass.
