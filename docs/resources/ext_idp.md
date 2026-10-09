---
page_title: "authing_ext_idp Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages the identity and name of an Authing External Identity Provider.
---

# authing_ext_idp (Resource)

Manages an external identity-provider **container**, not its connections, login
configuration, application enablement, credentials, or keys. Its only mutable
attribute is `name`. Creating this resource alone does not configure an external
login connection.

## Example Usage

```terraform
resource "authing_ext_idp" "example" {
  name = "Example OIDC IdP"
  type = "oidc"
  # Omit tenant_id for the user-pool/default scope.
}
```

## Schema and lifecycle

| Attribute | Terraform semantics | API support / verification |
|---|---|---|
| `name` | Required, mutable | Create and Update; exact-ID GET verifies the configured name after each write. Refresh detects out-of-band name drift. |
| `type` | Required, replacement only | Create supports the service's connection-type string; changing it plans delete/create, never an in-place update. Only `oidc` has live lifecycle evidence. |
| `tenant_id` | Optional + Computed, replacement only | Omission creates in the default scope and resolves to `""`. An existing/imported scope is retained when omitted; omission is **not** a tenant-to-default move. Explicit scope changes require replacement. Tenant routing and identity checks have offline tests only. |
| `id`, `ext_idp_id` | Computed | Both contain the same canonical Authing IdP ID. Corrupt, unknown or mismatched state identity fails closed. |

The [Management V3 OpenAPI](https://api.authing.cn/openapi-json) exposes
`create-ext-idp` (`name`, `type`, optional `tenantId`), `get-ext-idp`,
`update-ext-idp` (`id`, `name`, optional `tenantId`), and `delete-ext-idp`
(`id`, optional `tenantId`). GET also offers application/category filters which
this resource does not expose. Its GET `type` category filter (`social` /
`enterprise`) is **not** the resource's connection `type`.

Create pins the returned ID and performs exact-ID readback of ID, name, type and
scope. Failed post-create verification reports the returned ID for manual
recovery; do not retry create blindly. Read, update preflight and delete preflight
reject foreign ID, tenant or type, incomplete names/types, malformed responses,
and business/transport errors without adopting remote identity. Only explicit
not-found removes state. Update verifies the new name by GET before storing it.

### Destructive deletion and connections

Deletion first requires an exact-ID GET with a verified **empty connections
array**, then a successful deletion acknowledgment and exact-ID GET 404. A
nonempty, omitted, null, or structurally unknown `connections` value blocks
deletion; detach unmanaged connections separately before destroying this
container. This also applies to replacement. The published schema describes
`connections` as an object, whereas the tested empty runtime shape is `[]`;
other runtime shapes are deliberately not treated as empty.

A concurrent external attachment between preflight and delete cannot be made
atomic by the provider; coordinate ownership and avoid concurrent modification.

## Import

```shell
# Default user-pool scope:
terraform import authing_ext_idp.example idp-123

# Tenant scope (offline verified; not live certified):
terraform import authing_ext_idp.example tenant-123:idp-123
```

A tenant-scoped import must include `tenant_id:ext_idp_id`. Empty components and
multiple separators are rejected. The exact scoped GET hydrates name/type; a
scope mismatch is an error, not permission to adopt an IdP found in another
scope. Keep HCL `name`, `type` and any explicitly configured `tenant_id` aligned
with the imported object to obtain a no-change plan.

## Acceptance scope and known limitations

- Offline Terraform CLI tests cover default-scope create/read, explicit HCL name
  update, import into fresh state, drift/reconciliation and deletion; tenant
  composite import and `type`/`tenant_id` replacement plans are exercised against
  `httptest` without any tenant creation or replacement apply.
- Fail-closed tests cover corrupt state, foreign ID/scope/type, invalid responses,
  403/5xx, failed/missing readback, unknown/nonempty connections, and state-less
  create failure. Sandbox cleanup requires the ID pinned by the creating
  Terraform state; generated-name matches never authorize deletion.
- Live evidence and the exact tested commits are maintained in
  [acceptance-evidence](../acceptance-evidence.md). Live run [37878214556](https://github.com/hefanxu/terraform-provider-authing/actions/runs/37878214556)
  verified the default-scope OIDC container's create/read, explicit HCL name
  update, fresh-state import with no-change plan, name drift/reconciliation,
  and destroy followed by exact-ID GET 404. All expected phases and confirmed
  cleanup were required by the controlled runner before it reported success.
- No tenant-scoped live lifecycle, other connection types, connection CRUD,
  application/category-filter variants, external authentication, credential/key
  management, or replacement apply is claimed. Tenant-related live families are
  paused independently; this resource's default-scope test does not resume them.
