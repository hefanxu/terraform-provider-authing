---
page_title: "authing_webhook Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing Webhook event subscription.
---

# authing_webhook (Resource)

Manages an Authing Webhook subscription for identity events.

## Example Usage

```terraform
resource "authing_webhook" "audit_hook" {
  name         = "Audit Log Webhook"
  url          = "https://api.example.com/webhooks/authing"
  content_type = "application/json"
  secret       = "whsec_supersecretkey123"
  enabled      = true

  events = [
    "user.created",
    "user.updated",
    "user.deleted",
    "user.login"
  ]
}
```

## Schema

### Required

- `events` (List of String) List of event codes to subscribe to.
- `name` (String) Webhook name.
- `url` (String) Webhook callback URL endpoint.

### Optional

- `content_type` (String) Payload format (`application/json`, etc.).
- `enabled` (Boolean) Whether the webhook is enabled. Defaults to true.
- `secret` (String, Sensitive) Secret token for signature verification.

### Read-Only

- `id` (String) The webhook ID.
- `webhook_id` (String) Authing Webhook ID.
