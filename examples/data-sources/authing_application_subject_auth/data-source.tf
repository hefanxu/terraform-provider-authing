data "authing_application_subject_auth" "portal_user" {
  target_id   = "subject-id"
  target_type = "USER"
  app_id      = "application-id"
}

output "portal_auth_type" {
  value = data.authing_application_subject_auth.portal_user.auth_type
}
