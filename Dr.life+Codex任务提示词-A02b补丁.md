# A-02b：登录加固补丁

**用法：** 整块复制代码块内容，或用本地文件指令让 Codex 读取。基线 `origin/main` 最新提交。这是对已合并 A-02 的评审补丁，来源是对 A-02 评审分支（CMP-112 / 124 / 25 / 146）逐条对照 main 的核对结论。

---

## A-02b 登录加固（商户老板账号复用与歧义、登录审计表只追加、审计写失败可见）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】A-02b 登录加固补丁（商户老板账号复用与歧义、登录审计表只追加、审计写失败可见）
【分支】task/A-02b-login-hardening
【依赖】A-02 已合并（PR #30）、A-03 已合并
【先阅读】
1. backend/internal/iam/login.go（upsertAccount、loginValidated、recordFailure、roleForEnd）、backend/migrations/00003_iam_tables.sql（ux_account_person_role_merchant）、00011_iam_telegram_login.sql（ck_login_event_result）、00002_roles.sql（iam_rw 权限）
2. 本地评审分支 CMP-112-a-02-iam 的提交 14e8b0d、03cc499、d737059、4e7a6e3，以及分支 CMP-146-a-02 上的 review-cmp146.md / review-cmp146-summary.md（独立复审的探针与结论）。**这些分支只作参考，不要 cherry-pick**：main 上的登录实现（Service.Login）已重写，代码不兼容。
【背景（已对照 main 核对）】
- 评审分支里 7 个提交中，大部分已被 main 的 A-02 覆盖；下面 3 项在 main 上**仍然存在**，需要补。
- 已核对**不需要**补的：47e27b7（验证后销毁源 Bot 令牌）——main 的 DBPoolSource 返回 cloneBots 深拷贝，PoolVerifier 销毁的是副本，不会清掉缓存，不要移植。
【范围——要做】
1. **商户老板账号复用与歧义（安全/功能缺陷）**：约束是 `UNIQUE NULLS NOT DISTINCT (person_id, role, merchant_id)`。老板被绑定商户后（merchant_id 非空）再登录，main 的 upsertAccount 会插入一条 merchant_id 为空的**第二个** merchant_owner 账号并返回它——登录成为一个没有商户的孤儿账号。改为：先按 (person_id, role) 查账号（LIMIT 2，不限制 merchant_id）：恰好 1 条→复用；0 条→按现有逻辑创建；≥2 条→歧义，**拒绝登录**（返回 5xx 类错误，不选择其中任何一个），并写审计。测试（真实 PostgreSQL）：已绑定商户的老板登录得到原账号且 person 下 merchant_owner 数量仍为 1；再给同一 person 建第二家商户的 owner 账号后登录被拒；buyer / driver 首次登录行为不变，driver 的 kyc_status 仍为 pending。
2. **歧义登录审计，且不得嵌套取连接**：新增迁移（取序号见 migrations/README.md，当前预期 00020）把 ck_login_event_result 增加 'ambiguous_account'（Down 对称还原；Down 前先把已有 ambiguous_account 行改为 internal_error，说明理由）。审计必须**先结束外层事务（Rollback）再用新事务写**，不要在外层事务未结束时再向连接池要第二条连接——评审实测：MaxConns 较小时并发歧义登录会整池互锁到超时且审计一条都没有。测试：MaxConns=1 与 =4、4 路并发歧义登录均在合理时间内返回，且 ambiguous_account 事件数 = 请求数。
3. **recordFailure 不得吞掉错误**：BeginTx / insertLoginEvent / Commit 失败至少写 error 日志（不含 initData、令牌）并增加一个失败计数指标（审计写失败本身就是安全事件）；不改变对客户端的响应。测试：注入失败的 DB，断言计数指标增加、响应不变。
4. **登录审计表只追加（评审 S1）**：main 上 iam_rw 对 iam.login_event 仍有 UPDATE、DELETE、TRUNCATE、TRIGGER、REFERENCES 权限。新增迁移（可与第 2 项同一个）：`REVOKE UPDATE, DELETE, TRUNCATE, TRIGGER, REFERENCES ON iam.login_event FROM iam_rw`，Down 对称 GRANT。**TRIGGER 必须一并撤销**：评审实测 iam_rw 可在 pg_temp 建 `RETURN NULL` 的触发器函数并挂为 BEFORE INSERT，之后所有登录审计 INSERT 被静默吞掉且不报错，跨会话生效。核对并在 PR 里写明：仓库内没有任何代码对 login_event 做 UPDATE / DELETE / TRUNCATE（已核对为无）；若将来需要清理，必须走单独的、有评审的维护角色。测试：用 iam_rw 登录角色，UPDATE / DELETE / TRUNCATE / 挂触发器均得到 42501；INSERT 与 SELECT 仍可用；Down 再 Up 后 REVOKE 仍生效。
【验收标准】
① 已绑定商户的老板登录得到原账号，不再生成孤儿账号；歧义时拒绝登录且有且仅有一条 ambiguous_account 事件
② 小连接池下并发歧义登录不互锁
③ 审计写失败有日志与指标
④ iam_rw 对 login_event 只剩 INSERT、SELECT（含 TRIGGER 被撤销的专项测试）
⑤ 已合并迁移未被修改；go vet、-race、全量集成测试通过，结果写进 PR 描述
【待确认（不要自己拍板，PR 里列出）】
- 同一 person 持有多家商户的 owner 账号是否合法（F1，董事会尚未裁定）：本补丁保守地拒绝登录；若裁定为合法，需要另行设计商户选择流程。
- person.status 是否参与登录放行（本补丁不改）。
【红线】不得在歧义时任选其中一个账号放行；不得把 initData、Bot 令牌、完整手机号写进日志或审计；不得放宽 L-12 / 现有守卫测试。
```
