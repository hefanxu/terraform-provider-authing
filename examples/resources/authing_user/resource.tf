resource "authing_user" "developer" {
  username       = "dev_zhang"
  email          = "zhang@example.com"
  phone          = "13800138000"
  nickname       = "San Zhang"
  password       = "P@ssw0rd12345!"
  status         = "Activated"
  gender         = "M"
  email_verified = true
  phone_verified = true
}

resource "authing_group" "backend" {
  code        = "backend_devs"
  name        = "Backend Developers"
  description = "Engineers responsible for backend microservices"
}

resource "authing_group_member" "dev_to_backend" {
  group_code = authing_group.backend.code
  user_id    = authing_user.developer.id
}
