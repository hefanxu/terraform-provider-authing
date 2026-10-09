data "authing_data_object_row" "example" {
  model_id = "model-id"
  row_id   = "row-id"
}

# jsondecode(data.authing_data_object_row.example.cells_json)["field-id"]
# Cell values are sensitive and remain in Terraform state.
