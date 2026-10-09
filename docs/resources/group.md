---
page_title: "authing_group Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing user group.
---

# authing_group (Resource)

Manages an Authing user group.

## Example Usage

```terraform
resource "authing_group" "devs" {
  code        = "developers"
  name        = "Developers Team"
  type        = "static"
  description = "Group for software developers and engineers"
}
```

## Schema

### Required

- `code` (String, Forces New) Unique code / provider identity for the group. Changing it replaces the resource; this provider deliberately does not use the API’s `newCode` rename operation.
- `name` (String) Display name of the group.
- `type` (String, Forces New) Group type. Specify a type supported by your Authing environment; the OpenAPI contract gives `static` as an example but does not declare an enum or default. Changing it replaces the group.

### Optional

- `description` (String) Description of the group. Optional + computed. Omitted/null on create is sent as an empty string. After refresh/import, omitted/null adopts the remote value; omission is not a clearing operation. Configure `description = ""` to clear it explicitly.

### Read-Only

- `id` (String) The group code.

## Import

Groups can be imported using the group `code`:

```shell
terraform import authing_group.devs developers
```

## Supported-field acceptance and scope

Contract refreshed from the official [Management OpenAPI](https://api.authing.cn/openapi-json)
and [Tenant Management OpenAPI](https://api.authing.cn/tenant-management-openapi-json).
The resource is user-pool scoped: neither its schema nor these group CRUD operations
expose a tenant selector. The API has no documented group-type enum or default.

| Schema field | API contract / Terraform semantics | Acceptance oracle |
|---|---|---|
| `id` | Computed provider identity equals `code`, **not** the API's internal `GroupDto.id` | Creating-state pin, exact-code GET, same identity after fresh import, GET 404 after delete |
| `code` | Required; immutable within this provider; replacement on change. API `newCode` rename intentionally not modeled | Real CLI plan JSON requires `delete,create`; no replacement apply against live Authing |
| `type` | Required; create + GET; no type field in `UpdateGroupReqDto`; replacement on change | Exact create/import/refresh GET and real CLI replacement-plan JSON; other types and replacement apply are not live-certified |
| `name` | Required, mutable; create/update/GET | Explicit HCL update + exact GET + plan 0; direct remote drift + GET + plan 2 + repair + plan 0 |
| `description` | Optional + computed, mutable; API requires it in create/update | Explicit HCL update, explicit empty-string clearing + exact GET, remote drift and repair; omitted create offline-tested |

The expanded lifecycle is offline verified; its live result is recorded in
[the evidence ledger](../acceptance-evidence.md). The earlier name-only live run
is not proof of complete supported-field acceptance.

The API also exposes `customData` on create/update and `withCustomData` on GET,
but the published group GET DTO does not describe a corresponding custom-data
response field or clearing semantics. It is **not** a supported Terraform
attribute. `metadataSource`, internal API `id`, embedded `members`, public-account
relations and authorized resources are also not managed by this resource.
`authing_group_member` is a separate relation and its live selector remains
suspended because the prior incident has no state-pinned cleanup authority.
Nothing in this lifecycle adds users, public accounts, roles or invitations.

Create and update accept success only after exact-code GET confirms all supported
fields; malformed/mismatched/403/422/5xx reads preserve state, rather than claiming
absence. Delete performs exact-code preflight and requires explicit GET 404 after
the successful delete response. The disposable acceptance harness additionally
requires the creating Terraform state's ID, exact generated identity, expected
name/description/type and an explicit zero-count, empty membership inventory
(`limit=50`) before deletion. A failed create without a pinned state ID is never
cleaned up by guessing from the generated code/name. Failure output preserves the
first phase, cleanup outcome and non-reversible state-ID fingerprint when pinned.
