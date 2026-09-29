---
page_title: "Provider: Authing"
subcategory: ""
description: |-
  The Authing provider enables managing Authing IAM Identity Platform resources with Terraform / OpenTofu.
---

# Authing Provider

The Authing provider is used to interact with the resources supported by [Authing](https://www.authing.cn) Identity and Access Management (IAM) platform. The provider needs to be configured with the proper credentials before it can be used.

## Example Usage

```terraform
terraform {
  required_providers {
    authing = {
      source  = "authing/authing"
      version = "~> 1.0.0"
    }
  }
}

# Configure the Authing Provider
provider "authing" {
  access_key_id     = var.authing_access_key_id
  access_key_secret = var.authing_access_key_secret
  # host            = "https://api.authing.cn" # Optional custom API host
}
```

## Schema

### Optional

- `access_key_id` (String) Authing User Pool ID / Access Key ID. Can also be set via the `AUTHING_ACCESS_KEY_ID` or `AUTHING_USERPOOL_ID` environment variable.
- `access_key_secret` (String, Sensitive) Authing User Pool Secret / Access Key Secret. Can also be set via the `AUTHING_ACCESS_KEY_SECRET` or `AUTHING_USERPOOL_SECRET` environment variable.
- `host` (String) Authing OpenAPI host URL. Defaults to `https://api.authing.cn`. Can also be set via the `AUTHING_HOST` environment variable.
- `tenant_id` (String) Authing Tenant ID for multi-tenant environments. Can also be set via the `AUTHING_TENANT_ID` environment variable.
