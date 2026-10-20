resource "authing_invitation_roster" "employees" {
  name = "Employees"
}

# To associate an existing invitation policy, set:
# policy_id = authing_invitation_policy.employees.id
