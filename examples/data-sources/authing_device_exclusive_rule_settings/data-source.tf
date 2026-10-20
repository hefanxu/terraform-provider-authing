data "authing_device_exclusive_rule_settings" "current" {}

output "device_exclusive_rule" {
  value = data.authing_device_exclusive_rule_settings.current.rule
}
