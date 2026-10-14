# Codex 任务提示词：L-07（每日对账与快照）、L-12（账务对外接口收口）、A-02（Telegram 登录）

**用法：** 整块复制代码块内容粘贴给 Codex。基线 `origin/main` 最新提交（L-01～L-06 已合并，迁移最大序号 00009）。
**顺序与并行：**
- **L-07 与 L-12 都改 `backend/internal/ledger/`，请先 L-07、后 L-12**（L-12 要把通用 `Post()` 收口，L-07 合并后再动，避免大面积冲突）。
- **A-02 在 `backend/internal/iam/`，与账务无重叠，可以和 L-07 同时开。**
- **A-02 有一个人工闸门：** 代码可以先写、先评审，但**必须等你完成 P0-1 的真机验收（`poc/telegram-initdata/RESULT.md` 里补上真实 initData 的证据）之后才能合并**。验收步骤见 `poc/telegram-initdata/README.md`：六个测试 Bot、真机 Mini App、`/revoke` 主 Bot。
合并前确认 `ci-gate` 为绿。已合并的迁移（00001–00009）一律不得修改，变更只能新增迁移（L-07 用 00010，L-12 尽量不需要迁移，A-02 如需用 00011）。

---

## L-07 余额缓存校验与每日快照

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。L-01～L-06 已合并。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】L-07 余额缓存校验与每日快照（账务最后一块安全网）
【分支】task/L-07-ledger-reconciliation，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（账务 1–7 条，尤其第 7 条"对账恒等式每天验证"；规则 8、12）
2. Dr.life+数据库表结构与账务详细设计.md：第三章（科目与恒等式）、第七章（对账与审计：三层对账、snapshot_daily、审计 18 项对应）、第十一章
3. Dr.life+平台后台权限与审计规范.md：审计项 #1、#2、#3、#16
4. Dr.life+开发任务拆分与验收.md：L-07、L-08 两行
5. backend/internal/ledger/ 全部（尤其 hold.go 的 VerifyHoldsMatchFrozen、withdrawal.go 的 VerifyPendingPayoutMatchesWithdrawals）；migrations 00004（snapshot_daily、balance 同步触发器）、00005–00009；backend/internal/platform/observability/business.go

【背景】
`ledger.balance` 是缓存，真正的账是分录。必须有一个独立的、不信任缓存的核对：每天全量重算，发现不一致就报警并冻结相关出款。本任务**不做银行对账文件导入（L-08）**，只做"内部对账"（第七章三层对账的第①层）。

【文件归属】backend/internal/ledger/（recon.go、freeze.go 等与测试）、迁移 00010_ledger_reconciliation.sql、cmd/ledgerctl/（命令行入口，见下）、observability/business.go 的埋点补充、更新《数据库表结构与账务详细设计》第七章与新增表说明。不要写 HTTP 接口、不要改 iam、不要做银行文件解析。

【迁移 00010】
1. `ledger.recon_run`：id、day date、kind（'daily'|'spot'）、started_at、finished_at、status（'running'|'ok'|'failed'）、summary jsonb（各项检查结果与不一致数量，**不含账户余额明细之外的敏感数据**）；`unique (day, kind) where kind='daily' and status in ('ok','failed')`（同一天只能有一次已完成的每日对账；重跑用显式参数，见下）。
2. `ledger.recon_issue`：id、run_id、check_code（见下）、account_id（可空）、txn_id（可空）、expected bigint、actual bigint、detail text、created_at。
3. `ledger.payout_freeze`：id、scope（'global'|'account'）、account_id（scope='account' 时必填）、reason text 非空、run_id、created_at、released_at、released_by bigint、release_reason text；CHECK：released_at 与 released_by、release_reason 同时为空或同时非空；部分唯一索引：同一范围同时只能有一条未解除记录。
4. `ledger.snapshot_daily` 追加**只增不改**保护：BEFORE UPDATE/DELETE 触发器拒绝（SQLSTATE 55000），TRUNCATE 拒绝；`ledger_rw` 对其仅 SELECT/INSERT；同样保护 recon_run 的已完成记录（完成后不可改）与 recon_issue（只增）。freeze 表只允许更新 released_* 三列且只能由未解除→解除一次。
5. 迁移可逆（Down），并同步设计文档。

【核对内容（每项一个 check_code，全部在同一个只读 REPEATABLE READ 事务里完成，保证看到一致的快照）】
- `BALANCE_CACHE`：对每个账户，用分录重算余额（贷方正常账户：贷方合计 − 借方合计；借方正常账户 BANK_DEDICATED 相反），与 `ledger.balance.balance` 比较；另检查 version 不倒退（若有上次快照可比较，可选）、`allow_negative` 与账户定义一致、非 allow_negative 账户余额 ≥ 0。
- `TXN_BALANCED`：全部交易借方合计 = 贷方合计 = txn.amount，且分录数 ≥ 2（兜底检查数据库触发器，万一被绕过）。
- `IDENTITY`：恒等式——所有非 BANK_DEDICATED 账户（含 SUSPENSE，可为负）的余额合计 = BANK_DEDICATED 余额。
- `HOLD_FROZEN`：调用/复用 VerifyHoldsMatchFrozen 的逻辑（冻结账户余额 = 未结清 hold 之和）。
- `PENDING_PAYOUT`：复用 VerifyPendingPayoutMatchesWithdrawals 的逻辑。
- `REVERSAL`：每笔 reversal 都指向存在的原交易，且分录互为镜像（兜底 00009 的触发器）。
- `ORPHAN`：posting 无对应 txn、txn 无分录（不应存在）。
实现要求：SQL 聚合一次扫描，不得逐账户 N+1 查询；为大表预留分块/游标的接口，但第一版全量即可，并在 PR 里记录 1 万账户、100 万分录规模下的耗时。

【每日快照】
- `SnapshotDaily(ctx, day)`：对截止 `day` 当日结束（`txn.created_at < day+1 天`，按 UTC 或配置的时区，写明并可配置，默认 Asia/Phnom_Penh）的分录重算每个账户余额，写入 snapshot_daily(day, account_id, balance)。**幂等**：同一天重复运行不改已有快照（ON CONFLICT DO NOTHING），并在已有快照与重算不一致时报 `SNAPSHOT_DRIFT`（说明历史被改动，严重）。
- 每日任务顺序：先 SnapshotDaily，再各项核对；任何一项不一致 → run.status='failed'，写 recon_issue；全部通过 → 'ok'。

【失败处理：报警 + 冻结相关出款】
- 记录 `recon_issue`，写 outbox 事件 `ledger.recon.failed`（payload：run id、day、各 check_code 的不一致数量、前若干个账户 id；**不含用户姓名、收款账户、手机号**）。
- 在 observability.Business 增加 `ReconRun(ok bool)`、`ReconMismatch(checkCode string, n int)`（定义在 ledger 里的接口注入，不要引入循环依赖）。
- **冻结规则**：
  - 账户级不一致（BALANCE_CACHE 针对某账户）→ 写 scope='account' 的 payout_freeze：该账户不得发起提现、出款确认；
  - 全局不一致（TXN_BALANCED、IDENTITY、SNAPSHOT_DRIFT、REVERSAL、ORPHAN）→ 写 scope='global'：`ConfirmPayout`、`Sweep`、`CarrierPay`、`RequestWithdrawal` 一律拒绝（ErrPayoutFrozen）。
  - 冻结检查放在上述方法**同一事务里**读取（在锁住相关 balance 行之后）；没有冻结记录才放行。`CancelWithdrawal`/`FailWithdrawal`（把钱退回用户）**不受冻结影响**。
  - 解除：`ReleasePayoutFreeze(ctx, freezeID, ReleasedBy, Reason)`，reason 非空，写 released_*；**解除人必须不同于触发该冻结的对账运行的发起人**（若由系统自动运行则不限制），并写 outbox 事件 `ledger.payout_freeze.released`。解除后必须重新运行对账并通过，才能认为恢复（本任务只提供接口和事件，审批流在后台任务里）。
- 重跑：`RunDailyReconciliation(ctx, day, opts{Force bool})`：默认同一天已完成则返回已有结果；Force 创建新 run（记录原因）。

【抽检（每次入账）】
`Options.SpotCheckEvery int`（默认 0 = 关闭）：每 N 笔成功入账后，异步（不阻塞入账、失败不影响入账）对本笔涉及账户做一次 BALANCE_CACHE 抽检，不一致则按"账户级"处理。提供测试证明开启后不影响入账结果，并记录开启前后的基准耗时。

【命令行入口】
`backend/cmd/ledgerctl`：子命令 `recon --day YYYY-MM-DD [--force --reason "..."]`、`snapshot --day`、`freeze list`、`freeze release --id --by --reason`。只做参数解析并调用 ledger 包；数据库连接用现有配置；输出不含敏感信息；退出码：0 通过、1 失败、2 参数错误。（定时调度接入 asynq 放到后续运维任务，本任务只要命令可被调度器调用。）

【测试（真实 PostgreSQL，testcontainers，-race）】
1. **人为篡改 balance 被检出**：用管理员 SQL 改某账户 balance（+1、−1、置为负数、改 version），BALANCE_CACHE 对应账户被列出，expected/actual 正确，写入 account 级冻结，该账户的 RequestWithdrawal / ConfirmPayout 被拒，其他账户不受影响。
2. **恒等式**：人为让银行账户 balance 偏离，IDENTITY 失败并触发 global 冻结；global 冻结时 ConfirmPayout/Sweep/CarrierPay/RequestWithdrawal 被拒，CancelWithdrawal/FailWithdrawal 仍可用；解除冻结（符合规则的解除人）后恢复。
3. **绕过触发器的脏数据**：测试里用超级用户临时禁用触发器，插入借贷不平的交易、孤立分录、不对称的冲销，各自被 TXN_BALANCED / ORPHAN / REVERSAL 检出（测试结束要恢复触发器并清理）。
4. **快照**：同一天重复运行不改变快照；人为篡改历史分录造成与既有快照不一致 → SNAPSHOT_DRIFT；快照表 UPDATE/DELETE/TRUNCATE 被拒（管理员与 app_ledger 两种身份）。
5. **一致快照**：在对账运行期间并发执行大量入账（100 路），对账结果不出现假阳性（REPEATABLE READ 一致性）。
6. **正常场景无误报**：在 L-03～L-06 的随机业务序列（含冲销、提现、冻结）之后运行全量对账，必须全部通过。
7. **事件与指标**：失败时恰好一条 `ledger.recon.failed`，payload 不含敏感字段；指标埋点被调用。
8. **抽检**：开启后每 N 笔触发；抽检失败不影响入账；关闭时零开销（基准）。
9. **命令行**：recon/snapshot/freeze 子命令的集成测试（退出码、幂等、Force 需要 reason）。
10. 迁移 Down/Up 往返；用 app_ledger 身份运行对账与冻结检查，证明权限够用（对账只需读 + 写 recon_*、snapshot_daily、payout_freeze）。

【验收标准】
① 人为篡改 balance 能被检出并冻结相关出款；② 恒等式每日验证；③ 失败产生"异常"事件并冻结相关出款（账户级与全局级），退回用户的流程不受影响；④ 快照只增不改、历史被改动能检出；⑤ 对账期间并发入账无假阳性；⑥ 全部核对项在随机业务序列后通过；⑦ go vet、-race、集成测试、ci-gate 全绿；没有 float、没有 HTTP 接口。

【待确认（不要自己拍板）】
- 解除全局冻结是否需要**两个人**（现在只要求"解除人 ≠ 发起对账的人"）：先按上面实现，PR 里列为待确认；
- 快照的日界线时区（默认 Asia/Phnom_Penh）；
- 对账不一致后"通知谁"（财务、审计）由通知模块决定，本任务只发事件。

【红线】不改 00001–00009；不放宽触发器与权限；不在日志/事件里放收款账户、手机号；不用 float；对账读取不得持有会阻塞入账的长锁（只读快照事务，不要 SELECT FOR UPDATE 账务表）。

【交付】PR 描述：核对项清单与 SQL 思路、冻结规则表、规模耗时数据、测试对照、待确认事项、对设计文档的改动。
```

---

## L-12 账务对外接口收口（把通用 `Post()` 变成包内私有）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。L-07 已合并（若未合并先等合并）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】L-12 账务对外接口收口
【分支】task/L-12-ledger-api-surface，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（账务规则 1（只有 ledger 模块能改余额）、规则 8）
2. Dr.life+数据库表结构与账务详细设计.md：第五章（"入账流程 Post(ctx, req)"）、第六章 6.18–6.19、第十一章
3. Dr.life+开发任务拆分与验收.md：L-03、L-11、L-12（本任务由评审提出，若任务表没有 L-12 请在 PR 里一并补上一行）
4. backend/internal/ledger/ 全部

【背景】
设计文档说"其他模块只能调用 Post(...)"，但那是在没有业务封装的时候。现在 L-03～L-06 已经把每类业务做成强类型、带校验、带状态机的方法。**通用 `Post()` 仍然是导出的：任何模块都可以用任意类型、任意账户、任意金额记账，等于绕过了全部业务规则、状态机和职责分离。** 现在还没有任何其他模块调用 ledger，收口成本最低。

【文件归属】backend/internal/ledger/（只重构、不改行为）；更新《数据库表结构与账务详细设计》第五章措辞（"唯一入口"改为"业务方法是唯一入口，通用 post 为包内私有"）与 AGENTS.md 里相关表述；可新增 `export_test.go`。不要改迁移、不要改行为、不要新增 HTTP 接口。

【范围——要做】
1. **收口**：把导出的 `Post()` 改为包内私有（`post`/`postRaw`，避免与已有内部 `post` 重名，请合理命名）；`PostRequest`、`Posting` 等输入类型**不得被任何导出方法接受**（可以保留导出或改为私有，以编译通过且测试可用为准；如保留导出，PR 里说明原因）。`PostResult`、`Balance` 因为是业务方法的返回值，保持导出。
2. **测试访问**：大量现有测试在 `ledger_test` 外部包里直接调用 `Post()`。用 `export_test.go`（只对本包测试可见）暴露测试专用的 `PostForTest` 等包装，把现有测试改过去；**不得因此在非测试代码里增加导出方法**。
3. **API 表面守卫测试**：新增 `TestPublicAPISurface`：用 reflect 列出 `*ledger.Service` 的全部导出方法，与代码里的白名单逐项比对；多一个或少一个都失败，失败信息提示"新增对外账务方法必须经过评审并更新白名单"。白名单就是 L-02～L-07 的业务方法与只读查询（EnsureWallet、Topup、FreezeHold、SettleHold、RefundHold、DisputeHold、ResolveDispute、RequestWithdrawal、StartReview、ApproveReview、ConfirmPayout、FailWithdrawal、CancelWithdrawal、Reverse、DeliveryHold/Release/Refund、StorageFee、AdCharge/Spend/Refund、MemberFee、Sweep、CarrierPay、BankDiff，以及 Get*/List*/Verify*/Recon*/Snapshot*/Freeze* 等只读或对账方法——以仓库当前实际导出为准，逐一核对，**不要为了让测试通过而放宽**）。
4. **导入守卫测试**：新增架构测试：解析 backend/internal 下除 ledger 包以外的所有 Go 文件的 import，断言它们**不 import ledger 的内部实现细节**（如未来的 ledger/internal/...）；并断言只有白名单内的包可以 import ledger（目前为空或仅 cmd、api 装配层），其余模块要用账务必须经评审加入白名单。
5. **人工调整 `Adjust`（新增，唯一的"自由记账"入口，设计得足够窄）**：
   - 输入：ReasonCode/Reason（非空）、IdemKey 来源（Ref 类型+编号）、借方账户、贷方账户、金额、ActorID、ReviewerID；
   - 规则：ActorID ≠ ReviewerID（两人）；借贷两个账户都必须是**平台自有账户**（PLATFORM_HELD、SUSPENSE、PAYABLE_CARRIER），**不得涉及 BANK_DEDICATED、任何用户钱包/冻结/广告预算/托管/待出款账户**（客户资金的更正只能走冲销或业务方法）；不得使客户资金账户为负；金额 > 0；
   - 类型固定为 `adjust`，同事务写 outbox 事件 `ledger.txn.adjusted`（payload：原因、两位操作人、金额、两个账户 id，不含敏感字段）；幂等键 `adjust:<ref_type>-<ref_id>`；
   - 这是依据 txn.type 已有的 `adjust` 类型做的最保守实现。**请在 PR 里把"是否允许 adjust 触及客户资金账户"列为待确认**，默认不允许；不要自己放开。
6. **文档与注释**：在 ledger 包的 doc.go 写清对外契约：对外只有业务方法；每个方法的前置条件、幂等键规则、错误类型；如何新增业务方法（必须有第六章对应条目、表驱动测试、恒等式随机序列覆盖）。

【测试】
1. 全部现有账务测试在重构后通过且**断言内容不变**（重构前后跑一遍对比测试数量，PR 里写明：重构前 N 个测试/子测试，重构后 N 个，无删减）；
2. `TestPublicAPISurface` 与导入守卫测试：故意在测试里模拟"多一个导出方法"（用一个本地副本或表驱动的假数据）证明守卫会失败，而不是只测通过的情况；
3. Adjust：成功路径与分录；ActorID = ReviewerID 拒绝；触及 BANK_DEDICATED、用户钱包、托管、待出款账户均拒绝；Reason 为空拒绝；重复调用幂等、同键不同内容冲突；事件恰好一条且无敏感字段；恒等式随机序列加入 Adjust 步骤后仍成立（Adjust 只在平台自有账户之间移动，恒等式不变）；并发 100 次相同 Adjust 只生效一次；
4. 用 app_ledger 身份跑 Adjust。
5. 全量 `-race`、`go vet`、CI 全绿；`go build ./...` 证明没有其他包依赖已收口的符号。

【验收标准】
① 通用 Post 不再是对外 API，编译层面阻止其他模块使用；② 对外方法表面有白名单守卫，新增必须过评审；③ 现有测试无删减、全部通过；④ 唯一的自由记账入口 Adjust 足够窄（两人、平台自有账户、不碰客户资金与银行账户）；⑤ 文档与 AGENTS.md 同步。

【红线】不改任何账务行为与迁移；不放宽守卫；不为了通过测试在非测试代码里导出新方法；Adjust 不得触及客户资金与银行账户。

【交付】PR 描述：收口前后导出符号对比、测试数量对比、Adjust 的规则与待确认项、对文档的改动。
```

---

## A-02 Telegram 登录（多 Bot 校验）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。A-01（iam 表）、F-05（网关）、F-10（信封加密）、P0-1（PoC，已合并 PoC 代码与 RESULT.md）已在 main。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】A-02 Telegram 登录（多 Bot 校验）
【分支】task/A-02-telegram-login，只提交到这个分支并发起 PR，不要推 main。
【合并闸门】本任务代码与测试可以先完成并评审，但**在 poc/telegram-initdata/RESULT.md 补上"真机 initData 校验通过 + 主 Bot 撤销后备用 Bot 通过"的真实证据之前不得合并**。PR 描述里写明这个闸门；不要伪造或预填证据。

【先阅读（按顺序）】
1. AGENTS.md（规则 9、10、11、13、14；第 8 节"遇到不确定时"）
2. Dr.life+第一版交付形态与技术架构.md：第二章（Telegram 自动登录、Bot 池、风险）、第六章"登录与会话"一行、第七章（域名与网关）
3. Dr.life+数据库表结构与账务详细设计.md：第二章 iam（person、account、bot、account_bot、login_event、session、account_device）
4. Dr.life+开发任务拆分与验收.md：P0-1、A-01、A-02、A-03、A-04、A-05 各行
5. poc/telegram-initdata/（validator.go、pool.go、RESULT.md——**算法与测试向量以 PoC 为准，但 PoC 是独立目录，不得被生产代码 import，请在 backend 内重新实现并复用其测试向量**）
6. backend/migrations/00003_iam_tables.sql、backend/internal/platform/gateway/（Identity、Authenticator、限流、幂等）、backend/internal/platform/secrets/（Envelope 与 AAD 约定 `bot_token:<id>`）、backend/internal/api/handler.go 与 backend/api/openapi.yaml、httpx 错误码
7. Telegram 官方文档 Validating data received via the Mini App（以官方当前文档为准）

【背景】
买家、商家、司机三个端都是 Telegram Mini App，各有一个 Bot 池（主 + 备用）。登录只信任**服务端校验过的 initData**；对该端池内**所有**令牌逐个校验，任一通过即有效；账号按 `Telegram 用户编号 + 角色` 保存，**不绑定某个 Bot**；买家池令牌不能登录商家端。

【文件归属】backend/internal/iam/（新增 telegram 校验、登录服务、令牌签发、认证器等）、backend/api/openapi.yaml 与生成代码、backend/internal/api/ 的接口装配、httpx 新增错误码、config 新增配置、必要时迁移 00011（见下）。不要改账务、不要写刷新/作废逻辑（A-03）、不要写员工邀请绑定（A-05）、不要写语言登记接口（A-04）。

【范围——要做】
1. **OpenAPI 先行**：新增 `POST /v1/public/auth/telegram`（无需登录，`security: []`）。请求体：`end`（buyer|merchant|driver）、`init_data`（字符串，≤ 4096 字节）、`device_id`（可选，≤ 128 字符，仅字母数字与 `-_:`）、`start_param` 不单独传（它在 initData 里，服务端自己解析）。响应：`access_token`、`access_expires_in`、`refresh_token`、`refresh_expires_in`、`account`（public_id、role、lang、status）、`needs_language`（bool：lang 为空时 true）、`is_new_account`。错误响应沿用统一格式；新增 2xxx 错误码并写进 httpx 与 OpenAPI：登录失败（所有校验类失败**统一一个对外码**，不泄露是哪一步失败）、账号被冻结/停用（可区分，不暴露内部原因细节）、尝试过多（429）。先改 openapi.yaml → 生成代码 → 再实现；OpenAPI 无差异检查必须通过。
2. **initData 校验（backend/internal/iam/telegram/）**：按官方算法（secret = HMAC_SHA256("WebAppData", token)，data_check_string 为除 hash 外所有字段按键排序以换行连接；`signature` 字段保留在 HMAC 输入中——与 PoC 结论一致，以官方文档当前内容为准）；恒定时间比较；解析 user（id > 0）、auth_date；**auth_date 最大有效期可配置**（`TELEGRAM_INITDATA_MAX_AGE`，默认 24 小时，与架构文档"建议 24 小时内"一致；PoC 默认 1 小时，注释里说明差异与取舍：initData 在 Mini App 打开期间不会刷新，所以刷新令牌用来承接后续静默续期；有效期越长，被盗 initData 的可重放窗口越长）；未来时间容差 60 秒；输入长度上限；重复字段拒绝；畸形输入不 panic（加模糊测试）。
3. **Bot 池校验**：从 iam.bot 读取该端全部 Bot（按 role_in_pool、priority 排序，主 Bot 优先；**状态为 limited、banned 的 Bot 也要参与校验**——令牌仍有效时旧入口的用户必须还能登录，见架构文档应急切换一条）；令牌用 F-10 信封解密（AAD `bot_token:<id>`），**解密后的令牌只放内存，用 secrets.Secret 类型包装，不进日志、错误、指标**；可做短 TTL 内存缓存（默认 5 分钟，管理端更换令牌/状态后可手动失效的接口预留）；逐个校验，记录命中的 Bot id 与尝试次数（tried_count）；**买家池令牌不能通过商家端校验**（端与池严格隔离，测试证明）。
4. **账号与人员**：命中后：按 `tg_user_id` 查找或创建 `iam.person`（更新 tg_first_name、tg_username、tg_lang_code，不覆盖已登记的账户语言）；按 (person, role) 查找或创建 `iam.account`（buyer 端 → role=buyer；merchant 端 → role=merchant_owner；driver 端 → role=driver），**用数据库唯一约束 + ON CONFLICT 保证并发首次登录只创建一个账号**（100 并发测试）；新用户 `is_new_account=true`、`needs_language=true`；public_id 用 ULID（复用 backend/internal/platform/ids）；账号 status 为 frozen/disabled → 拒绝并返回账号状态错误码（不签发令牌），仍写 login_event。
5. **account_bot / account_device**：记录用户在命中 Bot 下的绑定（write_access：initData 里 `allows_write_to_pm` 为真时置 true）；device_id 非空时 upsert account_device（更新 last_seen_at）。
6. **login_event**：**每次登录尝试（成功与失败）都写一条**：person_id（失败时可空）、bot_id（本次请求所用端的主 Bot id 或空）、valid_by_bot_id（命中的 Bot）、tried_count、auth_date、ip、device_id、result（枚举：ok、invalid_signature、expired、malformed、account_blocked、rate_limited 等）。**不得记录完整 initData、令牌、手机号**；ip 取网关解析出的客户端 IP。
7. **会话与令牌签发（最小版，A-03 会补充刷新/作废）**：
   - 访问令牌：JWT（`golang-jwt/jwt/v5`，版本固定），HS256 或 EdDSA 二选一并在 PR 说明理由；签名密钥来自 F-10 的密钥机制（支持 `kid`，同时可验证新旧两把密钥以便轮换）；claims：sub = 账号 public_id、role（网关 Role 取值：buyer/merchant/driver）、sid = 会话 public_id、exp、iat、jti；**有效期默认 15 分钟，可配置**；
   - 刷新令牌：高熵随机串（≥ 32 字节，base64url），**只存 SHA-256 哈希**到 iam.session.refresh_token_hash；有效期默认 30 天可配置；session 记录 device_id、ip、expires_at；
   - 实现 `gateway.Authenticator`（`iam.Authenticator`）：从 Authorization: Bearer 解析并校验签名、过期、角色取值，返回 gateway.Identity{AccountID: 账号 public_id, Role}；**角色与路径不匹配仍由网关 403**（已有逻辑，别重复造）；本任务的认证器**不查会话是否已作废**（A-03 加），但要把接口留好（可注入的 SessionChecker，默认总是通过，并在注释里标明 A-03）。
   - 令牌**不能**在日志、错误或响应以外的地方出现；响应里的刷新令牌只出现一次。
8. **防刷与失败封禁**：沿用网关"登录"类限流（IP + 账号维度，已有）；另加：同一 IP 在窗口内校验失败超过阈值（默认 20 次 / 10 分钟，可配置）→ 临时封禁该 IP 的登录接口（返回尝试过多错误码），到期自动解除；封禁与计数用现有 Limiter（Redis/内存）实现；写 login_event(result=rate_limited)；指标：登录校验失败按端与原因计数（使用 observability.Business 已有的"登录校验失败"埋点位）。
9. **启动参数 start_param**：从 initData 解析并**只记录**（渠道码、邀请码）到 login_event 或日志可用的非敏感字段；员工邀请码的绑定逻辑属于 A-05，本任务**不实现绑定**，但解析函数要有测试。
10. **配置**：新增配置项（带默认值与校验）：TELEGRAM_INITDATA_MAX_AGE、ACCESS_TOKEN_TTL、REFRESH_TOKEN_TTL、JWT 签名密钥来源、登录失败封禁阈值与窗口、Bot 令牌缓存 TTL；`backend/.env.example` 只放占位符；README 写清。

【测试（真实 PostgreSQL + 合成令牌；**不得使用真实 Bot 令牌**）】
1. **校验核心**：PoC 测试向量全部复现；逐字段篡改（user、auth_date、query_id、start_param…）失败；过期、未来时间、缺 hash、重复字段、超长、畸形、模糊测试；
2. **池隔离**：买家池令牌签发的 initData 在商家端、司机端校验失败；主 Bot 令牌被移除后备用 Bot 签发的仍通过；limited/banned 状态的 Bot 令牌仍能校验通过；tried_count 与 valid_by_bot_id 正确；
3. **并发首次登录**：同一 Telegram 用户 100 并发首次登录，只产生 1 个 person、1 个 account、n 条 login_event，全部成功返回同一 account public_id；
4. **账号状态**：frozen/disabled 账号被拒绝且无令牌；
5. **login_event**：成功与每种失败都写一条，字段正确，**不含 initData/令牌**（对 login_event 的所有列、日志输出、错误字符串做子串断言）；
6. **令牌**：JWT 验证（签名、过期、错误 kid、算法混淆攻击如 alg=none/换算法被拒）、密钥轮换（旧新密钥都可验）、刷新令牌只存哈希（数据库里查不到明文）；
7. **网关集成**：用签发的买家访问令牌访问 /v1/merchant/*、/v1/admin/* → 403；无令牌 → 401；过期令牌 → 401；
8. **封禁**：IP 连续失败超过阈值后被临时封禁，到期解除；封禁期间正确的 initData 也被拒（记 rate_limited）；
9. **接口契约**：OpenAPI 契约测试通过（kin-openapi 已有）；错误响应格式统一；对外错误信息不泄露失败原因；
10. 令牌与 Secret：令牌相关类型的 fmt/日志输出为打码字符串（Secret 的行为测试）。

【验收标准】
① 买家池令牌不能登录商家端；② 校验命中的 Bot 与尝试次数写入 login_event；③ 过期/篡改拒绝；④ 新用户自动建账号并标记需选语言；⑤ 限流与失败封禁；⑥ 令牌与 initData 不进日志、不进错误、不进 login_event；⑦ 并发首次登录只建一个账号；⑧ OpenAPI 先行且无差异；⑨ go vet、-race、集成测试、前端无影响、ci-gate 全绿。⑩ **合并闸门**：PoC 的真机证据补全前不得合并。

【待确认（不要自己拍板，PR 里列出）】
- merchant 端、driver 端首次登录是否自动创建账号：本实现 buyer/merchant_owner/driver 都自动创建；merchant_owner 此时 `merchant_id` 为空（商户资料与入驻审核由后续任务补），driver 的 `kyc_status` 设为 'pending'。若业务要求司机、商家必须先经平台开通才能登录，请指出，需要改为"白名单/邀请制"；
- JWT 算法与密钥轮换方式的最终取舍；
- initData 最大有效期默认 24 小时 vs PoC 的 1 小时；
- 二次验证（短信/支付密码）不在本任务。

【红线】不得 import poc/ 目录；不得把 Bot 令牌、签名密钥、刷新令牌、initData 写入日志/错误/指标/Sentry；不得信任前端传来的任何用户信息（只信校验后的 initData）；不得用 Telegram 用户编号以外的字段识别人；不改已合并迁移。

【交付】PR 描述：接口与错误码清单、令牌方案与理由、配置项清单、测试对照、待确认事项、合并闸门说明、对文档的改动。
```

---

## 评审要点（给我自己用）

| 任务 | 重点 |
|---|---|
| L-07 | 核对是否在同一只读 REPEATABLE READ 事务里（并发入账无假阳性）；是否真的"不信任缓存"（从分录重算）；绕过触发器的脏数据能否检出；冻结只拦"钱出去"的路径、不拦"退回用户"；冻结检查在出款事务内；快照只增不改且能发现历史被改；事件与日志无敏感字段；规模耗时数据 |
| L-12 | 是否真的收口（grep 无导出 Post）；现有测试数量无删减；守卫测试会不会"测不出问题"（是否有负向证明）；Adjust 是否真的挡住客户资金与银行账户；没有改行为/迁移 |
| A-02 | 池隔离；limited/banned 参与校验；JWT alg 混淆；refresh 只存哈希；login_event 与日志无 initData/令牌；并发首次登录；统一错误码不泄露原因；是否 import poc；**闸门是否被遵守（真机证据未补不得合并）** |
