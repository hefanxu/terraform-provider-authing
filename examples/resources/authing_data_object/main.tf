resource "authing_data_object" "inventory" {
  name           = "Inventory"
  description    = "Stock records"
  type           = "custom"
  parent_key     = ""
  enable         = true
  data_type      = "list"
  show_field_key = ""
}

resource "authing_data_object_field" "sku" {
  model_id = authing_data_object.inventory.id
  key      = "sku"
  name     = "SKU"
  type     = "Text"
  show     = true
  editable = true
}
