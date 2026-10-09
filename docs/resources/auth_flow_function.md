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

## Sandbox lifecycle tracer

`TestDestructiveLiveAuthFlowFunctionTrace` is opt-in only (destructive sandbox flag, exact confirmation and isolated credentials); ordinary tests use a local `httptest` service and the real Terraform CLI. It creates a unique `hermesacc-` function in `PRE_AUTHENTICATION` with `enabled = false`, no application scope, and inert pass-through source. The test checks the state-derived exact `funcId`, name, description, scene, source and disabled status by GET; proves converged plans, out-of-band name drift (plan exit 2), reconciliation and GET 404 after destroy. Failures attempt deletion only for a state-derived ID whose GET still matches those ownership fields. If create succeeded but Terraform has no state ID, cleanup cannot be authorized: the test retains the first failing `phase`, generated `code`, and `cleanup=incomplete`. With a state-pinned ID, only an exact ownership GET permits deletion; a failed or uncertain cleanup reports `cleanup=unknown`, while a confirmed GET 404 reports `cleanup=confirmed`. Source code exists in state and Terraform output is never printed by the tracer. This mock trace does not establish live Authing compatibility or application-scoped behavior. The live selector remains disabled after run 37874218372: the published [Management V3 OpenAPI](https://api.authing.cn/openapi-json) has no `list-auth-flow-functions` endpoint. Its `list-pipeline-functions` belongs to the Pipeline controller, while exact auth-flow GET requires `funcId`; neither contract establishes a complete auth-flow inventory by name. Thus absence and cleanup of the earlier generated name cannot be proven, and no live write or name-derived deletion is authorized.
