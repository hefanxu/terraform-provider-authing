resource "authing_data_policy_assignment" "reader" {
  policy_id   = "policy-123"
  target_type = "USER"
  target_id   = "user-456"
}
