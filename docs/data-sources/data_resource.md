# authing_data_resource

Looks up an Authing permission data resource using `namespace_code` and `resource_code`. See the [resource documentation](../resources/data_resource.md) for the supported `STRING`, `ARRAY`, and `TREE` structures, attributes, and extension limitations.

```hcl
data "authing_data_resource" "existing" {
  namespace_code = "default"
  resource_code  = "documents"
}

output "document_actions" {
  value = data.authing_data_resource.existing.actions
}
```

An absent resource is an error; the data source does not create or delete anything.
