---
page_title: "authing_auth_flow_function Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing AuthFlowFunction with verified readback.
---

# authing_auth_flow_function (Resource)

Manages an AuthFlowFunction using the `/api/v3/*-auth-flow-function` management endpoints. This is distinct from `authing_pipeline_function`; do not manage the same remote function with both resources. The `appId` create/update parameter is intentionally unsupported: the GET response does not return it, so application scope cannot be verified or drift-detected. One-shot execute/re-upload operations are not managed.

## Example Usage

```terraform
resource "authing_auth_flow_function" "before_login" {
  func_name = "CheckLogin"
  scene     = "PRE_AUTHENTICATION"
  enabled   = false
  timeout   = 3

  source_code = <<-JS
    async function pipe(user, context, callback) {
      callback(null, user, context);
    }
  JS
}
```

## Import

```shell
terraform import authing_auth_flow_function.before_login <funcId>
```

Import reads the full source code from Authing. `source_code` is marked **Sensitive**, which hides normal CLI output but **does not encrypt or omit it from Terraform state or saved plans**. Protect state storage, backups, and access. The API has not been tested against a live Authing tenant; validate behavior there before production use.

## Schema

Required: `func_name`, `scene`, `source_code` (Sensitive). Changing `scene` replaces the resource because the update endpoint cannot change it.

Optional, read back from Authing: `func_description`, `is_asynchronous`, `timeout` (seconds, Authing permits 1–60), `terminate_on_timeout`, `enabled`. Omitted values adopt Authing's returned defaults. Read-only `id` is Authing's stable `funcId`.

Create and update verify the returned ID and GET readback; delete confirms absence. If a create succeeds but readback fails, the diagnostic includes the returned `funcId` for recovery via import rather than silently persisting unverified state.
