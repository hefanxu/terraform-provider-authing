resource "authing_data_resource" "documents" {
  namespace_code = "default"
  resource_code  = "documents"
  resource_name  = "Documents"
  type           = "ARRAY"
  struct         = jsonencode(["report", "invoice"])
  actions        = ["read", "write"]
  description    = "Document IDs"
}

data "authing_data_resource" "documents" {
  namespace_code = authing_data_resource.documents.namespace_code
  resource_code  = authing_data_resource.documents.resource_code
}

resource "authing_data_resource" "folders" {
  namespace_code = "default"
  resource_code  = "folders"
  resource_name  = "Folders"
  type           = "TREE"
  struct = jsonencode({
    code = "root"
    name = "All folders"
    children = [
      { code = "reports", name = "Reports", value = "report-folder" },
      { code = "invoices", name = "Invoices", value = "invoice-folder" }
    ]
  })
  actions = ["read"]
}
