# 远端 Authing 测试数据清理核查

## 结论：尚不能宣称全部清空

本次执行器向 Authing **创建 0、更新 0、删除并读回验证 0**。只读盘点先发现 4 个对象候选及 1 条关系；后续只读快照中，群组、用户和租户候选在外部发生了变化。本执行器未发出这些删除，不能把它们记为本次清理成果，也不能判断操作者。

最终 [只读核查 39362459089](https://github.com/hefanxu/terraform-provider-authing/actions/runs/39362459089)，测试提交 `17b2ce6c33a7f7eff1814eecbfa354bed7dc1025`：

- **6 个已知目标精确 GET 返回业务 404**；另一个原租户候选在完整、未过滤的租户列表中不存在，列表总数为 0。这 7 个目标不是本次删除的计数。
- 完成 **10 个资源 family、19 个有效读取 scope**，其中 Pipeline 按官方全部 10 个 scene 分别读取。当前匹配生成标记或已记录完整 ID 指纹的候选为 **0**。分页上限 50；显式 count/list、稳定总数、重复 ID、空短页及页数上限均 fail closed。
- **7 个已知完整 ID 指纹**在各自已完成的 scope 中均无匹配。组织项仅检查默认池 scope，不得据此推导原租户 scope 中的组织已消失。
- 保留当前无测试标记/已知指纹的对象：用户列表总数 1、应用 1、权限分组 2、默认组织列表 1。未把这些对象认定为测试数据，也未修改它们。
- 仍有 **2 个需要控制台核对的范围**：原租户下来源未知的组织，以及无精确 ID 的认证流函数。另有原租户精确 ID 无法恢复的诊断缺口；这不推翻完整租户列表总数为 0 的事实。

## 前后快照与归属

| 目标 | 首次只读快照 | 最终核查 | 本次删除 |
|---|---|---|---|
| 群组 `hermesacc-d4ecff84407c6617` | 精确 GET 匹配生成 code/name/ownership marker，成员 1；只有候选证据，没有创建 state | 精确 code GET 404；完整群组列表 0 | 否 |
| 用户 `hermesacc-9628c310fcf35671` | 精确 GET、username/nickname 匹配；完整 ID SHA-256 `189f388675a6a05c85cecec5041c9ba887558a4d2477724665d156c6b79f99ea` 与旧候选截断指纹相符，但仍非创建 state | 精确 username GET 404；完整用户列表中没有该完整 ID 指纹 | 否 |
| 上述 group-member 关系 | 完整 group-members 列表发现 1 条，用户完整指纹一致；没有原创建 state | 父群组已 GET 404，不声称单独删除或独立关系读回成功 | 否 |
| 租户 `hermesacc-97f07719b2e15f0c` | 精确候选 GET；apps/users/admins=0、organizations=1；候选 ID 指纹 `dc09fe56a1a8400bf17b36a43edfb69eb78428ccee94857bdf3763ab21b2090a`；authority=none | 未过滤租户列表完整、total=0；无原始 ID，不能补做该 ID 的精确 GET | 否 |
| 该租户中的组织 | scoped list + scoped exact GET 匹配；组织名含上述生成名；ownership marker 不匹配；children=0、递归 members=0；组织 code SHA-256 `9fa54c3ab0e7ed835577131ac44a9e0b44d2c48d51436e75f444ba1132b1cc2a` | 原组织 code/tenantId 原值不可恢复；默认组织 scope 的完整指纹无匹配，但**不是**原租户组织的精确缺失证明 | 否 |
| 认证流函数 `hermesacc-4cba9ad3756d8fa0` | 旧失败执行没有保存 funcId，创建与清理均不确定 | 公布的 API 只有 funcId 精确 GET，没有完整按名称枚举；Pipeline family 即使全部 scene 为空也不能证明它不存在 | 否 |

另外四个本来就已确认缺失、此次再次 GET 404 的目标是两次 expanded group 的 code `hermesacc-69fb9c423513635b`、`hermesacc-c834fa2818addda0`，旧 namespace `hermesacc-a69a5cb1b971aa8e`，以及 public account `hermesacc-802de9a7c2297afd`。加上上表群组和用户，共 6 个精确 404，不重复计数。

**没有恢复任何未知候选的删除 authority。** 名称、marker、时间窗及 discovery 指纹不能替代旧 creating-state identity。原 tenant/group-member/auth-flow run 的 artifact metadata 均为 total_count=0。没有伪造 state、import 或 provider CRUD guard 例外。

## 只读执行与修正记录

| Run | 测试 SHA | 结果与限制 |
|---|---|---|
| [39360447598](https://github.com/hefanxu/terraform-provider-authing/actions/runs/39360447598) | `f721c913d74c5a5321d8d15f764f2c16c8518f9c` | 初始候选、关系及租户组织依赖盘点；user-groups 使用了未公布路径，其错误**不可作为空成员关系证明**。保留原 JSONL，不追改历史输出。 |
| [39361366261](https://github.com/hefanxu/terraform-provider-authing/actions/runs/39361366261) | `00f79fb0e20486cb316590613336b56066f061fc` | 改为官方 `GET /api/v3/get-user-groups`（不发送分页参数），新增 collection 404=unknown 回归测试；Pipeline 全部 10 scene。此时群组/用户/租户已从远端列表消失，未实际触发该用户的 groups 读取。 |
| [39361745993](https://github.com/hefanxu/terraform-provider-authing/actions/runs/39361745993) | `c09214623f65749c5d41b0c3464c56ee6137abc2` | 用户精确 username GET 404、默认组织完整读取；对 tenant/org 的只读管理员审计记录尝试恢复既有指纹对应的诊断 ID。审计记录缺少或重复 request identity，拒绝信任部分数据，未恢复精确 scope。 |
| [39362459089](https://github.com/hefanxu/terraform-provider-authing/actions/runs/39362459089) | `17b2ce6c33a7f7eff1814eecbfa354bed7dc1025` | 最终重复读回并检查完整 ID 指纹，防止可变名称变更隐藏已知目标。6 exact 404、19 完成 scope；审计恢复仍 fail closed。 |

四次均只有 `cleanup-inventory` job 成功，其他 10 个 jobs 跳过；绿色表示只读核查执行成功，不表示所有对象缺失。已将创建/更新/drift 的 `destructive-case` job 显式暂停。复用已在默认分支注册的 manual dispatch 的独立只读 job；没有为了注册新 workflow 修改或合并 main，也没有运行不必要的 destructive cleanup job。

Environment 读回仍为 `authing-sandbox`，用户明确允许的 reviewer=0、自定义分支策略，唯一允许分支 `feat/management-api-coverage`。只查 AK/SK secret 的名称与更新时间，值未读取或输出；两条 secret 的更新时间仍为原配置日期，未显示本次核查期间轮换。未更改 Environment、Hermes approval/yolo、其他 profile 或业务数据。

## 历史覆盖、计数和文件

- 以 workflow **path/ID** 而非可变显示名汇总，最终分页声明总数 **52**，去重后也是 52：**48 次历史执行 + 本次 4 次只读核查**。初次按旧显示名筛选会漏掉早期 1 次只读执行，已补齐。
- 历史控制日志缺失 1 次（37869612313，job 没有 step 记录），另 2 次早期日志 step metadata 不全。未把离线 mock 中的 `sandbox case ... passed` 当作真实 API 写入或 live pass。
- `inventory.csv` 是 **30 条追踪记录，不是 30 个当前对象**：20 个有生成名称的目标、1 个 scoped organization、1 条关系、8 个没有输出精确 identity 的历史成功 lifecycle。包含 3 条在 preflight 停止、未写入的目标。
- 8 个 identity 未输出的历史成功 trace 沿用原 exact absence/cleanup 证据；没有假装恢复它们的 ID 或进行新的精确 GET。其余 8 个旧 application 名称当前完整列表没有 marker 匹配，也不被冒充为本次删除或新的 exact-ID 404。
- `run-index.jsonl`：全部 52 次 workflow 的受控元数据。
- `live-history.jsonl`：48 次历史执行的 live-step 受控诊断，不含 mock 输出、raw API body、Authing raw ID 或 PII。
- `inventory-<run>.jsonl`：每批只读结果的 append-only 受控快照；保留首轮失败诊断和后续修正事实。
- `inventory.csv`：逐目标 provenance、ownership、authority、dependencies、before/after、run URL。
- `resource-scopes.csv`：最终 19 个完成资源 scope，以及 2 个无法信任的 audit inventory；audit 的 count/total 为 -1，不是 0。
- `summary.json`：程序去重聚合后的准确计数与缺口。
- `read-contract.json`：官方 Management OpenAPI 的相关路径、参数与 schema 引用，不含响应示例或凭据；用于离线请求 allowlist 契约回归检查。

代码验证：Go test/race/vet/build、真实 Terraform protocol smoke（不执行 init/plan/apply）、Python sanitizer/dispatch tests、actionlint 和 diff check。最终提交的 current-head CI 应另行读回后才可宣称其通过。

## 需要用户提供的具体信息

无需再次泛泛授权“继续”；需要的是下列两个目标的确定事实：

1. **认证流函数**：请在控制台按 `hermesacc-4cba9ad3756d8fa0` 核对。如果仍存在，提供它的 **funcId** 并确认这是可删除测试对象；如果已由你或其他管理员删除，提供该目标的控制台缺失确认。不要提供 AK/SK、token 或密码。
2. **原租户组织**：请确认租户 `hermesacc-97f07719b2e15f0c` 及其组织是否已被外部删除/级联删除。原组织名称含该测试名，组织 code 的完整 SHA-256 为 `9fa54c3ab0e7ed835577131ac44a9e0b44d2c48d51436e75f444ba1132b1cc2a`。如果组织仍存在，请提供精确 **organizationCode 与所属 tenantId**，并明确确认它是可删除测试组织；会先重新读完整依赖，不能根据同名或默认组织猜测级联删除。

在这些信息补齐前，不再创建测试对象、不猜测接口、不删除无归属或非测试依赖，也不宣称全清。
