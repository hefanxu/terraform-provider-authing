data "authing_device_status" "kiosk" {
  device_id = "device-row-id"
}

output "kiosk_status" {
  value = data.authing_device_status.kiosk.status
}
