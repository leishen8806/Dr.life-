# L-13：资金类 HTTP 接口层

**用法：** 整块复制代码块内容粘贴给 Codex，独立 worktree 与分支。**建议在 U-04/U-06/U-07/U-08 的提现相关页面联调之前完成**；L-09 可并行。
**与其他任务的关系：**依赖 L-05/L-07b/L-12（账务层）、S-01（step-up 与幂等）、A-06（后台 step-up 签发与 RBAC）；用户侧 step-up 依赖手机号验证（建议新增 A-11，本任务用 fake）。

---

## L-13 资金类 HTTP 接口层（钱包、提现、后台出款复核、出款冻结）

> 原任务表遗漏：账务层（L-05）只有服务方法，没有对外 HTTP 接口；U-04（买家提现页）、U-07（司机提现页）、U-06（商家资金与提现）、U-08（后台出款复核）都假定这些接口存在。本任务补上，并把 BULKHEAD/幂等的『资金类路由前缀』与真实路径对齐。

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】L-13 资金类 HTTP 接口层（钱包、提现、后台出款复核、出款冻结）
【分支】task/L-13-funds-http-api
【依赖】L-05、L-07/L-07b、L-12 已合并；A-02、A-03（会话）、S-01（step-up 骨架、幂等绑定）、A-06（后台 RBAC 与 step-up 签发）、A-05（商家 fin 权限）、Q-01（审计，可先 fake）；L-09 可并行（分页与导出机制复用）
【先阅读】
1. backend/internal/ledger/doc.go 与 withdrawal.go、freeze.go、recon.go（RequestWithdrawal / StartReview / ApproveReview / ConfirmPayout / FailWithdrawal / CancelWithdrawal / ListOverdueWithdrawals / ListLargePending / DecryptWithdrawalPayee / ReleasePayoutFreeze 的前置条件、错误类型、幂等规则）；backend/internal/ledger 的 API 表面白名单守卫测试（L-12）
2. Dr.life+API安全协议与容量及队列设计.md 第三章 3.4（step-up）、第四章（幂等绑定请求内容）、5.3（critical 类）；backend/internal/platform/gateway/（RequireStepUp、幂等存储、金融路由前缀配置）、platform/overload/（BULKHEAD_CRITICAL_PREFIXES）
3. Dr.life+平台后台权限与审计规范.md（提现出款、大额两人复核、附加控制、审计 #3/#16/#17）；Dr.life+交互文档.md 买家/商家/司机提现页、平台『提现出款』流程；Dr.life+技术方案与系统边界.md 第六章『提现』状态机
4. Dr.life+开发任务拆分与验收.md：L-05、L-09、A-06、U-04、U-06、U-07、U-08；backend/api/openapi.yaml、backend/internal/api/
【范围——要做】
1. **接口放在 api 装配层（`backend/internal/api/funds*.go` 或独立 `internal/funds` HTTP 包），只调用 ledger 已导出的业务方法与只读方法**；不得绕过 L-12 收口，不新增 ledger 通用入账入口。确实需要的**只读**新方法（钱包汇总、按账号列出自己的提现、后台提现列表过滤）放在 ledger 包里，**同步更新 API 表面白名单并在 PR 说明理由**。OpenAPI 先行，再生成。
2. **用户侧（buyer / merchant / driver 三端，路径 `/v1/{end}/wallet/*`，同一处理函数薄封装，角色与路径一一对应）：**①`GET /wallet`：可用、冻结、待出款三项余额（首次访问时按需 `EnsureWallet`；商家账号取令牌里的商户范围，merchant_id 由 iam 的只读接口提供，**不信请求参数**）；②`POST /wallet/withdrawals`：必须带 `Idempotency-Key`（映射为 ledger 的 `RequestKey`，同键同内容返回原提现单，同键不同内容 422）与 **step-up 令牌（scope=withdraw）**；请求体：金额（e4 整数，>0）、收款账户（字符串，长度/字符集校验，不记日志）；网关验证 step-up 后才设置 `SecondFactorVerified`（**不得由请求体字段传入**）；账号状态检查通过 `AccountGuard`（iam 提供实现：冻结/停用拒绝，A-04 状态）；商家端要求 `fin` 模块 edit 权限（A-05 的 `RequireMerchantPerm`），**大额提现是否需要老板二次确认列为待确认，默认：店员（finance 角色）可发起，系统不加限制**；响应只含提现单 public_id、金额、状态、收款账户末 4 位、requires_review、创建时间；③`GET /wallet/withdrawals`（只列本人，keyset 分页）、`GET /wallet/withdrawals/{id}`（别人的返回 404，含状态时间线，**不含 maker/checker 管理员身份**）；④`POST /wallet/withdrawals/{id}/cancel`：**用户只能在 requested / review 状态撤销**（approved 之后可能已提交银行，只能由后台操作，避免『撤销与银行付款并发』的双重损失）；幂等。
3. **后台侧（`/v1/admin/withdrawals*`，RBAC `wd` 模块；IP 白名单、双因素、更严限流沿用 S-01）：**①`GET /v1/admin/withdrawals`：筛选 status / 是否大额 / 是否超时（`ListOverdueWithdrawals`，默认 24 小时）/ 金额范围，keyset 分页，**显示待出款时长，超时标『关注』**，收款账户只显示末 4 位；②`POST .../{id}/start-review`（maker，`wd` edit）；③`POST .../{id}/approve`（checker，`wd` edit + **step-up scope=withdraw_review**；**checker ≠ maker 且 ≠ 申请人**由 ledger 与数据库双重保证，HTTP 层把 `ErrSeparationDuty` 映射为 403 且提示『需要另一位有权限的管理员复核』）；④`POST .../{id}/confirm-payout`（`wd` edit + **step-up scope=withdraw_payout**，body：bank_ref，必填；金额与钱包取自提现行，**接口不接受金额参数**）；⑤`POST .../{id}/fail`、`.../cancel`（必填原因，`wd` edit，cancel/fail 的状态约束按 ledger）；⑥`GET .../{id}/payee`：**取收款账户明文（仅用于出款），需要 `wd` edit + `sens` 权限 + step-up scope=withdraw_payee，每次调用写审计事件（ledger.DecryptWithdrawalPayee 的 Actor/Purpose 参数按当前主干签名传入）**，响应 `Cache-Control: no-store`，日志不含内容。所有写操作同事务/可恢复地写审计（Q-01 的 audit.Writer，未合并用 fake），审计 detail 不含收款账户。
4. **出款冻结（`/v1/admin/ledger/freezes`，RBAC `fund` 模块）：**`GET`（未解除列表，含触发的对账运行摘要）、`POST /{id}/release`（step-up scope=freeze_release，必填原因；**解除人 ≠ 触发该冻结的对账发起人**由 ledger 保证，映射 403）；解除后接口响应提示『需重新运行对账并通过』。冻结生效时，用户提现与出款确认返回**统一的『暂时无法办理，请稍后再试』**错误码（`ErrPayoutFrozen`），**不向用户暴露冻结原因**，后台看得到原因。
5. **用户侧 step-up 签发（二次验证）：**网关的 step-up 令牌目前只有后台（A-06，TOTP）能签发。Mini App 用户需要：`POST /v1/{end}/step-up/challenge`（scope、动作摘要，**限频：同账号每小时 ≤ 5 次、每分钟 ≤ 1 次**）→ 经 `SmsSender` 接口（N-01 的接口位；本任务提供 fake 与接口，**不实现真实短信**）向**已验证的手机号**发送 6 位一次性验证码（有效 5 分钟、最多 5 次尝试、哈希存储、用后即焚）；`POST .../step-up/verify` → 签发 step-up 令牌（5 分钟、单次、绑定 scope 与账号，`jti` 入 Redis 已用集合；复用 S-01 的 `StepUpVerifier` 签发端）。**person.phone_verified_at 为空时返回『需要先绑定手机号』的专用错误码**——**手机号绑定与验证的流程不在本任务（原任务表缺失，建议新增 A-11），PR 里写明；测试用 fake 手机号状态**。
6. **路径与保护对齐（必须做）：**把上述资金路径加入 `BULKHEAD_CRITICAL_PREFIXES` 的默认值（`/v1/buyer/wallet/withdraw*`、`/v1/merchant/wallet/withdraw*`、`/v1/driver/wallet/withdraw*`、`/v1/admin/withdrawals`、`/v1/admin/ledger/freezes` 及现有项）与 S-01 的 `IDEMPOTENCY_FINANCIAL_PREFIXES` 默认值（两处保持一致，加一个**一致性测试**断言两份清单覆盖同一批路径）；step-up 挑战/验证接口归 `auth` 类并限流；资金读取（余额、列表）归 read 类。
7. **错误映射（统一，三语文案走前端词条，后端只返回稳定错误码与最少的参数）：**ErrInsufficientFunds → `ERR_INSUFFICIENT_FUNDS`（可带『差额』仅当为本人资金；**不返回他人信息**）；ErrPayoutFrozen → 统一暂缓码；ErrSeparationDuty → 403；ErrWithdrawalState → 409；ErrAccountOwnerMismatch/他人资源 → 404；账号冻结/停用 → 专用账号状态码；ErrSecondFactorRequired → 『需要二次验证』码（前端据此拉起验证）；幂等冲突 → 422；未知错误 → 统一 500，**内部细节不外泄**。
8. 指标与日志：提现申请/复核/出款/失败/撤销计数（`WithdrawalMetrics` 接口已在 ledger，接 observability），step-up 挑战/验证成功与失败计数；**日志、指标、错误、审计 detail 一律不含收款账户全文、验证码、令牌**（测试里捕获日志做子串断言）。
9. **本任务不包含：**充值与银行对账接口（L-08，依赖 P0-6）、流水查询与导出（L-09）、手机号绑定（建议 A-11）、真实短信发送。
【验收标准】
① 用户从申请到出款的完整 HTTP 流程跑通：小额自动 approved → 后台 confirm-payout；大额 → start-review → approve（另一位管理员）→ confirm-payout；每一步的权限、step-up、幂等、状态机都有集成测试（真实 PostgreSQL，Redis 用 testcontainers）
② 越权矩阵：买家令牌访问 merchant/driver/admin 路径 403；用户只能看到自己的提现（别人的 404）；无 `fin` 权限的商家员工 403；无 `wd` / `sens` / `fund` 权限的管理员 403；缺 step-up 或 scope 不符/已用过/过期 → 需要二次验证码；checker = maker 或 = 申请人 → 403
③ 并发与幂等：50 个并发请求同一 Idempotency-Key 只产生一张提现单和一次扣款；同键不同体 422；确认出款并发 100 次只出款一次；资金路由在 Redis 不可用时 503（不无保护执行）
④ 冻结生效时提现与出款被拒且对用户只显示统一暂缓错误；解除冻结需要 step-up 与不同的解除人；撤销规则：用户只能撤 requested/review，approved 之后只有后台能撤
⑤ 收款账户明文只通过 `/payee`（权限 + step-up + 审计事件）获取；所有响应、日志、指标、审计 detail 不含收款账户全文（子串断言）；`Cache-Control: no-store`
⑥ 路径清单一致性测试通过（critical 前缀与幂等金融前缀覆盖同一批资金路径）；OpenAPI 无差异；ledger API 表面白名单更新有说明
⑦ go vet、-race、全量集成测试、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 商家店员（finance 角色）发起提现是否需要老板二次确认（PRD 写『待定』；默认不加，但留配置开关）
- 用户二次验证方式：短信验证码（本任务）还是支付密码（需要另做设置与找回流程）；手机号绑定任务（A-11）排期
- 提现最小/最大金额与每日次数限额（默认：无最小、单笔上限与每日上限走配置，先给保守默认并在 PR 里列出）
- approved 之后用户申请撤销的处理（本任务一律只允许后台）
【红线】不得新增通用入账入口或绕过 L-12；step-up 标记不得由请求体设置；收款账户全文、验证码、令牌不得出现在日志、指标、错误、审计里；不得向用户暴露冻结原因与管理员身份；金额不得浮点。
```

---

## 评审要点（给我自己用）

| 任务 | 重点 |
|---|---|
| L-13 | step-up 单次性与 scope 绑定；SecondFactorVerified 的来源；用户只能撤 requested/review；/payee 的权限与审计；资金路径是否同时进入 critical 前缀与幂等金融前缀；越权 404 与 403 的区分；冻结对用户的信息泄露；并发幂等 |
