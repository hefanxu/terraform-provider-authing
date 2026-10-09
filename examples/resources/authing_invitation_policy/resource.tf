resource "authing_invitation_policy" "employees" {
  name                      = "Employee onboarding"
  enabled_identifier_verify = false
  enabled_info_fill         = true
  register_info_fill_msg    = "Complete your profile"
}
