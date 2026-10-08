---
page_title: "authing_ext_idp Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing External Identity Provider (身份源).
---

# authing_ext_idp (Resource)

Manages an Authing External Identity Provider (第三方身份源，如微信、钉钉、LDAP、SAML、OIDC 等).

## Example Usage

```terraform
resource "authing_ext_idp" "wechat" {
  name = "Corporate WeChat"
  type = "wechatwork"
}

resource "authing_ext_idp" "ldap" {
  name = "Enterprise Active Directory"
  type = "ldap"
}
```

## Schema

### Required

- `name` (String) Name of the identity provider.
- `type` (String, Forces New) Identity provider type: `wechat`, `dingtalk`, `ldap`, `saml`, `oidc`, `wechatwork`, etc.

### Optional

- `tenant_id` (String, Forces New) Tenant ID if in a multi-tenant user pool. Every lookup, update, and deletion is scoped to this tenant; a returned tenant or type mismatch is an error rather than adoption of another IdP.

### Read-Only

- `ext_idp_id` (String) Authing External IdP identifier.
- `id` (String) The external identity provider ID.

## Import

Import a user-pool IdP by its ID, or a tenant IdP as `tenant_id:ext_idp_id`:

```shell
terraform import authing_ext_idp.wechat idp-123
terraform import authing_ext_idp.wechat tenant-123:idp-123
```

A tenant-scoped import must include the tenant ID; a failed scope check retains the existing state and prevents update/delete. Tenant changes replace the IdP.
