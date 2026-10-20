resource "authing_invitation_invitee" "guest" {
  roster_id = authing_invitation_roster.guests.id
  name      = "Ada Lovelace"
  email     = "ada@example.com"
}
