# authing_tenant

Manages an Authing tenant. **Destroy deletes the tenant.** Import by its stable `tenantId`.

```hcl
resource "authing_tenant" "example" {
  name          = "Example tenant"
  app_ids       = ["application-id"]
  description   = "Managed by Terraform"
  source_app_id = "application-id"
}
```

## Arguments

- `name` (required): Tenant name.
- `app_ids` (required, set of strings): **Exclusive ownership** of the complete associated application ID set. Terraform reconciles the entire set and may remove applications associated outside Terraform. An empty set sends an empty array.
- `description` (optional): Tenant description.
- `source_app_id` (optional): Source application ID. Omit for tenants created from the Authing console.

## Attributes

- `id`: Stable tenant ID (`tenantId`).
- `code`: Authing-generated tenant code.

## Import

```sh
terraform import authing_tenant.example tenant-id
```

Import then configure the required `name` and full `app_ids` set to match the tenant before applying. This resource does not manage `logo` (write accepts an array while read returns a string), create-only enterprise domains or expiry/billing limits, or other unrepresented tenant settings. It does not send those fields on update.

## Live acceptance blocker

Tenant acceptance is **incomplete**, not a live CRUD pass. The [original sandbox run](https://github.com/hefanxu/terraform-provider-authing/actions/runs/37875933577) created a tenant and pinned its ID in ephemeral Terraform state, then failed `verify-created`; cleanup remained incomplete. The [separately gated read-only diagnosis](https://github.com/hefanxu/terraform-provider-authing/actions/runs/37879630451), at commit `b7e28eb46f52ca505ef43557ae53a86ce346ab22`, found one exact-name candidate, verified its exact-ID GET, and independently read its app/user/admin/organization inventories. Apps, members and admins were explicitly empty; the tenant-scoped organization inventory contained **one organization**. This is a nonempty dependency, not an absent or malformed collection. Its provenance is not established; do not assume it is safe to cascade-delete a default organization.

The original run has no retained artifacts, and its creating-state ID is not recoverable from the controlled output. A unique name/list candidate and successful exact GET do **not** restore state-pinned deletion authority. Tenant, tenant-membership and tenant-organization live write selectors remain suspended. No new tenant or cleanup write was attempted during diagnosis. Explicit live HCL update, fresh-state import/converged plan, drift/repair and guarded deletion/404 are still unverified.

The recovery workflow requires `confirm=READ_ONLY_SANDBOX`, `test_case=group` (unused destructive-selector placeholder), `read_only_audit=tenant`, exact incident `test_object_code=hermesacc-97f07719b2e15f0c`, and empty audit window inputs. Its result always reports `authority=none`; it never deletes. Future failed tenant traces preserve closed stage/count diagnostics, the first failing phase, cleanup outcome and a non-reversible **creating-state** ID fingerprint through the outer output allowlist. That improvement cannot retroactively supply evidence for this incident. Resuming cleanup requires independently verifiable creating-state/response identity evidence plus authorization for that exact target and any nonempty organization dependency; do not infer approval from a name match or issue another create to work around the blocker.
