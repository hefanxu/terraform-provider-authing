# authing_tenant_organization

Manages one organization in a specific Authing tenant, separate from the unscoped `authing_organization`. Use only one resource to manage a given tenant/code pair.

```hcl
resource "authing_tenant_organization" "example" {
  tenant_id         = "tenant-id"
  organization_code = "engineering"
  organization_name = "Engineering"
  description       = "Engineering organization"
}
```

`tenant_id` and `organization_code` are required and immutable; changing either replaces the resource. `organization_name` is required and mutable. `description` is optional and read back from Authing. The `id` is a versioned base64url encoding of a JSON array `[tenant_id, organization_code]`, not an Authing-generated root department ID. The create endpoint requires `metadata`; this provider sends an empty object and does not manage metadata because it is not available for round-trip in the organization detail response. This behavior follows the published Management API schema but has not been verified against a live tenant.

The resource preflights Create to avoid adopting an existing organization. All reads require the returned tenant ID and organization code to exactly match state; a missing/mismatched tenant is an error, not evidence of absence. Delete preflights the exact identity and **refuses to delete** when `hasChildren` is true or absent: Authing's delete endpoint removes the entire organization tree. Remove child departments before destroying the organization. A concurrent child creation after preflight is not preventable by this resource; review the tenant tree before destruction. HTTP/business 404 removes state on Read and makes Delete idempotent; other errors preserve state.

Import using the resource's composite ID. For example, for tenant `tenant-id` and code `engineering`, generate the ID with:

```sh
python3 -c 'import base64,json; print("v1." + base64.urlsafe_b64encode(json.dumps(["tenant-id","engineering"], separators=(",", ":")).encode()).decode().rstrip("="))'
terraform import authing_tenant_organization.example "$(python3 -c 'import base64,json; print("v1." + base64.urlsafe_b64encode(json.dumps(["tenant-id","engineering"], separators=(",", ":")).encode()).decode().rstrip("="))')"
```

Import performs no mutation; the next refresh verifies the exact tenant and code and populates name/description.
