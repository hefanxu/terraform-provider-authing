data "authing_global_mfa_settings" "current" {}

output "enabled_mfa_factors" {
  value = data.authing_global_mfa_settings.current.enabled_factors
}
