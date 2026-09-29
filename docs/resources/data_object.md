---
page_title: "authing_data_object Resource - terraform-provider-authing"
description: |-
  Manages an Authing custom data object model.
---

# authing_data_object (Resource)

Manages a custom data object model through Authing's metadata API. **Deleting a model may delete its fields and associated data**; back up data before destroying or replacing it. Manage fields separately with `authing_data_object_field`.

## Example Usage

```terraform
resource "authing_data_object" "inventory" {
  name           = "Inventory"
  description    = "Stock records"
  type           = "custom"
  parent_key     = ""
  enable         = true
  data_type      = "list"
  show_field_key = ""
}
```

## Schema

### Required

- `name` (String) Display name.
- `description` (String) Model description; use `""` if none.
- `type` (String, Forces New) Model type, usually `custom` for user-defined objects.
- `parent_key` (String) Parent menu key; use `""` for none.
- `enable` (Boolean) Whether the model is enabled.
- `data_type` (String, Forces New) `list` or `tree`.
- `show_field_key` (String) Field key to display. Use `""` if none. Authing requires this for update but does not return it from get-model; set this to the actual remote value when adopting an existing model. Drift in this setting cannot be detected during refresh.

### Read-Only

- `id` (String) Authing model ID.

Updates preserve the current remote `fieldOrder` and `config` by reading them before applying changes; concurrent edits to those settings can still race. A missing model is removed from state only on an explicit 404 response; other API errors remain errors.

## Import

```shell
terraform import authing_data_object.inventory model-id
```

After import, configure the required fields to match the remote model, particularly `show_field_key` (which the get-model API does not expose).
