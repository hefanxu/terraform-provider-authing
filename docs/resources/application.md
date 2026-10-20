---
page_title: "authing_application Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing Application (自建应用).
---

# authing_application (Resource)

Manages an Authing Application (自建应用 / 开发者应用).

## Example Usage

```terraform
resource "authing_application" "portal" {
  app_name             = "Company Portal"
  app_type             = "web"
  app_identifier       = "company-portal"
  app_logo             = "https://portal.example.com/logo.png"
  default_protocol     = "oidc"
  sso_enabled          = false
  permission_strategy  = "DENY_ALL"
  redirect_uris        = ["https://portal.example.com/callback"]
  logout_redirect_uris = ["https://portal.example.com/logout"]
  init_login_url       = "https://portal.example.com/login"
  description          = "Main internal employee portal"
}
```

## Schema

### Required

- `app_name` (String) Application name.

### Optional

- `app_type` (String) Application type (e.g. `web`, `spa`, `native`, `api`).
- `app_identifier` (String) Unique application identifier. The published CreateApplicationDto schema requires `appName` only; this field remains optional. Changes are sent to Authing on update and confirmed by GET readback.
- `app_logo` (String) Application logo URL.
- `default_protocol` (String) Default application protocol: `oidc`, `oauth`, `saml`, `cas`, or `asa`.
- `sso_enabled` (Boolean) Enable SSO. An explicit `false` is sent on create and update; omission uses Authing's value. Application updates are confirmed against GET readback, and an acknowledged write with mismatched or failed readback reports an error without replacing prior Terraform state.
- `permission_strategy` (String) Default application access policy: `ALLOW_ALL` or `DENY_ALL`. Optional and computed: omission adopts Authing's current policy. The provider sets it after application creation and refreshes it on read. There is no independent strategy delete/reset operation; removing this argument does not reset Authing's policy. Deleting the application deletes the application, not just this policy. If a follow-up strategy operation fails after creation, import the reported application ID before retrying to avoid duplicates.
- `description` (String) Application description.
- `init_login_url` (String) Initial login URL.
- `logout_redirect_uris` (List of String) Allowed logout redirect callback URIs.
- `redirect_uris` (List of String) Allowed redirect callback URIs.

### Read-Only

- `app_id` (String) Authing Application ID.
- `id` (String) The application ID.

## Sandbox acceptance trace

The application lifecycle test is opt-in and destructive. Use **only a disposable Authing sandbox** with `AUTHING_ACCESS_KEY_ID`, `AUTHING_ACCESS_KEY_SECRET`, and (if needed) `AUTHING_HOST` set in the environment:

```shell
AUTHING_ACCEPTANCE_CONFIRM=DESTRUCTIVE_SANDBOX go test ./scripts/acceptance -run '^TestDestructiveLiveApplicationTrace$' -count=1 -args -authing-destructive-sandbox
```

It creates one randomly named `hermesacc-` web application with SSO disabled, `DENY_ALL` access, an inert callback, and no secrets. It checks convergence, exact ID and ownership marker, remote name drift, reconciliation, deletion, and GET absence. Cleanup deletes only the exact ID after ownership verification; if cleanup cannot be confirmed, the test reports `cleanup-incomplete` for manual investigation. Mock tests run without the flag or credentials; the live trace is skipped by default.

## Import

Applications can be imported using `app_id`:

```shell
terraform import authing_application.portal 6343b98b7cfxxx9366e9b7c
```
