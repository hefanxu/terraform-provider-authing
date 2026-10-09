resource "authing_custom_domain" "login" {
  custom_domain = "sso.example.com"
}

output "dns_txt_name" {
  value = authing_custom_domain.login.dns_txt_name
}

output "dns_txt_value" {
  value = authing_custom_domain.login.dns_txt_value
}
