---
page_title: "authing_tenant_admin Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Grant administrator privilege to one existing Authing tenant member.
---

# authing_tenant_admin (Resource)

Grants administrator privilege to an **existing** tenant membership. This resource does not create a membership or user. Destroy only revokes administrator privilege; it never removes the member or user. Changing either identifier replaces the relation.

## Example Usage

```terraform
resource "authing_tenant_membership" "alice" {
  tenant_id    = "tenant-123"
  link_user_id = authing_user.alice.id
}

resource "authing_tenant_admin" "alice" {
  tenant_id    = authing_tenant_membership.alice.tenant_id
  link_user_id = authing_tenant_membership.alice.link_user_id
}
```

## Schema

### Required

- `tenant_id` (String) Tenant ID; replacement on change.
- `link_user_id` (String) Linked userpool user ID; replacement on change.

### Read-Only

- `id` (String) `ta1.` followed by unpadded base64url of a compact JSON array `[tenant_id,link_user_id]`.
- `member_id` (String) Pinned Authing tenant membership ID obtained from `get-tenant-user`.

## Import

Import an already-admin tenant member rather than trying to grant it again:

```shell
terraform import authing_tenant_admin.alice 'ta1.WyJ0ZW5hbnQtMTIzIiwidXNlci00NTYiXQ'
```

The example encodes `tenant-123` and `user-456`. For other values:

```python
import base64, json
parts = ["tenant-123", "user-456"]
print("ta1." + base64.urlsafe_b64encode(json.dumps(parts, separators=(",", ":")).encode()).decode().rstrip("="))
```

Create checks the exact tenant/user pair exists and is not already admin, grants via `/api/v3/set-tenant-admin` using a single `memberIds` entry, then reads back the same member ID with `isTenantAdmin: true`. An existing admin must be imported. Refresh removes state on a genuine 404 or revoked privilege; other failures retain state. Delete checks the pinned member ID, calls only `/api/v3/delete-tenant-admin` with that `memberId`, and verifies revocation. If the member ID changes, it refuses to revoke the replacement. Authing mutation responses must have `statusCode: 200`; if an optional `data.success` is present it must be true. A remote actor could replace a membership between preflight and mutation, so coordinate concurrent changes. Tested with local HTTP mocks; not tested against a live tenant.
