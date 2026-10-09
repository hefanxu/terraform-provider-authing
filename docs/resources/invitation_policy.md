# authing_invitation_policy

Manages a **persistent** invitation policy. This resource does not send invitations, generate invitation links, or manage invitation recipients. Import by Authing `policyId`.

```hcl
resource "authing_invitation_policy" "employees" {
  name                      = "Employee onboarding"
  enabled_identifier_verify = false
  enabled_info_fill         = true
  register_info_fill_msg    = "Complete your profile"
}
```

```sh
terraform import authing_invitation_policy.employees POLICY_ID
```

## Arguments

- `name` (required): Invitation policy name, 1–200 characters.
- `enabled_identifier_verify` (optional, computed): Require identity verification.
- `enabled_info_fill` (optional, computed): Require registration information completion.
- `register_info_fill_msg` (optional, computed): Information completion prompt. An omitted/null value differs from an explicit empty string.

`id` is the computed Authing `policyId`.

Only fields reliably available in both write requests and get-policy responses are exposed. Password configuration (including default passwords), link generation, email templates, external identity provider binding, extension fields, and one-shot invitation operations are intentionally not managed. Omitted optional values are read from Authing and are not sent on create; changed non-null values are sent on update. The API does not provide a documented way to clear `register_info_fill_msg` back to null; an explicit empty string can be written instead. Authing may have tenant-specific defaults. Live-tenant compatibility has not been verified.
