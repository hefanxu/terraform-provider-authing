---
page_title: "authing_role Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Manages an Authing Role.
---

# authing_role (Resource)

Manages an Authing Role within a permission namespace.

## Example Usage

```terraform
resource "authing_namespace" "platform" {
  code = "cloud_platform"
  name = "Cloud Platform"
}

resource "authing_role" "admin" {
  code        = "admin"
  name        = "Administrator"
  namespace   = authing_namespace.platform.code
  description = "Full administrative access"
}
```

## Schema

### Required

- `code` (String, Forces New) Unique code for the role within the namespace.

### Optional

- `description` (String) Description of the role.
- `name` (String) Name of the role.
- `namespace` (String, Forces New) Permission namespace code (defaults to `default`).

### Read-Only

- `id` (String) The role code.

## Destructive sandbox acceptance

`TestDestructiveLiveRoleTrace` is opt-in and skipped in normal tests. In a **disposable sandbox only**, after owner approval, use `-authing-destructive-sandbox` with `AUTHING_ACCEPTANCE_CONFIRM=DESTRUCTIVE_SANDBOX` and sandbox environment AK/SK. The tracer generates an isolated `hermesacc-` namespace and a marked role whose `namespace` HCL reference enforces parent-first creation. It checks Terraform apply → plan 0 → external **description** drift → plan 2 → reconcile → plan 0, then destroys the role before its namespace and independently GETs absence of both exact codes. Its offline `httptest` test exercises the real Terraform CLI/provider protocol but cannot prove live Authing semantics. Role name drift is **not** tested: `authing_role` Read does not refresh `name`, so a Terraform plan cannot observe out-of-band renames; description updates send the configured name and unchanged code because the API requires both fields. Namespace deletion refuses missing ownership, unrelated roles/resources/data resources, incomplete inventories, and **any** global data policy (the policy list API has no namespace filter). A refused cleanup reports the generated codes for manual investigation; it never deletes another identity. The live tracer has not been run.

## Import

Roles can be imported using `code`:

```shell
terraform import authing_role.admin admin
```
