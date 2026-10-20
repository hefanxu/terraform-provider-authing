# Authing Terraform Provider: management coverage roadmap

## Scope and definition of done

The target is **declarative management of persistent configuration**, not a Terraform wrapper around every HTTP operation. Both Management V3 specifications are in scope (`openapi-json` and `tenant-management-openapi-json`); their shared paths must be counted once. Billing/metering is deferred. Sending invitations, logging users out, triggering jobs, rotating secrets, approvals and other one-shot operations are excluded from resources. Useful stable lookups may become data sources.

For each managed object, acceptance means: a stable identity; Create/Read/Update/Delete or a documented safe exception; import; refresh and drift detection; well-defined ownership for collection fields; explicit 404 versus transport/authorization failures; protection against deleting unrelated objects; documentation and local mock tests. **Live tenant acceptance tests are still required before production use.** Merely referencing an API path does not establish coverage.

## Recommended sequence

| Stage | Domains and primary capabilities | Critical gate |
| --- | --- | --- |
| 0 | Fix existing user, group, organization, department, role, application and identity-source lifecycle defects; HTTPS/token handling; CI. | No silent state deletion on 5xx/403 and no ignored delete failures. |
| 1 | Tenant, existing-user membership, tenant administrators, tenant organizations/departments/member relationships. | Distinguish `tenantId`, `memberId`, `linkUserId`; never delete a user-pool user when removing membership. |
| 2 | Complete application settings and separate app authorization/configuration; identity-provider connections; auth-flow configuration. | Read back every managed field; avoid conflicting ownership of an application's settings. |
| 3 | Data-object models, fields, rows and relations; data resources including TREE and extension fields; data-policy assignments and custom fields. | Validate field/row type round trips and destructive delete semantics. |
| 4 | Global security/MFA, device-exclusive settings, LDAP/message configuration, domain and whitelist/risk-policy objects. | Singleton settings lacking a delete API must not claim that `destroy` resets remote state. |
| 5 | Remaining persistent invitation policies/rosters, public accounts and associations, tenant IdP/connection relationships, supported read-only lookups. | Keep one-shot sends, kicks, refreshes, execution triggers, audit events and billing out of managed resources. |

## Known API limitations to validate before modeling

- The Management spec includes untagged `/api/v3/add-device` (and deprecated `/api/v3/create-device`) but does not declare management security for them or offer a complete device detail GET. Device status has a read endpoint; full device CRUD is not yet justified.
- Sync tasks have create/get/update but no documented delete endpoint; a normal destructible Terraform resource would misrepresent this lifecycle.
- Tenant creation and update accept different field sets; `logo` is modeled differently in input and output. Treat non-round-trippable values as create-only or exclude until tested.
- Identity-provider connections only offer parent-scoped list readback; verify stable IDs, secret masking and tenant scoping.
- Application authorization detail does not expose the complete ALLOW/DENY effect of a grant; do not infer complete assignment state from that endpoint alone.
- Collection-level `set-*` endpoints can overwrite values owned by other admins or resources. Explicitly choose either whole-collection ownership or independent item lifecycle.

## Evidence

- Management V3 OpenAPI: https://api.authing.cn/openapi-json
- Tenant Management V3 OpenAPI: https://api.authing.cn/tenant-management-openapi-json
- Raw operation snapshot: [openapi-operation-inventory.csv](openapi-operation-inventory.csv). Its literal `path_referenced_in_provider_code` flag is *not* a coverage claim.
