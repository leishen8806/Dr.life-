# Codex 任务提示词：L-04（冻结明细）、L-05（提现与复核）、L-06（冲销）

**用法：** 整块复制代码块内容粘贴给 Codex。基线 `origin/main` 最新提交（L-03、F-07、F-11 已合并）。
**三个任务都改 `backend/internal/ledger/`，彼此有重叠，建议顺序执行：L-04 → L-05 → L-06**（每个任务合并后再开下一个；如果一定要并行，L-05 与 L-06 要各自 rebase，并约定迁移序号：L-04 用 `00006`、L-05 用 `00007`、L-06 一般不需要迁移）。
**这批是账务最后一块硬骨头，评审会最严格。** 合并前确认 `ci-gate` 为绿。

**需要你（人）先回答的两个业务问题**（Codex 提示词里已写了推荐做法，你只需确认或改）：
1. **大额出款谁是"发起人"、谁是"复核人"？** 权限规范写的是"财务 A 发起 + 另一位有出款权限的管理员 B 复核"。已合并的 `withdrawal` 表只有 `maker_actor`（创建时必填）。推荐：用户申请提现时记录 `requester_actor`（新列），`maker_actor`/`checker_actor` 都指**管理员**，在出款处理阶段才填，大额必须两人且不同。
2. **冻结资金可以分批部分结算吗？** 目前设计是"一个批次一条 hold，整笔结算或整笔退回"。推荐第一版保持整笔，部分结算以后再加。

---

## L-04 冻结明细与分批结算

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。L-01～L-03 已合并。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】L-04 冻结明细（ledger.hold）与分批结算
【分支】task/L-04-ledger-holds，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（账务 1–7 条；规则 8、12）
2. Dr.life+数据库表结构与账务详细设计.md：第三章、第五章、第六章 6.2–6.4、"配套表"（ledger.hold）、第十一章
3. Dr.life+开发任务拆分与验收.md：L-03、L-04、L-05、L-06 四行
4. backend/migrations/00004、00005；backend/internal/ledger/ 全部（post.go、business.go、accounts.go、query.go、outbox.go 及测试）

【背景】
装修资金按批次冻结：买家的可用资金冻结到 WALLET_FROZEN，节点确认后结算给收款方，或争议后退回。L-03 已有无状态的 `Freeze/Settle/RefundFrozen`，只管分录。L-04 把它们与 `ledger.hold` 表绑成一个**有状态机、同事务**的流程，使"冻结账户余额 = 未结清 hold 之和"恒成立。

【文件归属】backend/internal/ledger/（新增 hold.go 等与测试）、迁移 00006_ledger_hold_guard.sql、更新《数据库表结构与账务详细设计》（hold 状态机与新增约束说明）。不要改 iam、不要新增 HTTP 接口。

【先做：架构整理】
在 `Post` 内部增加"事务内钩子"（已有 transactionCheck 前置钩子；再加**后置钩子** `func(ctx, tx, PostResult) error`，在 posting 写入之后、COMMIT 之前执行），使"记账 + 更新 hold 状态/写 hold 行"在**同一事务**内完成。重放（幂等命中）时后置钩子**不重复执行**，但要校验 hold 当前状态与该次操作一致，否则返回 ErrIdempotencyConflict。

【hold 状态机（Go + 数据库双重保证）】
held（已冻结）→ released（已结算给收款方）｜ refunded（已退回买家）｜ disputed（争议冻结）；disputed → released｜refunded；released/refunded 为终态，不可再变。
迁移 00006：
1. BEFORE UPDATE 触发器：只允许上述合法状态迁移；amount、account_id、ref_type、ref_id、freeze_txn_id、created_at 任何角色都不能改；close_txn_id 与 closed_at 只能在进入终态时一次性写入且必须同时写；DELETE/TRUNCATE 拒绝（SQLSTATE 55000，风格同 00005）。
2. `ledger_rw` 对 hold：撤销 DELETE/TRUNCATE/REFERENCES/TRIGGER；UPDATE 仅保留 (status, close_txn_id, closed_at, dispute_reason, dispute_by)（如新增这些列则在本迁移中添加：`dispute_reason text`、`disputed_by bigint`、`disputed_at timestamptz`、`resolved_by bigint`，并加 CHECK：status='disputed' 时 dispute_reason 非空）。
3. 迁移可逆（Down），并更新设计文档。

【方法（强类型，复用 L-03 的账户归属校验）】
- `FreezeHold(ctx, FreezeHoldInput{RefType, RefID, WalletAvailable, WalletFrozen, Amount, ActorID})`：同事务写 freeze 分录 + 插入 hold(held, freeze_txn_id)；`UNIQUE(ref_type, ref_id)` 冲突视为重复请求（内容一致返回原结果，不一致返回 ErrIdempotencyConflict）；余额不足 ErrInsufficientFunds 且无 hold 残留。
- `SettleHold(ctx, SettleHoldInput{HoldID, PayeeAvailable, Fee, ActorID})`：`SELECT … FOR UPDATE` 锁 hold 行（并保持 L-02 的"按 account_id 升序锁 balance 行"顺序；hold 行锁放在 balance 行锁之后或之前都可以，但**全局固定一种顺序并写进注释**，防止与其他方法交叉死锁）；仅 held 或 disputed→（裁决后）可结算；金额取自 hold.amount（**调用方不得传金额**），第一版 Fee 恒为 0（参数保留并测试）；结算后 hold=released 并写 close_txn_id。
- `RefundHold(ctx, RefundHoldInput{HoldID, ActorID, PayerActorID})`：退回买家；SoD：裁决人（ActorID）≠ 出款人（PayerActorID）；hold=refunded。
- `DisputeHold(ctx, {HoldID, ActorID, Reason})`：held→disputed，**不动账**（无分录），reason 非空；争议期间 SettleHold / RefundHold 直接拒绝（ErrHoldDisputed），必须先 `ResolveDispute`。
- `ResolveDispute(ctx, {HoldID, Resolution: "release"|"refund", ResolvedBy, PayerActorID, PayeeAvailable})`：裁决人（ResolvedBy）≠ 出款人（PayerActorID）；内部调用 SettleHold 或 RefundHold 的同事务实现。
- **收口**：L-03 的导出方法 `Freeze`、`Settle`、`RefundFrozen` 改为**包内私有**（小写），外部模块只能走带 hold 的新方法，避免绕过状态机；相应调整 L-03 的测试。（`Post()` 本身是否也应收口见文末"待你决定"，**本任务不动**。）
- 查询：`GetHold(ctx, id)`、`ListHoldsByAccount(ctx, walletFrozenAccountID, statuses)`（只读）。

【不变量检查函数】
`VerifyHoldsMatchFrozen(ctx)`：对每个 WALLET_FROZEN 账户断言 `balance = sum(hold.amount where status in ('held','disputed'))`，返回不一致的账户列表（供 L-07 每日对账调用）；测试里在每个场景后调用。

【测试（真实 PostgreSQL，testcontainers，-race）】
1. hold 与分录一一对应：每个 hold 的 freeze_txn_id / close_txn_id 指向的交易分录与第六章表格一致；
2. 状态机全路径表驱动：合法迁移成功，非法迁移（如 released→refunded、refunded→released、disputed 直接结算）被拒；并且绕过 Go、直接用 SQL（管理员连接与 app_ledger 连接）改 hold 的 amount/状态/删除，被触发器或权限拒绝；
3. 争议：disputed 期间冻结余额不动；裁决人 = 出款人被拒（Go 层）；
4. 并发：同一 hold 并发 SettleHold + RefundHold，只有一个成功，另一个得到状态错误，余额不重复动；同一 (ref_type, ref_id) 并发 FreezeHold 100 次只有一个 hold、一次冻结；
5. 幂等重放不重复执行后置钩子，且重放时状态不一致返回冲突；
6. 余额不足时 FreezeHold 无 hold 残留、无 txn/posting/outbox 残留；
7. 不变量 VerifyHoldsMatchFrozen 在随机序列（至少 200 步：冻结/结算/退回/争议/裁决混合，固定种子）每一步后成立；扩展 L-03 的恒等式随机序列测试；
8. 用 app_ledger 登录身份跑主要流程，证明收紧后的权限够用；
9. Down/Up 往返。

【验收标准】
① hold 与分录一一对应；② 争议冻结不动；③ 裁决人 ≠ 出款人；④ 状态机由数据库触发器兜底；⑤ 冻结余额恒等于未结清 hold 之和；⑥ go vet、-race、集成测试、ci-gate 全绿；没有 float、没有 HTTP 接口。

【待确认（不要自己拍板）】分批是否支持**部分结算**（一个 hold 分多次结算）。第一版按"整笔结算/整笔退回"实现，PR 里列为待确认，不要偷偷做部分结算。

【红线】不改 00004/00005；不放宽任何触发器与权限；不用 float；其他模块不得直接写 hold 表。

【交付】PR 描述：状态机图、锁顺序说明、导出方法收口清单、测试对照、对设计文档的改动、待确认事项。
```

---

## L-05 提现与复核

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。L-04 已合并（若未合并先等合并）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】L-05 提现与复核（withdrawal 状态机）
【分支】task/L-05-ledger-withdrawals，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（账务 1–7 条；规则 8、11、12、14）
2. Dr.life+数据库表结构与账务详细设计.md：第六章 6.5–6.7、withdrawal 表定义、第十一章
3. Dr.life+平台后台权限与审计规范.md：职责分离规则、大额出款（≥ 阈值，默认 1,000 美元）、审计项 #3、#16（提现超时 24 小时）
4. Dr.life+技术方案与系统边界.md：状态机（提现：申请 → （大额）复核 → 银行出款 → 完成；失败回滚）、高风险二次验证
5. Dr.life+开发任务拆分与验收.md：L-05 一行
6. backend/internal/ledger/ 全部（尤其 L-03 的 WithdrawReq/Payout/PayoutFail、L-04 的后置钩子）、backend/internal/platform/secrets/（F-10 信封加密）、observability/business.go

【背景】
提现是最容易出资损的流程。L-03 的 `WithdrawReq/Payout/PayoutFail` 只管分录，金额与钱包由调用方传入，没有和提现单绑定（评审已指出）。L-05 把它们与 `ledger.withdrawal` 绑成**状态机 + 同事务**：分录、提现单状态、复核人一次提交。

【文件归属】backend/internal/ledger/（withdrawal.go 等与测试）、迁移 00007_ledger_withdrawal_guard.sql、更新《数据库表结构与账务详细设计》withdrawal 表与状态机。不要写 HTTP 接口、不要改 iam。

【设计决定（请按此实现，并在 PR 里写明；与文档冲突处列为待确认）】
1. **三类人**：`requester_actor`（用户本人，申请提现）、`maker_actor`（管理员，发起出款处理）、`checker_actor`（另一位管理员，大额复核）。迁移 00007：新增 `requester_actor bigint not null`、`requires_review boolean not null`、`reviewed_at timestamptz`、`failed_reason text`；把 `maker_actor` 改为可空（用户申请时还没有管理员发起）；保留 `ck_withdrawal_separation`。**新增 CHECK**：`status in ('approved','paid') AND requires_review` → `checker_actor IS NOT NULL AND maker_actor IS NOT NULL`；`checker_actor IS NOT NULL` → `maker_actor IS NOT NULL`。表里现有数据为空，迁移无需回填。
2. **account_id 的含义**：存申请人的 `iam.account.id`（ledger 不跨 schema 外键）；对应的 WALLET_AVAIL 由 ledger 按 owner_account 解析。
3. **账号状态检查**（冻结/停用禁止提现）：ledger 不得读 iam 表（规则 8）。定义接口 `AccountGuard interface{ CanWithdraw(ctx, accountID int64) error }`，由 `ledger.New(..., Options{AccountGuard: ...})` 注入；本任务提供一个测试用的假实现；未注入时 `RequestWithdrawal` 直接拒绝（fail-closed）。
4. **阈值可配置**：`Options.LargeWithdrawalThreshold`，默认 1000 美元 = 10_000_000（单位 0.0001 美元）；`requires_review = amount >= threshold`，**申请时就固定写入该行**（之后改阈值不影响已申请的单子）。
5. **收款账户**：`payee_account_text` 用 F-10 的信封加密（定义 `PayeeCipher` 接口：Encrypt/Decrypt，AAD = `withdrawal_payee:<public_id>`）；`payee_account_last4` 明文末 4 位；日志、指标、错误信息、outbox 事件里**不得出现收款账户全文**；提供一个"只返回末 4 位"的读取函数。
6. **public_id**：ULID（26 位 Crockford Base32）；若仓库已有生成函数则复用，否则放在 `backend/internal/platform/ids/` 的小包里并测试。

【状态机】
requested →（requires_review）review → approved → paid
requested →（不需要复核）approved（自动，同事务）
requested｜review｜approved → cancelled（撤销）；approved → failed（银行退回）
paid、failed、cancelled 为终态。
- `RequestWithdrawal(ctx, {RequesterAccountID, WalletAvailable, Amount, PayeeAccount, ActorID})`：同事务：AccountGuard 检查 → 加密收款账户 → 写 withdrawal(requested，或直接 approved 当不需要复核) → 记 `withdraw_req` 分录（DR WALLET_AVAIL / CR PENDING_PAYOUT）→ 回填 req_txn_id；余额不足 ErrInsufficientFunds 且无任何残留。
- `StartReview(ctx, {WithdrawalID, MakerActor})`：requires_review 的单：requested→review，写 maker_actor（管理员）。
- `ApproveReview(ctx, {WithdrawalID, CheckerActor})`：review→approved，写 checker_actor、reviewed_at；**checker == maker 或 checker == requester 被拒绝（Go 层 + 数据库 CHECK 双保险）**。
- `ConfirmPayout(ctx, {WithdrawalID, BankRef, ActorID})`：approved→paid；**同事务**记 `payout` 分录（DR PENDING_PAYOUT / CR BANK_DEDICATED），金额**取自 withdrawal 行**（调用方不传金额），回填 paid_txn_id、bank_ref、paid_at；需要复核的单若无 checker 被拒绝；bank_ref 非空；同一 bank_ref 不得用于两个提现单（加唯一索引：`unique (bank_ref) where bank_ref is not null`）。
- `FailWithdrawal(ctx, {WithdrawalID, Reason, ActorID})`：approved→failed，同事务记 `payout_fail` 分录（DR PENDING_PAYOUT / CR 申请人的 WALLET_AVAIL），金额与钱包都取自提现行。
- `CancelWithdrawal(ctx, {WithdrawalID, Reason, ActorID})`：requested｜review｜approved → cancelled，同事务退回可用余额（同 payout_fail 分录类型，幂等键用 `payout_fail:withdrawal:<id>` 保证只能退一次）；已 paid 不可撤销。
- **并发**：所有状态变更先 `SELECT … FOR UPDATE` 锁 withdrawal 行再检查状态；与 balance 行锁的顺序固定并写进注释（与 L-04 的 hold 约定一致：全局统一一种顺序）。
- **收口**：L-03 的导出方法 `WithdrawReq`、`Payout`、`PayoutFail` 改为包内私有；外部只能走上面的状态机方法。

【迁移 00007：数据库兜底】
1. BEFORE UPDATE 触发器：只允许上述合法迁移；`amount、account_id、payee_account_text、payee_account_last4、public_id、requester_actor、requires_review、requested_at、req_txn_id（写入后）` 不可改；终态后任何列都不可改；`maker_actor`/`checker_actor` 一旦写入不可改；DELETE/TRUNCATE 拒绝（55000）。
2. `ledger_rw` 对 withdrawal：撤销 DELETE/TRUNCATE/REFERENCES/TRIGGER；UPDATE 保留状态相关列（status、maker_actor、checker_actor、reviewed_at、paid_txn_id、bank_ref、paid_at、failed_reason）。
3. 迁移可逆，并更新设计文档。

【超时检测（审计 #16）与大额关注（审计 #3）】
- `ListOverdueWithdrawals(ctx, now, olderThan time.Duration)`（默认 24 小时）：status in ('requested','review','approved') 且 requested_at 早于阈值；`ListLargePending(ctx)`：requires_review 且未终态。只读，返回 public_id、金额、状态、已等待时长，**不含收款账户**。
- 在 `observability.Business` 增加提现相关埋点调用（申请、复核通过、出款、失败、撤销、超时数）；不要引入对 observability 的循环依赖（用接口注入）。

【二次验证（短信/支付密码）】不在 ledger 内实现，由 API 层（A 系列任务）负责；在 `RequestWithdrawal` 的注释和 PR 说明里写明"调用前必须已通过二次验证"，并在 Input 里加 `SecondFactorVerified bool`，为 false 时拒绝（fail-closed）。

【测试（真实 PostgreSQL，-race）】
1. **发起人 = 复核人被数据库拒绝**：绕过 Go，用 SQL 直接写 checker_actor = maker_actor，被 CHECK 拒绝；Go 层 checker = maker 或 checker = requester 也被拒绝；
2. 大额（≥ 阈值）没有复核人不能 approved/paid（数据库 CHECK + Go），阈值边界：阈值-1、阈值、阈值+1；阈值可配置并测试默认 10_000_000；
3. 失败回滚到可用：FailWithdrawal 与 CancelWithdrawal 后 WALLET_AVAIL 回到申请前，PENDING_PAYOUT 回零，分录与第六章一致；已 paid 不可撤销；
4. 账号冻结/停用（假 AccountGuard 返回错误）禁止提现，且无 withdrawal/txn/outbox 残留；未注入 AccountGuard 时 fail-closed；SecondFactorVerified=false 拒绝；
5. **并发**：同一提现单并发 ConfirmPayout + CancelWithdrawal + FailWithdrawal 只有一个成功；同一提现单并发 ConfirmPayout 100 次只出款一次；同一用户并发 100 次申请，余额不为负、不超额；重复 bank_ref 被拒；
6. **金额绑定**：ConfirmPayout/FailWithdrawal 不接受外部金额与钱包；提现行 amount 被篡改（管理员 SQL）被触发器拒绝；
7. **状态机全路径表驱动**（合法/非法迁移）及触发器兜底（直接 SQL 改终态单、改金额、删除单都失败；用 app_ledger 身份）；
8. **收款账户保护**：数据库里存的是密文（断言不含明文子串）；日志（捕获 slog 输出）、错误字符串、outbox payload 都不含收款账户全文；解密只在专门函数里；
9. **超时检测**：伪造时间（注入 now）验证 24 小时边界；大额待出款列表；返回结果不含收款账户；
10. 恒等式随机序列（在 L-03/L-04 的基础上加入提现步骤，≥ 300 步）每步后：恒等式成立、PENDING_PAYOUT 余额 = 未终态提现单金额之和（新增 `VerifyPendingPayoutMatchesWithdrawals`）；
11. 迁移 Down/Up 往返；用 app_ledger 身份跑主要流程。

【验收标准】
① 发起人 = 复核人被**数据库**拒绝；② 失败回滚到可用；③ 账号冻结/停用禁止提现；④ 阈值可配置（默认 1000 美元）；⑤ 超时进入审计关注（24 小时）；⑥ 金额与钱包取自提现行，调用方无法篡改；⑦ PENDING_PAYOUT 余额恒等于未终态提现单之和；⑧ 收款账户全文不进日志/事件/指标；⑨ go vet、-race、集成测试、ci-gate 全绿。

【待确认（不要自己拍板，列在 PR 里）】
- 小额（< 阈值）提现是否也需要管理员点"确认出款"：本实现是"自动 approved，仍需 ConfirmPayout 回填银行回单"，请在 PR 里确认这是否符合预期；
- 设计文档第六章写 `PENDING_PAYOUT(A)`（按用户），已合并 SQL 是单例，本任务以"单例 + withdrawal 行做明细"实现，并建议同步文档措辞；
- 管理员 actor 与用户 actor 的区分（iam.actor.kind）在 A-06 之前先用整数占位，保持接口为 int64。

【红线】不改 00004/00005/00006；不放宽任何触发器与权限；收款账户全文不进日志；不用 float；不写 HTTP 接口。

【交付】PR 描述：状态机图、三类人与表变更说明、锁顺序、收口清单、测试对照、待确认事项、对设计文档的改动。
```

---

## L-06 冲销与调整

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。L-04、L-05 已合并（若未合并先等合并）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】L-06 冲销（Reverse）
【分支】task/L-06-ledger-reverse，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（账务 2、4；规则 12）
2. Dr.life+数据库表结构与账务详细设计.md：第五章（reversal 相关约束）、第六章 6.18、第十一章
3. Dr.life+平台后台权限与审计规范.md：冲销需"资金"编辑权限并写审计
4. Dr.life+开发任务拆分与验收.md：L-06 一行
5. backend/migrations/00004（`validate_reversal`、`uq_txn_reverses_once`、`ck_txn_reversal_reference`）；backend/internal/ledger/ 全部

【背景】
流水只增不改不删，更正靠冲销：新增一笔交易，把原交易的分录逐条借贷互换，指向原交易，原交易保持不变。L-01 已在数据库层限制"一笔交易最多被冲销一次"；L-06 做 Go 层的 `Reverse()` 与业务规则。

【文件归属】backend/internal/ledger/（reverse.go 与测试）；仅在发现数据库约束不足时才新增迁移 00008（并说明理由）；更新设计文档 6.18 的补充说明。不写 HTTP 接口、不改 iam。

【范围——要做】
`Reverse(ctx, ReverseInput{OriginalTxnID, ActorID, Reason})`：
1. 读取原交易及其全部分录；**逐条互换借贷，账户、金额、role、category 不变**；新交易 type='reversal'，amount = 原金额，reverses_txn_id = 原交易 id，幂等键 `reversal:txn:<原交易 id>`（同一交易只能冲销一次；重复调用返回原结果，不是新交易）。
2. **Reason 必填**（非空、长度上限），写入 note；ActorID > 0。
3. **不可再冲销**：已被冲销的交易、以及 reversal 类型的交易本身，不能被冲销（返回 ErrAlreadyReversed / ErrNotReversible；数据库唯一约束兜底，并发下只有一个成功）。
4. **只允许冲销白名单类型（第一版）**：topup、storage_fee、member_fee、ad_charge、ad_spend、ad_refund、bank_diff、sweep、carrier_pay、adjust。**不允许**直接冲销：freeze/settle/refund（要走 hold 状态机，L-04）、withdraw_req/payout/payout_fail（要走提现状态机，L-05）、delivery_hold/delivery_release/delivery_refund（按单托管逻辑，需另行设计）。返回 ErrReversalNotAllowed，并在 PR 里列出每一类被排除的理由，列为"待确认"，不要自己扩大白名单。
5. **余额规则不放宽**：冲销也要走 `Post()` 的非负检查。如果冲销会让客户资金账户为负（例如冲销一笔已被花掉的充值），返回 ErrInsufficientFunds 且不记账；PR 里说明此时应由财务通过其他业务流程处理，而不是强行透支。
6. **对称校验**：冲销交易写入前，在同一事务内重新读取原分录核对（防止调用方篡改），并在测试里证明"冲销后所有受影响账户的余额回到原交易之前的值"（在没有其他并发交易的场景下），以及"冲销交易自身借贷平衡、金额相等"。
7. **权限与审计**：ledger 不做角色判断（权限在 API 层：需"资金"编辑权限），但要求 ReverseInput 带 `AuthorizedBy` 字段说明已授权（actor id，非空且 > 0）；同事务写 outbox 事件 `ledger.txn.reversed`（含原交易 id、冲销交易 id、reason、actor），由审计模块（后续任务）消费并写入哈希链审计日志。**不要在 ledger 里直接写 audit schema**（规则 8）；在 PR 里列为"待确认：审计写入由事件消费方保证至少一次"。
8. 提供只读查询：`GetReversalOf(ctx, originalTxnID)`；以及在 `GetByID` 结果里能看出"已被冲销"（只读，不改原交易）。

【测试（真实 PostgreSQL，-race）】
1. 对白名单的每种类型各做一笔业务再冲销：冲销后账户回到原值，恒等式成立；
2. 重复冲销被拒；**并发 100 次冲销同一笔**只有一个成功，其余返回同一结果或 ErrAlreadyReversed，且最终只有一条冲销交易；
3. 冲销一笔 reversal 被拒；冲销非白名单类型被拒；冲销不存在的交易被拒；
4. 余额不足时冲销被拒且无残留（先花掉充值，再冲销充值）；
5. 篡改防护：用 SQL 绕过 Go，写一条"借贷不对称"的冲销（金额或账户不一致）——触发器/约束能拦住的要有断言，拦不住的（L-01 里是"逐条对称留给 L-06"）在此新增校验：**如果现有 `validate_reversal` 不能保证逐条对称，请在迁移 00008 里补一个延迟约束触发器**（提交时核对：冲销交易的分录集合 = 原交易分录借贷互换），并测试；
6. Reason 为空、ActorID 无效被拒；outbox 事件写入且内容正确；重放不产生第二条事件；
7. 随机序列测试（L-03/L-04/L-05 的基础上加入冲销步骤）每步后恒等式成立；
8. 用 app_ledger 身份跑主要流程。

【验收标准】
① 冲销后账户回到原值；② 重复冲销被拒（含并发）；③ 冲销逐条对称（数据库兜底）；④ 需 reason 与授权人并写审计事件；⑤ 白名单之外一律拒绝；⑥ go vet、-race、集成测试、ci-gate 全绿。

【红线】不修改或删除原交易；不放宽触发器与权限；不扩大白名单；不直接写 audit schema。
**已合并的迁移（00001–00008）一律不得修改**，任何变更只能新增迁移（本任务如需迁移，使用 00009）。
**事件风格**：沿用 L-05 的 outbox 约定（topic 形如 `ledger.txn.reversed`，payload 只含 public 标识、金额、操作人、reason、时间，**不含任何敏感字段**）；L-05 的 `insertWithdrawalEvent` 可参考，但冲销事件要和冲销交易在同一事务内写入，重放不重复写。
**偶发失败提醒**：评审时全量 `-race` 曾出现过一次未复现的账务包失败；请在本任务里把新增并发测试跑 `-count=5`，并注意不要依赖时序假设（用同步原语而不是 sleep）。

【交付】PR 描述：白名单与排除理由、对称校验方案（是否新增迁移及理由）、事件格式、测试对照、待确认事项。
```

---

## 待你决定（不阻塞本批）

| 问题 | 说明 | 我的建议 |
|---|---|---|
| `Post()` 要不要收口？ | 设计文档规定"其他模块只能调用 `Post(...)`"，但现在 `Post()` 是导出的，任何模块可以用任意类型、任意账户记账，等于绕过了业务封装与状态机 | L-06 合并后单独做一个小任务：把通用 `Post()` 改为包内私有，只保留业务方法，必要的"人工调整 `adjust`"另设带复核的专用方法 |
| 部分结算 | 冻结批次是否支持分多次结算 | 第一版整笔，以后再加 |
| 大额复核的人员 | 小团队里"有出款权限的人"不足两个会卡住大额出款（审计规范已提示） | 运营上至少配两位有出款权限的管理员 |

## 评审要点（给我自己用）

| 任务 | 重点 |
|---|---|
| L-04 | 后置钩子是否同事务、重放不重复执行；hold 触发器是否真的挡住直接 SQL；`Freeze/Settle/RefundFrozen` 是否真收口；冻结余额 = 未结清 hold 之和 的随机序列；锁顺序是否统一 |
| L-05 | 状态机全路径与触发器；金额/钱包取自提现行；大额无复核不能 paid（数据库 CHECK）；并发出款/撤销/失败只成功一个；收款账户全文不进日志/事件/错误；账号冻结 fail-closed；`bank_ref` 唯一；PENDING_PAYOUT = 未终态提现之和 |
| L-06 | 白名单是否被悄悄扩大；并发冲销只成功一次；对称性是否真有数据库兜底（L-01 遗留）；冲销不能让客户账户为负；有没有直接写 audit schema |
