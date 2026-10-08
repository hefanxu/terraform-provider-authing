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
- `app_identifier` (String) Unique application identifier. Authing describes this as required for self-built applications; existing configurations may omit it to retain their current behavior. Changes are sent to Authing on update.
- `app_logo` (String) Application logo URL.
- `default_protocol` (String) Default application protocol: `oidc`, `oauth`, `saml`, `cas`, or `asa`.
- `sso_enabled` (Boolean) Enable SSO. An explicit `false` is sent on create and update; omission uses Authing's value.
- `description` (String) Application description.
- `init_login_url` (String) Initial login URL.
- `logout_redirect_uris` (List of String) Allowed logout redirect callback URIs.
- `redirect_uris` (List of String) Allowed redirect callback URIs.

### Read-Only

- `app_id` (String) Authing Application ID.
- `id` (String) The application ID.

## Import

Applications can be imported using `app_id`:

```shell
terraform import authing_application.portal 6343b98b7cfxxx9366e9b7c
```
