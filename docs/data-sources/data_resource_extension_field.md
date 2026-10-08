# authing_data_resource_extension_field

Read one extension-field definition on an Authing data resource (including TREE). This lookup is **read-only**. Although Authing exposes `create-dnef`, `update-dnef`, and `delete-dnef`, the `update-data-resource` contract does not guarantee preservation of extension definitions omitted from a parent update. No extension-field resource lifecycle is provided until the interaction is verified against an isolated tenant.

```hcl
data "authing_data_resource_extension_field" "department" {
  namespace_code = "default"
  resource_code  = "org_tree"
  key            = "department"
}
```

The three arguments must be known and nonempty. `id` is a JSON array `[namespace_code, resource_code, key]`; `value_type` (`STRING` or `SELECT`), `label`, and optional `description` are computed. SELECT options/config and per-node `extendFieldValue` are **not exported or managed**. The lookup checks the exact key through all pages of `list-dnef`, rejects duplicate keys, unsupported/malformed definitions or incomplete pagination, and errors on missing fields and failed requests. It does not confirm the parent is a TREE or that the definition is referenced by any node.

A separate `authing_data_resource` parent can refresh while definitions exist, but its update fails closed while they are present. Deleting the parent can remove definitions and attached values; do not delete it as a way to reconcile drift.
