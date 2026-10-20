# Authing Management API coverage inventory

`openapi-operation-inventory.csv` is a snapshot of the public Management V3 and Tenant Management V3 OpenAPI operations, retrieved from:

- https://api.authing.cn/openapi-json
- https://api.authing.cn/tenant-management-openapi-json

The `path_referenced_in_provider_code` column is a **literal source-code search**, not a claim that Terraform correctly manages that operation. It may count test helpers or a path shared by multiple operations. The two specs overlap; do not add their operation totals to estimate independent capability coverage. The inventory intentionally includes operations that should **not** become Terraform resources (one-shot actions and billing); they must be triaged separately. A resource is complete only after its persistent remote state, import, drift, error handling and delete semantics are verified.

Scope decisions: include multi-tenant and tenant-member configuration; defer billing; exclude invitation sends, session kickoffs, token/secret rotation, and other one-shot actions from resource modeling. Read-only stable queries can become data sources. An undocumented or write-only operation should not be modeled as a normal managed resource without reliable readback.
