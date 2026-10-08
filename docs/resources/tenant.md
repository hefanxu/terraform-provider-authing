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

Import then configure the required `name` and full `app_ids` set to match the tenant before applying. This resource does not manage `logo` (write accepts an array while read returns a string), create-only enterprise domains or expiry/billing limits, or other unrepresented tenant settings. It does not send those fields on update. No live tenant integration validation was performed.
