# authing_invitation_invitee

Manages one persistent invitee entry in an existing invitation roster. This resource **does not send invitations**, generate invitation links, or manage other invitees. Destroy calls batch deletion with exactly one `inviteeId`; deleting the roster itself is separate and may affect all its entries.

`name`, `email`, and optional `phone` are personal data in Terraform state (including remote state and backups). Protect state storage and access. Marking values sensitive in CLI output would **not** encrypt state.

The provider checks the complete, unfiltered roster list before creation and refuses an existing email (case-insensitive); import that entry instead. It pages by roster and compares exact invitee IDs for refresh, update, and deletion. A failed or incomplete list, missing ID, or transient error does not imply absence. Creation and updates require list readback of the configured values; an email that Authing normalizes to a different case will not round-trip exactly, so use the returned casing when importing. If creation succeeds but readback fails, the diagnostic includes a recovery import ID. There is no live-service acceptance verification of this contract yet.

## Example

```hcl
resource "authing_invitation_invitee" "guest" {
  roster_id = authing_invitation_roster.guests.id
  name      = "Ada Lovelace"
  email     = "ada@example.com"
  # phone   = "18800008888"
}
```

## Schema

- `roster_id` (required, replacement): Parent invitation roster ID.
- `name` (required): Invitee name.
- `email` (required): Invitee email; preserve server-returned casing.
- `phone` (optional): Invitee phone; omitting clears it on update. Country code is not managed.
- `invitee_id` (computed): Stable Authing invitee ID.
- `id` (computed): Versioned composite roster/invitee identity.

## Import

Use the computed ID from an existing state, or construct `ii1.` followed by unpadded URL-safe base64 encoding of the JSON array `["roster-id","invitee-id"]`. For example, generate the import key locally:

```sh
python3 -c 'import base64,json; print("ii1."+base64.urlsafe_b64encode(json.dumps(["roster-id","invitee-id"],separators=(",",":")).encode()).decode().rstrip("="))'
```

Then run `terraform import authing_invitation_invitee.guest 'ii1.…'`. The composite ID is validated before state is populated. Keep PII-bearing state private.
