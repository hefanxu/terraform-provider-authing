---
page_title: "authing_pipeline_function Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing Pipeline serverless function.
---

# authing_pipeline_function (Resource)

Manages an Authing Pipeline serverless extension function.

## Example Usage

```terraform
resource "authing_pipeline_function" "ip_whitelist" {
  func_name        = "VerifyRegistrationIP"
  func_description = "Checks registration IP against allowed subnet"
  scene            = "PRE_REGISTER"
  is_asynchronous  = false

  source_code = <<EOF
async function pipe(context, callback) {
  const { request } = context;
  if (!request.ip.startsWith("10.")) {
    return callback(new Error("Registration only allowed from internal network."));
  }
  callback(null, context);
}
EOF
}
```

## Schema

### Required

- `func_name` (String) Pipeline function name.
- `scene` (String) Trigger scene:
  - `PRE_REGISTER`: Triggered before user registers.
  - `POST_REGISTER`: Triggered after user registers.
  - `PRE_AUTHENTICATION`: Triggered before authentication.
  - `POST_AUTHENTICATION`: Triggered after authentication.
  - `PRE_OIDC_ID_TOKEN_ISSUED`: Triggered before OIDC ID Token issuance.
  - `PRE_OIDC_ACCESS_TOKEN_ISSUED`: Triggered before OIDC Access Token issuance.
  - `PRE_COMPLETE_USER_INFO`: Triggered before completing user info.
- `source_code` (String) JavaScript source code of the pipeline function.

### Optional

- `func_description` (String) Pipeline function description.
- `is_asynchronous` (Boolean) Whether the pipeline function runs asynchronously.

### Read-Only

- `func_id` (String) Authing Pipeline function ID.
- `id` (String) The pipeline function ID.
