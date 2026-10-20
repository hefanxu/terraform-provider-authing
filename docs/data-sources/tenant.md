# authing_tenant (data source)

Looks up a tenant by its stable Authing tenant ID. A missing tenant returns an error.

```hcl
data "authing_tenant" "example" {
  tenant_id = "tenant-id"
}

output "tenant_code" {
  value = data.authing_tenant.example.code
}
```

## Arguments

- `tenant_id` (required): Tenant ID.

## Attributes

- `id`: The tenant ID.
- `name`: Tenant name.
- `app_ids`: Associated application ID set.
- `description`: Tenant description.
- `source_app_id`: Source application ID (empty when absent).
- `code`: Authing-generated tenant code.
