data "authing_ext_idp_connection" "corporate" {
  ext_idp_id    = "6318061be13c0ce6a64093e5"
  connection_id = "60b49eb83fd80adb96f26e68"
}

output "corporate_connection_type" {
  value = data.authing_ext_idp_connection.corporate.type
}
