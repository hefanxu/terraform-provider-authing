resource "authing_public_account" "shared" {
  username = "shared-lobby"
  name     = "Lobby account"
  nickname = "Lobby"
  email    = "shared-lobby@example.com"
}

data "authing_public_account" "shared_lookup" {
  user_id = authing_public_account.shared.id
}
