---
page_title: "authing_application_subject_auth Data Source - terraform-provider-authing"
subcategory: ""
description: |-
  Reads an application subject authorization detail without granting access.
---

# authing_application_subject_auth (Data Source)

Looks up authorization details via `GET /api/v3/get-subject-auth-detail`. This data source only reads an existing authorization; it does not assign, grant, or revoke access. Authing must return a complete detail for the exact requested application and subject.

## Example Usage

```terraform
data "authing_application_subject_auth" "portal_user" {
  target_id   = "subject-id"
  target_type = "USER"
  app_id      = "application-id"
}

output "portal_auth_type" {
  value = data.authing_application_subject_auth.portal_user.auth_type
}
```

## Schema

### Required

- `target_id` (String) Subject ID to query.
- `target_type` (String) Subject type: `USER`, `ROLE`, `GROUP`, `ORG`, or `AK_SK`.
- `app_id` (String) Application ID to query.

### Read-Only

- `app_name` (String) Application name.
- `req_target_id` (String) Requested subject ID returned by Authing.
- `req_target_name` (String) Requested subject name returned by Authing.
- `req_target_type` (String) Requested subject type returned by Authing.
- `result_target_type` (String) Target subject type returned by Authing (API `targetType`), distinct from the input `target_type` (API query `targetType`).
- `target_name` (String) Target subject name returned by Authing.
- `auth_type` (String) Authorization type returned by Authing: `DEFAULT`, `ALL`, `SELF`, or `SUBJECT`.

Missing fields, mismatched application/subject, and API failures raise diagnostics rather than writing partial state. No real-tenant behavior has been validated by the local tests.
