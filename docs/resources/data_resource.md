# authing_data_resource

Manages an Authing permission data resource. This is separate from `authing_resource` (RBAC resource) and `authing_data_object` (metadata model). Deleting it removes the remote data resource. Only `STRING` and `ARRAY` are supported. `TREE` and nonempty `extendFieldList` are deliberately rejected: their nested structure and extension values cannot be round-tripped safely by this schema.

```hcl
resource "authing_data_resource" "documents" {
  namespace_code = "default"
  resource_code  = "documents"
  resource_name  = "Documents"
  type           = "ARRAY"
  struct         = jsonencode(["report", "invoice"])
  actions        = ["read", "write"]
  description    = "Document IDs"
}
```

## Arguments

- `namespace_code`, `resource_code`, `resource_name` — required strings; namespace and code are immutable.
- `type` — required, immutable `STRING` or `ARRAY`.
- `struct` — required JSON text: a JSON string (1–500 bytes) for `STRING`, or an array of distinct strings (at most 50) for `ARRAY`. Use `jsonencode`. Whitespace is preserved in state when the remote value is semantically identical.
- `actions` — required set of action strings, at most 50. Supply `[]` explicitly to allow no actions.
- `description` — optional string. Omission on create leaves the field unset; setting `""` clears it on update.

`id` is computed as a JSON array of namespace and resource code. Import with `terraform import authing_data_resource.documents '["default","documents"]'`. Slash characters in codes are safe because the ID uses JSON, not a slash separator. Changes to immutable fields require replacement. Before an update, the provider reads the remote resource and refuses to modify a missing, retagged, or extended resource. The provider does not manage tree extension fields; importing a tree or extended resource reports an error rather than silently dropping its configuration.

## Data source

```hcl
data "authing_data_resource" "documents" {
  namespace_code = "default"
  resource_code  = "documents"
}
```

The data source exports `id`, `resource_name`, `type`, `struct`, `actions`, and `description`; a missing resource is an error. A managed resource removes state only on an explicit Authing 404. Other errors preserve state and report a diagnostic.
