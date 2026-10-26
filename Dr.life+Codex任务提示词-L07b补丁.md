# L-07b：对账加固补丁

**用法：** 整块复制代码块内容，或用本地文件指令让 Codex 读取。基线 `origin/main` 最新提交。这是对已合并 L-07 的评审补丁。

---

## L-07b 对账加固（日期保护、冻结补全、卡死 running、抽检、收款账户解密留痕）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】L-07b 对账加固（日期保护、冻结补全、卡死 running、抽检、收款账户解密留痕）
【分支】task/L-07b-recon-hardening
【依赖】L-07、L-12 已合并
【先阅读】
1. backend/internal/ledger/recon.go、freeze.go、withdrawal.go（DecryptWithdrawalPayee）、迁移 00010_ledger_reconciliation.sql；Dr.life+数据库表结构与账务详细设计.md 第七章
2. Dr.life+API安全协议与容量及队列设计.md 6.6（定时任务）；backend/internal/ledger/ 的 API 表面白名单守卫测试（L-12）
【范围——要做】
1. **日期保护（最重要）**：SnapshotDaily、RunDailyReconciliation 只允许处理『已结束的日期』（按 reconciliationLocation 计算，day < 当前时区的今天；时钟用已有可注入的 s.now()）。对今天或未来日期返回新错误 ErrReconciliationDayOpen，不写快照、不写 recon_run、不冻结。ledgerctl 的 --day 默认改为『昨天』，对未结束日期报错并以退出码 2 结束。测试：今天/明天被拒且无副作用；注入时钟越过日界后可处理；复现场景（先对今天做快照→再入账→再对账）不再产生 SNAPSHOT_DRIFT 或全局冻结。抽检不受影响（只比对账户级缓存，不写 snapshot_daily）。
2. **冻结规则补全**：PENDING_PAYOUT 不一致 → 写 global 冻结；HOLD_FROZEN 不一致 → 对该冻结账户对应的 WALLET_AVAIL（同 owner_account）写 account 级冻结。更新设计文档第七章冻结规则表。测试：人为篡改使两项各自不一致，断言对应冻结生效、ConfirmPayout / RequestWithdrawal 被拒，CancelWithdrawal / FailWithdrawal 仍可用。
3. **卡死的 running 记录**：startReconciliationRun 遇到状态为 running 且 started_at 早于可配置超时（默认 30 分钟）的记录时，标记为 failed（summary 记 STALE_RUN；新迁移放宽 protect_recon_run 仅允许 running→failed 的超时处理，已合并迁移不得修改）并创建新的 attempt 继续，同时发 outbox 事件 ledger.recon.stale_run；未超时的 running 记录返回 ErrReconciliationRunning（不要当作已完成）。用注入时钟测试。
4. **抽检加固**：抽检不再为每次抽检写 recon_run 行，只在发现不一致时才写 run 与 issue（kind='spot'）并按账户级冻结；并发上限（默认同时最多 4 个抽检 goroutine，超出直接丢弃并计数）；服务关闭时可取消/等待在途抽检。测试：开启抽检高并发入账时 recon_run 行数不增长、goroutine 数有上限、不一致被检出并冻结账户。
5. **收款账户解密留痕**：DecryptWithdrawalPayee 增加参数 ActorID 与 Purpose（非空，如 bank_payout），每次调用同事务写 outbox 事件 ledger.withdrawal.payee_accessed（payload 含提现 public_id、actor、purpose、时间，不含账户内容）；同步更新 API 表面白名单守卫测试中的方法签名（不要放宽守卫）。测试：事件恰好一条且无账户内容。
【验收标准】
① 对今天/未来日期的对账与快照被拒且无副作用，复现场景不再误冻结
② PENDING_PAYOUT 与 HOLD_FROZEN 不一致时分别触发 global / account 冻结，退回用户的流程不受影响
③ 卡死的 running 记录被标记 failed 并重新对账；抽检不再产生无限 recon_run 行且并发有界
④ 收款账户每次解密都有审计事件且不含账户内容
⑤ go vet、-race、全量集成测试、ci-gate 全绿；已合并迁移未被修改
【待确认（不要自己拍板，PR 里列出）】
- 卡死阈值默认 30 分钟
- 抽检并发上限默认 4
【红线】不改 00001–00012 已合并迁移（新增迁移，先看 backend/migrations/README.md 取序号）；不放宽任何触发器与权限；事件里不含收款账户或个人信息。
```

---
