data "authing_device_exclusive_valid_scope_settings" "current" {}

output "device_exclusive_app_ids" {
  value = data.authing_device_exclusive_valid_scope_settings.current.app_ids
}
