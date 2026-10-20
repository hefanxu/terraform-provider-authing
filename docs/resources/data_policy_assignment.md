---
page_title: "authing_data_policy_assignment Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Authorizes one subject for one Authing data policy.
---

# authing_data_policy_assignment (Resource)

Manages **one policy-to-subject authorization**. The resource does not own or replace the policy's other authorizations. Changing any identifier replaces the assignment. Supported subject types are `USER`, `ORG`, `GROUP`, `ROLE`, and `PROGRAMMATIC_ACCOUNT`.

## Example Usage

```terraform
resource "authing_data_policy_assignment" "reader" {
  policy_id   = authing_data_policy.readers.id
  target_type = "USER"
  target_id   = authing_user.reader.id
}
```

## Schema

### Required

- `policy_id` (String) Authing data policy ID. Changing this replaces the assignment.
- `target_type` (String) Subject type. Changing this replaces the assignment.
- `target_id` (String) Subject ID. Changing this replaces the assignment.

### Read-Only

- `id` (String) Versioned composite identity (`v1.` followed by unpadded base64url of a compact JSON array `[policy_id,target_type,target_id]`).

## Import

Import an existing authorization using its composite ID. For example, `policy-123`, `USER`, `user-456` encode as:

```shell
terraform import authing_data_policy_assignment.reader 'v1.WyJwb2xpY3ktMTIzIiwiVVNFUiIsInVzZXItNDU2Il0'
```

To generate IDs for arbitrary identifiers:

```python
import base64, json
parts = ["policy-123", "USER", "user-456"]
print("v1." + base64.urlsafe_b64encode(json.dumps(parts, separators=(",", ":")).encode()).decode().rstrip("="))
```

Reads scan the policy's paginated target list for the exact subject ID **and** type. A failed list request is reported as an error rather than treated as missing. Deletion only revokes this authorization and succeeds without a revoke call if it is already absent. A valid list response is needed to establish absence; policy/API 404 responses on listing are treated as errors. This resource is tested with a local mock API, not a live Authing tenant.
