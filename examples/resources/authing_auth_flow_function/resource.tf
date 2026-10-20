resource "authing_auth_flow_function" "before_login" {
  func_name = "CheckLogin"
  scene     = "PRE_AUTHENTICATION"
  enabled   = false

  source_code = <<-JS
    async function pipe(user, context, callback) {
      callback(null, user, context);
    }
  JS
}
