resource "authing_application" "sso_portal" {
  app_name             = "Corporate SSO Portal"
  app_type             = "web"
  app_identifier       = "corporate-sso-portal"
  app_logo             = "https://sso.example.com/logo.png"
  default_protocol     = "oidc"
  sso_enabled          = true
  permission_strategy  = "DENY_ALL"
  redirect_uris        = ["https://sso.example.com/oauth/callback"]
  logout_redirect_uris = ["https://sso.example.com/logout"]
  init_login_url       = "https://sso.example.com/login"
  description          = "Central authentication application portal"
}

resource "authing_ext_idp" "enterprise_ldap" {
  name = "Enterprise Active Directory"
  type = "ldap"
}

resource "authing_webhook" "user_events" {
  name         = "User Event Notification"
  url          = "https://events.example.com/webhook/authing"
  content_type = "application/json"
  secret       = "whsec_authing_token_sample_123"
  enabled      = true

  events = [
    "user.created",
    "user.updated",
    "user.deleted"
  ]
}

resource "authing_pipeline_function" "email_domain_check" {
  func_name        = "VerifyCorporateEmailDomain"
  func_description = "Ensures registering users have @example.com email"
  scene            = "PRE_REGISTER"
  is_asynchronous  = false

  source_code = <<EOF
async function pipe(context, callback) {
  const { user } = context;
  if (!user.email || !user.email.endsWith("@example.com")) {
    return callback(new Error("Only corporate email addresses ending in @example.com are allowed."));
  }
  callback(null, context);
}
EOF
}
