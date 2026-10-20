---
page_title: "authing_data_object_row Data Source - terraform-provider-authing"
subcategory: "Data Objects"
description: |-
  Reads a data-object row by model ID and row ID, exposing field-ID-keyed cell JSON.
---

# authing_data_object_row (Data Source)

Read a row without managing its lifecycle. The Management API specifies `create-row`, `update-row`, and `remove-row`, but does not establish whether write `data` keys use field IDs or field keys, or whether partial updates preserve other cells and relations. A Terraform row resource could silently lose data; this provider does not expose one.

```terraform
data "authing_data_object_row" "example" {
  model_id = "model-id"
  row_id   = "row-id"
}

# Decode when needed: jsondecode(data.authing_data_object_row.example.cells_json)["field-id"]
```

`cells_json` is a JSON object keyed by exact field IDs, with values returned by Authing's `get-row` cell list. The lookup requests `showFieldId=true` and `showRelation=false`; relation data is not represented. It rejects missing/mismatched row identity, missing or duplicate cell IDs, incomplete cells, and unsuccessful responses rather than returning partial state. An empty cell list produces `{}`.

**Security:** Cell values may contain secrets or personal data. `cells_json` is marked sensitive in Terraform output, but its contents still reside in Terraform state. Protect the state backend and any decoded outputs; do not use this data source for values that must not enter Terraform state.

## Argument Reference

- `model_id` (Required, String) — Data-object model ID.
- `row_id` (Required, String) — Exact row ID.

## Attribute Reference

- `cells_json` (Sensitive, String) — JSON object keyed by field ID.
