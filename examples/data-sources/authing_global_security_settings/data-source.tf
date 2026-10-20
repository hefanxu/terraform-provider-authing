data "authing_global_security_settings" "current" {}

output "registration_disabled" {
  value = data.authing_global_security_settings.current.register_disabled
}
