# authing_invitation_roster

Manages a persistent Authing invitation roster's name and optional policy association. It does **not** send invitations, manage roster members, import/export recipient lists, or expose the roster secret.

```hcl
resource "authing_invitation_policy" "employees" {
  name = "Employee onboarding"
}

resource "authing_invitation_roster" "employees" {
  name      = "Employees"
  policy_id = authing_invitation_policy.employees.id
}
```

```sh
terraform import authing_invitation_roster.employees ROSTER_ID
```

## Arguments

- `name` (required): Nonempty roster name.
- `policy_id` (optional): Invitation policy ID. Omit to leave the roster unassociated; removing a previously configured value unbinds the policy using the exact roster ID and previous policy ID. A remote policy change since the last refresh blocks an unsafe unbind.

`id` is the computed Authing `rosterId`. Creation sends only `name`, then associates `policy_id` with a separate update. All writes require GET readback to confirm the name and policy before state is updated. If a create succeeds but subsequent association or verification fails, the diagnostic includes the created roster ID; import that ID rather than retrying create. Import refreshes the current name and policy. A documented GET with `withAssignedPolicy=true` is used to read association. Deletion uses the singular delete-roster endpoint and confirms the exact roster is absent.

Only metadata exposed by the create, update, and get-roster APIs is managed. The roster secret, invitation sending, recipient import/export, and roster contents are deliberately excluded. Live-tenant compatibility has not been verified.
