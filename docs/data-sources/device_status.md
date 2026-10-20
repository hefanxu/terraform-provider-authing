---
page_title: "authing_device_status Data Source - terraform-provider-authing"
subcategory: ""
description: |-
  Reads a terminal device's current status without managing its lifecycle.
---

# authing_device_status (Data Source)

Reads the status of an existing Authing terminal device using `POST /api/v3/device-status` with the device row ID. This is a read-only lookup: it does not create, suspend, activate, or delete the device.

## Example Usage

```terraform
data "authing_device_status" "kiosk" {
  device_id = "device-row-id"
}

output "kiosk_status" {
  value = data.authing_device_status.kiosk.status
}
```

## Schema

### Required

- `device_id` (String) Terminal device row ID returned when the device was created.

### Read-Only

- `status` (String) `activated`, `suspended`, or `deactivated`.
- `diff_time` (Number) Remaining suspension time in seconds when supplied by Authing; otherwise null. A zero value, when returned, is retained as zero.

An unsuccessful or incomplete API response causes the data source read to fail; it is not treated as an empty device. No real-tenant behavior has been validated by the local tests.
