# Authing sandbox acceptance evidence

`docs/acceptance-matrix.csv` lists all registered resources. `not_run` means no complete Terraform CLI tracer; `implemented_not_run` means the offline `httptest` + Terraform CLI tracer passed but the real Authing service has not been exercised; `live_pass` requires a successful manually approved GitHub Actions run for that exact case. These statuses do not certify untested attributes, import, replacement, pagination, or production readiness.

| Resource | Live verdict | GitHub Actions run | Tested commit | Verified path |
|---|---|---|---|---|
| `authing_group` | Passed | [37723568672](https://github.com/hefanxu/terraform-provider-authing/actions/runs/37723568672) | `c4fdaff9eadf2dafa5cc2991bf1cb043162b5c2f` | Sandbox create → no-change plan → direct `UpdateGroup` name drift → changed plan → apply reconciliation → no-change plan → destroy → exact-code GET returned explicit 404. |

The run above completed with `destructive-group` and its live trace step successful, while the read-only job was skipped. The tracer requires an exact random `hermesacc-` identity and verifies ownership before cleanup; raw API/CLI output is suppressed. The test does not prove group `type`/code replacement, bulk-delete response semantics outside this one code, or other resources.
