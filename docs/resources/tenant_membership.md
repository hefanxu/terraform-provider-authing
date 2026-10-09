---
page_title: "authing_tenant_membership Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Link an existing userpool user to an Authing tenant.
---

# authing_tenant_membership (Resource)

Links **one existing userpool user** to a tenant. It does not create or delete the userpool user, manage credentials, invite a user, or grant tenant-admin privileges. Changing either identifier replaces the membership.

## Example Usage

```terraform
resource "authing_tenant_membership" "alice" {
  tenant_id    = "tenant-123"
  link_user_id = authing_user.alice.id
}
```

## Schema

### Required

- `tenant_id` (String) Tenant ID; changing it replaces the membership.
- `link_user_id` (String) Existing userpool user ID; changing it replaces the membership.

### Read-Only

- `id` (String) `v1.` followed by unpadded base64url of a compact JSON array `[tenant_id,link_user_id]`.
- `member_id` (String) The Authing tenant member ID obtained by reading the exact tenant/user pair.

## Import

Import an existing tenant membership rather than creating a link already present remotely:

```shell
terraform import authing_tenant_membership.alice 'v1.WyJ0ZW5hbnQtMTIzIiwidXNlci00NTYiXQ'
```

The example ID encodes `tenant-123` and `user-456`. For arbitrary identifiers:

```python
import base64, json
parts = ["tenant-123", "user-456"]
print("v1." + base64.urlsafe_b64encode(json.dumps(parts, separators=(",", ":")).encode()).decode().rstrip("="))
```

Create checks that the pair is absent, calls `/api/v3/add-tenant-users`, then reads back the exact `tenantId`/`linkUserId` and `memberId`. Delete checks the exact pair and **removes by its recorded `memberId`**, using only `/api/v3/remove-tenant-users` with `tenantId` and a single `memberIds` entry. It refuses to detach if the remote member ID changed since state was refreshed; refresh and review before retrying. A successful response must contain `data.success: true`, and the membership must be absent on a subsequent read. A genuine 404 on get means absent; malformed replies or other API failures retain state and fail.

Authing's removal endpoint accepts either `linkUserIds` or `memberIds`; using the exact member ID avoids bulk or userpool-wide deletion. There is still a race if another actor replaces membership between preflight and removal, so coordinate external changes. Local `httptest` coverage verifies this behavior; no live Authing tenant was available for an integration test.
