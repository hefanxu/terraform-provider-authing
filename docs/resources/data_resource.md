# authing_data_resource

Manages an Authing permission data resource. This is separate from `authing_resource` (RBAC resource) and `authing_data_object` (metadata model). Deleting it removes the remote data resource **including any attached extension definitions and tree-node values**. `STRING`, `ARRAY`, and `TREE` are supported. The parent schema owns only its core attributes; extension definitions are readable through `authing_data_resource_extension_field` but are not owned or writable here. Nonempty tree-node `extendFieldValue` remains unsupported and is rejected on refresh. A parent refresh tolerates strictly validated extension definitions, but **parent update is blocked whenever any are present**: Authing does not document whether `update-data-resource` preserves omitted `extendFieldList` entries. Remove extensions explicitly outside this provider before updating the parent; never expect a parent update to preserve them.

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
- `type` — required, immutable `STRING`, `ARRAY`, or `TREE`.
- `struct` — required JSON text: a JSON string (1–500 bytes) for `STRING`; an array of distinct strings (at most 50) for `ARRAY`; or one root object for `TREE`. Tree nodes require nonempty `code` and `name` (at most 50 characters each), and optionally accept `value` (string, at most 1000 characters) and `children` (array of nodes). The root counts as level one; at most five levels are allowed. Sibling codes and sibling names must be unique, and a descendant cannot reuse an ancestor code. Unknown properties, duplicate JSON keys, and nonempty `extendFieldValue` are rejected; an empty object is accepted. Use `jsonencode`. JSON key ordering/whitespace is preserved in state when the remote value is semantically identical; imported values are canonicalized.
- `actions` — required set of action strings, at most 50. Supply `[]` explicitly to allow no actions.
- `description` — optional string. Omission on create leaves the field unset; setting `""` clears it on update.

`id` is computed as a JSON array of namespace and resource code. Import with `terraform import authing_data_resource.documents '["default","documents"]'`. Slash characters in codes are safe because the ID uses JSON, not a slash separator. Changes to immutable fields require replacement. Before an update, the provider reads the remote resource and refuses to modify a missing, retagged, or extension-bearing resource. A parent refresh tolerates valid extension definitions but does not put them in parent state; malformed/unknown extension definitions or nonempty node values fail safely. **Deleting the parent is destructive even when extensions are present.**

## Data source

```hcl
data "authing_data_resource" "documents" {
  namespace_code = "default"
  resource_code  = "documents"
}
```

The data source exports `id`, `resource_name`, `type`, `struct`, `actions`, and `description`; a missing resource is an error. A managed resource removes state only on an explicit Authing 404. Other errors preserve state and report a diagnostic.
