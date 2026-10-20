---
page_title: "authing_namespace Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing Permission Namespace (权限空间).
---

# authing_namespace (Resource)

Manages an Authing Permission Namespace (权限空间/权限分组).

## Example Usage

```terraform
resource "authing_namespace" "platform" {
  code        = "cloud_platform"
  name        = "Cloud Platform Permissions"
  description = "Permissions and roles for the cloud management platform"
}
```

## Schema

### Required

- `code` (String, Forces New) Unique code for the permission namespace.
- `name` (String) Display name of the permission namespace.

### Optional

- `description` (String) Description of the permission namespace.

### Read-Only

- `id` (String) The permission namespace code.

## Destructive sandbox acceptance

The repository includes an opt-in Terraform CLI tracer for this resource. Run it **only** with a disposable Authing user pool, after the read-only smoke and sandbox owner approval. It requires `-authing-destructive-sandbox`, `AUTHING_ACCEPTANCE_CONFIRM=DESTRUCTIVE_SANDBOX`, and the sandbox `AUTHING_ACCESS_KEY_ID` / `AUTHING_ACCESS_KEY_SECRET` environment variables; it is skipped in normal tests. It creates a random `hermesacc-` namespace with an ownership description, verifies convergence and an out-of-band name drift, then destroys it. No roles or resources are created. Cleanup refuses a missing/mismatched identity or marker, unexpected name, or nonempty/incomplete role, legacy-resource, or data-resource inventory; a refused cleanup requires manual investigation of the reported generated code. Never target the shared `default` namespace. The offline `httptest` case does not establish compatibility with a live Authing tenant.

## Import

Permission namespaces can be imported using `code`:

```shell
terraform import authing_namespace.platform cloud_platform
```
