---
page_title: "authing_data_object_field Resource - terraform-provider-authing"
description: |-
  Manages a basic Authing data object field.
---

# authing_data_object_field (Resource)

Creates, reads, and deletes **basic** fields of a data object. The Authing SDK's field DTOs disagree with the current OpenAPI schema and omit several required properties; this resource sends the complete basic creation payload through the provider's own TLS-verified Management API client. Only use it for new simple fields. **Do not adopt fields with advanced validation, defaults, enumerations, or relationships**: these settings cannot be round-tripped from the list response and in-place updates are deliberately disabled. All configuration changes require replacement, which can destroy data in the field. Back up data first.

## Example Usage

```terraform
resource "authing_data_object_field" "sku" {
  model_id = authing_data_object.inventory.id
  key      = "sku"
  name     = "SKU"
  type     = "Text"
  show     = true
  editable = true
}
```

## Schema

### Required (all Force New)

- `model_id` (String) Parent model ID.
- `key` (String) Unique field attribute key.
- `name` (String) Display name.
- `type` (String) Basic field type: `Text`, `Textarea`, `Number`, `Boolean`, or `Date`.
- `show` (Boolean) Whether to display the field.
- `editable` (Boolean) Whether to allow editing.

### Read-Only

- `id` (String) Authing field ID.

Refresh lists fields by `model_id` and matches the field ID. Only an explicit 404 or a successful list without the ID removes state; transport or API errors retain state and report diagnostics. No live Authing validation has been run.

## Import

```shell
terraform import authing_data_object_field.sku 'model-id:field-id'
```

Import is only suitable for basic fields. The import ID contains both IDs because Authing only exposes a model-scoped list endpoint for reading fields.
