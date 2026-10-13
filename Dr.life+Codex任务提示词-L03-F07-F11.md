# Codex 任务提示词：L-03（业务入账封装）、F-07（多语言基础）、F-11（可观测性）

**用法：** 整块复制代码块内容粘贴给 Codex。三个任务**可以同时开**，各用独立 worktree，基线 `origin/main` 最新提交（P0-1、L-02 合并后；若 #19/#20 还没合并，先合并再开）。
**文件互不重叠：** L-03 只动 `backend/internal/ledger/`；F-07 只动 `packages/i18n`、`packages/ui` 与各前端应用的词条使用；F-11 只动 `backend/internal/platform/observability/`、`deploy/observability/` 与 `server`/`config` 的最小接线。
**本批没写的：** A-02（等 P0-1 真机验收通过）、A-03（依赖 A-02）、F-09（等托管机柜就绪）。
合并前自己确认 `ci-gate` 是绿的。

---

## L-03 业务入账封装（6.1–6.19，不含冲销）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。L-01、L-02 已合并。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】L-03 业务入账封装
【分支】task/L-03-ledger-business-posting，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（第 4 节账务 1–7 条、规则 8、16）
2. Dr.life+数据库表结构与账务详细设计.md：第三章（科目与恒等式）、第五章、**第六章全部（6.1–6.19 逐条）**、第十一章（账务测试清单）
3. Dr.life+开发任务拆分与验收.md：L-03、L-04、L-05、L-06 四行（本任务**不包含**冲销 `Reverse`（L-06）、冻结明细表写入（L-04）、提现表与复核（L-05））
4. backend/migrations/00004_ledger_tables.sql 与 00005_ledger_account_hardening.sql（**已合并的真实约束，以它为准**）
5. backend/internal/ledger/post.go、query.go、outbox.go 与 post_integration_test.go（L-02 的接口与测试风格）

【背景】
L-02 提供了通用的 `Post()`。业务代码不应该手写分录，应当调用"每类业务一个强类型方法"，方法内部生成分录、幂等键、校验业务规则，再调用 `Post()`。这样"一种业务对应哪几条分录"只在一个地方定义，并且能逐条对照第六章表格测试。

【文件归属】只改 backend/internal/ledger/（新增文件，如 business.go、allocate.go、accounts.go 与对应测试）；允许新增迁移 00006_*.sql（仅在确有必要时，见下"账户开立"）；允许更新《数据库表结构与账务详细设计》第六章的说明（见"待确认"）。不要改 iam、不要新增 HTTP 接口、不要写 ledger.hold / ledger.withdrawal 的业务逻辑（L-04、L-05）。

【先处理两个 L-02 遗留小问题（独立小提交）】
1. 重放结果不应依赖 outbox：`loadExisting` 与 `GetByID/GetByIdemKey` 目前从 outbox.payload 取余额快照，将来 outbox 被清理会导致已入账的幂等键重放报 "no rows"。改为：outbox 缺失时仍返回 txn id、txn_no、`Replayed=true`，`Balances` 为空，**不得报错或误当成"不存在"而重复入账**；加测试（手工删除 outbox 行后重放）。（注意：为了测试可手工删除，需用管理员连接；应用角色没有 DELETE 权限，这是对的。）
2. 修正 post_integration_test.go 里 `admin.Config().MaxConns = 64` 不生效的问题（`Config()` 返回副本）；改为用 pgxpool.ParseConfig 设置 MaxConns 后再创建连接池，使 1000 并发测试真正使用更大的连接池；并在 PR 说明里记录新旧性能数据。

【账户开立（先做）】
- `EnsureWallet(ctx, ownerAccountID int64, merchantID *int64)`：幂等地开立 WALLET_AVAIL、WALLET_FROZEN（商户另加 AD_BUDGET），利用 `UNIQUE NULLS NOT DISTINCT (kind, owner_account)` 与 `ON CONFLICT DO NOTHING`，并发安全（100 并发开立同一用户只产生一组账户）；返回账户 id 集合。
- 单例账户（BANK_DEDICATED、PLATFORM_HELD、PAYABLE_CARRIER、SUSPENSE、PENDING_PAYOUT、DELIVERY_ESCROW）按 kind 查询并缓存 id（启动时一次，不要每次入账都查）。
- 注意：`ledger_rw` 对 ledger.account 只能 INSERT/SELECT 和更新 status；开立账户用 INSERT 即可，不要为此放宽权限。

【业务方法（强类型，金额 int64 单位 0.0001 美元，禁止 float）】
每个方法：参数用结构体、校验业务前置条件、生成固定的幂等键与 ref_type/ref_id、调用 `Post()`、返回 `PostResult`。**分录必须与第六章表格逐条一致。**

| 方法 | txn.type | 分录 | 幂等键规则 | 额外校验 |
|---|---|---|---|---|
| Topup | topup | DR BANK_DEDICATED / CR WALLET_AVAIL(A) | `topup:bankline:<bank_statement_line.id 或人工确认单号>` | 只允许在"银行确认到账"后调用；不确定入金走 BankDiff 入 SUSPENSE |
| Freeze | freeze | DR WALLET_AVAIL(buyer) / CR WALLET_FROZEN(buyer) | `freeze:hold:<ref_type>-<ref_id>` | 余额不足返回 ErrInsufficientFunds；**只写分录**，hold 表写入留给 L-04 |
| Settle | settle | DR WALLET_FROZEN(buyer) / CR WALLET_AVAIL(payee)；若 fee>0 再 CR PLATFORM_HELD(category=settle_fee)，收款方少收 fee | `settle:hold:<id>` | **第一版 fee 恒为 0，但 fee 参数与分录逻辑要有并测试**；fee<0 或 fee>金额拒绝 |
| RefundFrozen | refund | DR WALLET_FROZEN(buyer) / CR WALLET_AVAIL(buyer) | `refund:hold:<id>` | 参数含裁决人 ≠ 出款人（SoD）：两人相同直接拒绝 |
| WithdrawReq | withdraw_req | DR WALLET_AVAIL(A) / CR PENDING_PAYOUT（单例） | `withdraw_req:withdrawal:<id>` | 账号冻结/停用的检查在 L-05；本方法只记账 |
| Payout | payout | DR PENDING_PAYOUT / CR BANK_DEDICATED | `payout:withdrawal:<id>` | 以银行回单为准；金额必须等于原提现 |
| PayoutFail | payout_fail | DR PENDING_PAYOUT / CR WALLET_AVAIL(A) | `payout_fail:withdrawal:<id>` | |
| StorageFee | storage_fee | DR WALLET_AVAIL(M) / CR PLATFORM_HELD(category=storage) | `storage_fee:bill:<id>` | **余额不足时不得扣成负数**：返回专门的 `ErrInsufficientFunds`，调用方据此把账单标为欠费；整笔扣款，不做部分扣款 |
| DeliveryHold | delivery_hold | DR WALLET_AVAIL(M) / CR DELIVERY_ESCROW | `delivery_hold:order:<id>` | 商户资金不足不能发货 |
| DeliveryRelease | delivery_release | DR DELIVERY_ESCROW / CR WALLET_AVAIL(D)（司机份）+ CR PLATFORM_HELD(category=delivery_margin) + CR PAYABLE_CARRIER（中通份） | `delivery_release:order:<id>` | 各份用 `Allocate`（见下）；必须带"签收凭证编号"参数且非空；**该订单的托管余额检查见下** |
| DeliveryRefund | delivery_refund | DR DELIVERY_ESCROW / CR WALLET_AVAIL(M) | `delivery_refund:order:<id>` | 仅当该订单尚未做过 DeliveryRelease（同一订单 release 与 refund 互斥） |
| AdCharge | ad_charge | DR WALLET_AVAIL(M) / CR AD_BUDGET(M) | `ad_charge:ad:<id>[:<seq>]` | |
| AdSpend | ad_spend | DR AD_BUDGET(M) / CR PLATFORM_HELD(category=ads) | `ad_spend:ad:<id>:<bucket>`（bucket 为小时或日合并键，如 20261013H09） | 合并键保证同一广告同一时段只入账一次 |
| AdRefund | ad_refund | DR AD_BUDGET(M) / CR WALLET_AVAIL(M) | `ad_refund:ad:<id>` | |
| MemberFee | member_fee | DR WALLET_AVAIL(A) / CR PLATFORM_HELD(category=member) | `member_fee:member:<id>:<period>` | ⚠️ 数字商品支付规则待核对：实现方法但在代码注释和 PR 里标注"待确认，暂不对外开放" |
| Sweep | sweep | DR PLATFORM_HELD / CR BANK_DEDICATED | `sweep:sweep:<id>` | 金额 ≤ PLATFORM_HELD 余额（由 Post 的非负规则保证）；参数必须含复核人且 ≠ 发起人，写入 note 或 ref 以留痕 |
| CarrierPay | carrier_pay | DR PAYABLE_CARRIER / CR BANK_DEDICATED | `carrier_pay:statement:<id>` | 同样要求复核人 ≠ 发起人；PAYABLE_CARRIER 允许暂时为负的现有策略不变 |
| BankDiff | bank_diff | 差异入 SUSPENSE：银行多出（未识别入金）为 DR BANK_DEDICATED / CR SUSPENSE；银行少了为 DR SUSPENSE / CR BANK_DEDICATED | `bank_diff:bankline:<id>` | 参数含方向与理由（非空） |

**不在本任务内：** `Reverse`（L-06）、hold 表写入（L-04）、withdrawal 表写入与大额复核状态机（L-05）。

【按单托管余额检查（DeliveryRelease / DeliveryRefund）】
`DELIVERY_ESCROW` 是**单例账户**，余额是所有订单托管之和。仅靠 Post 的非负检查，**某一订单可能多放款（用了别的订单的托管）而总余额仍为正**。因此在 DeliveryRelease/DeliveryRefund 的同一事务里（已按序锁住 DELIVERY_ESCROW 的 balance 行，所以串行），查询该订单（ref_type='order', ref_id）在 DELIVERY_ESCROW 上已入账的净托管（贷方合计 − 借方合计），要求"本次放款/退回金额 ≤ 该订单净托管余额"，否则返回 `ErrEscrowExceeded`，不记账。需要在 L-02 的 `Post()` 内部提供"事务内回调/钩子"或抽出可复用的内部函数，让这项检查与入账在同一事务；**不要**在事务外检查再入账（有并发窗口）。写并发测试：同一订单并发 release 与 refund 只有一个成功；订单 A 的托管不能被订单 B 的放款动用。
同样原则适用于 PENDING_PAYOUT（单例）：按提现单的校验属于 L-05，这里在 PR 说明里列为"依赖 L-05 的按单检查"。

【分账 `Allocate`】
纯函数 `Allocate(total int64, shares []Share) ([]int64, error)`：
- Share 用**基点（万分比）**表示比例，各份比例之和必须 = 10000；指定**一份为"余额份"**（最后一项倒挤）；其余份 = floor(total × bp / 10000)，**余额份 = total − 其余之和**；所有份之和**恒等于 total**，任何份不得为负。
- `total × bp` 可能溢出 int64（total 最大约 9.2e18）：用 `math/bits.Mul64/Div64` 或 `math/big`，不得用 float，不得静默回绕。
- 属性测试（testing/quick 或 rapid 类库——优先标准库）：随机 total（含 1、2、3、奇数、质数、10^k±1、math.MaxInt64）× 随机比例，断言 ①之和 = total ②无负数 ③余额份 ≥ 0 ④同输入同输出（确定性）。
- DeliveryRelease 的默认分配：平台留存默认 0；自建配送时司机份 + 平台份（默认 0）；中通配送时 PAYABLE_CARRIER 份。比例由调用方（物流模块）传入，**本任务不硬编码任何费率**（规则 16：0% 佣金等承诺不要"顺手"改）。金额为 0 的份**不生成分录**（posting.amount 必须 > 0）。

【测试（真实 PostgreSQL，testcontainers，`-race`）】
1. **逐条对照表**：表驱动，每个方法一行用例，断言生成的分录（账户 kind、方向、金额、category）与第六章表格逐字对应，并且 txn.type、idem_key、ref 正确。
2. **幂等**：每个方法重复调用（含 100 并发）只入账一次；同键内容不同报 ErrIdempotencyConflict。
3. **余额不足**：Freeze、WithdrawReq、StorageFee、DeliveryHold、AdCharge、MemberFee、Sweep 余额不足都返回 ErrInsufficientFunds 且不留痕；StorageFee 不会把余额扣成负数。
4. **分账**：Allocate 属性测试 + DeliveryRelease 端到端（奇数金额、金额 1、极大金额），之和等于托管金额；分账后托管账户该订单净额回零。
5. **按单托管**：见上，含并发。
6. **恒等式**：每个场景结束后断言：所有贷方正常账户余额合计（含 SUSPENSE，可为负）= BANK_DEDICATED 余额（客户负债合计 + 平台自有 + 应付 + 过渡 = 银行专用账户）。再加一个**随机业务序列测试**（固定随机种子，数百步随机调用上面的方法，失败的步骤要么返回业务错误要么回滚），每步后恒等式都成立、没有账户为负（允许为负的除外）。
7. **SoD**：RefundFrozen、Sweep、CarrierPay 发起人 = 复核/裁决人被拒绝。
8. **EnsureWallet**：100 并发只产生一组账户；重复调用返回同一批 id。
9. **L-02 遗留修复**的两个测试。
10. 用 `app_ledger` 登录身份跑一遍主要业务方法，证明收紧后的权限够用。

【验收标准】
- 每个方法的分录与第六章表格一致（表驱动逐条对照）；
- 分账之和恒等于总额（含奇数/极小/极大/舍入边界）；
- 每个方法有定义并测试的幂等键规则；
- 恒等式在随机序列下恒成立；
- 全仓 `go vet`、`go test -race`、真实 PostgreSQL 集成测试、`ci-gate` 全绿；没有 float，没有 HTTP 接口，没有写 hold/withdrawal 表。

【待确认（不要自己拍板，按已合并 SQL 实现并在 PR 里列出）】
- 设计文档第六章把 6.5–6.7 写成 `PENDING_PAYOUT(A)`（按用户），但已合并的 00004 把 `PENDING_PAYOUT` 与 `DELIVERY_ESCROW` 定义为**单例**（owner_account 必须为空）。本任务按**单例**实现，用 ledger.withdrawal / 订单 ref 做明细；PR 里请列出"文档与 SQL 不一致处"，建议同步更新第六章措辞。
- 商户钱包的 owner_account 取值：用商户主账号的 iam.account.id，同时填 merchant_id？还是别的？先按"owner_account = 商户主账号 id，merchant_id = iam.merchant.id"实现并列为待确认。
- MemberFee 的支付规则（Telegram 数字商品规则）未核对。

【红线】不改 00004/00005 已合并迁移；不放宽任何触发器与权限；不用 float；不新增跨模块依赖；测试里只用占位金额与账号。

【交付】PR 描述：每个方法的分录对照表、幂等键清单、按单托管检查的实现方式（事务内钩子的设计）、两个遗留问题的修复与新旧性能数据、对设计文档的改动与待确认事项。
```

---

## F-07 多语言基础（在 F-06 已有骨架之上完善）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。F-06 已合并（pnpm 工作区、packages/i18n 的 zh/en/km 词条与 check-i18n 脚本、packages/ui、五个应用壳）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】F-07 多语言基础
【分支】task/F-07-i18n-foundation，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（规则 15；第 6 节"无硬编码字符串"）
2. Dr.life+交互文档.md 中语言选择/语言登记相关章节；prototype/index.html 中的词条表 `T`、`A(zh,en)` 辅助函数与语言切换逻辑（行为参照，**只迁移通用基础词条**，不要整表搬运）
3. Dr.life+开发任务拆分与验收.md F-06、F-07 两行
4. packages/i18n（index.tsx、locales/*.json、scripts/check-i18n.mjs、README）与 packages/ui

【背景】
F-06 搭好了词条与检查脚本的骨架。F-07 把它做成可以放心依赖的基础：缺词条在构建与 CI 里失败、高棉语排版规范落地、基础词条从原型迁移、语言解析与切换行为明确。

【文件归属】packages/i18n、packages/ui（字体与排版样式）、各 apps/* 里仅限"把硬编码字符串改成词条"的最小改动；允许更新 packages/i18n/README.md 与 apps/README.md。不要接后端接口（语言登记是 A-04）。

【范围——要做】
1. **词条结构与命名规范**：保持扁平 key（`模块.含义`），README 写清命名规则、占位符规则（`{name}`，三语占位符必须一致）、复数/量词处理的约定（中文无复数；英文与高棉语用显式 key 区分，如 `order.count.one/other`，**不要引入重型 ICU 库**，写一个小的 `plural` 辅助即可，并测试）。
2. **构建失败**：`check-i18n` 接入每个应用与 packages 的 `build`（`prebuild`/等价机制）和根级 `pnpm i18n:check`；缺 key、多余 key、空值、占位符不一致任一情况构建失败。保留现有的 `TODO-KM-REVIEW` 摘要（高棉语待母语审核的标记机制不要破坏，新增的高棉语译文如果是机器/草案翻译，必须带该标记）。
3. **类型安全**：`TranslationKey` 由 en.json 推导；传入不存在的 key 编译失败；占位符参数能在类型上提示（可选，做不到就在 README 说明）。
4. **语言解析**：沿用 `resolveLanguage(accountLanguage, telegramLanguage)`；补充测试：`zh-CN`/`zh_TW`/`km-KH`/`EN-us`/空值/未知语言 → 合理回退；回退顺序写进 README（账户语言 > Telegram 语言 > en）。
5. **高棉语排版规范（packages/ui 样式 + README）**：
   - 字体栈：中文、英文、高棉语分别指定系统字体回退（例如高棉语优先 `Noto Sans Khmer`，再 `Khmer UI`/`Leelawadee UI`、系统 sans-serif）；**不要从 Google Fonts 等外部 CDN 加载**（生产不得依赖外部 CDN）；如需自带字体文件，说明许可并放在仓库内、控制体积（先不引入大字体，只定义回退并在 README 列出"待决策：是否自托管 Noto Sans Khmer 子集"）；
   - 高棉语行高比中英文大（建议 `:lang(km)` 下 line-height ≥ 1.7，标题 ≥ 1.5），避免上下标音符被截；
   - 按钮、标签、导航不得使用固定宽度或 `overflow:hidden; text-overflow: ellipsis` 截断关键文字；允许换行或自适应宽度；
   - 根元素 `lang` 属性随语言切换更新（已有则保持）。
6. **基础词条迁移**：从原型迁移通用基础词条（通用按钮：确认/取消/返回/保存/删除/编辑/搜索/刷新/提交/关闭；状态：加载中/暂无数据/网络错误/重试/操作成功/操作失败；表单校验通用文案；底部导航与角色名称；语言名；错误提示通用文案（对应后端错误码 1001–1006、2001、2002 的**用户可读三语文案**，不暴露内部细节）。每条三语齐全；**高棉语译文如无把握，按 TODO-KM-REVIEW 标记，不要假装是定稿**。
7. **去硬编码**：扫描 apps/* 与 packages/ui 中残留的硬编码用户可见字符串（含 `aria-label`、`title`、`placeholder`、`alt`），全部改走 `t()`；加一个脚本检查（可放入 check-i18n 或独立脚本）：在 `.tsx` 里检测 JSX 文本节点与上述属性中的 CJK/高棉字符字面量，命中即报错（白名单：词条文件、测试、`language.*` 语言自称名）。
8. **语言切换组件**（packages/ui）：三语切换控件，切换后立即生效并更新 `document.documentElement.lang`；**本任务只在本地持久化偏好不上报后端**（用 localStorage，读写包 try/catch，不可用时静默回退）；注意"每个账户登记一种语言"的最终产品规则由 A-04 + 后端实现，本组件要预留 `onChange` 回调供以后上报。
9. **高棉语截断检查**：写一个测试页或 Vitest + 轻量 DOM 检查（jsdom 无法真排版，则用 Playwright 之类会引入重依赖——**不要为此引入 Playwright**），做法二选一：(a) 用 vite 构建一个"词条总览页"（仅开发用），列出所有词条在三语下的渲染，README 写明人工目检步骤；(b) 静态规则检查（上面第 5、7 条的 CSS/字面量规则）+ 长度比规则：km 词条长度超过 en 的 2.5 倍或超过 60 字符时给出**警告**（不失败），提示设计注意宽度。

【验收标准】
① 缺任一语言词条（或多余、空值、占位符不一致）→ 各应用 `pnpm build` 与 `pnpm i18n:check` 均失败（写一个负向测试证明）；
② 高棉语排版规范落地（样式 + README），按钮/导航无固定宽度截断；
③ 基础词条已从原型迁移，三语齐全，`TODO-KM-REVIEW` 摘要仍输出；
④ 去硬编码检查脚本接入 CI（frontend-check），且当前代码库通过；
⑤ 语言解析与回退、plural 辅助、切换组件有测试；
⑥ `pnpm typecheck`、`pnpm lint`、`pnpm test`、`pnpm build` 全绿；没有新增外部 CDN 依赖；
⑦ 依赖版本固定且锁文件更新；新增依赖需说明理由（尽量不加）。

【红线】不要整表搬原型词条；不要把高棉语机器译文当成定稿；不要引入外部字体 CDN；不要碰后端与 OpenAPI。

【交付】PR 描述：词条数量统计（zh/en/km）、新增的 TODO-KM-REVIEW 清单、排版规范摘要、去硬编码检查的规则与白名单、待决策项（字体自托管）。
```

---

## F-11 可观测性

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。F-02、F-05、F-10 等已合并。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】F-11 可观测性
【分支】task/F-11-observability，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（规则 11、14；第 5 节"不要提交真实密钥"）
2. Dr.life+技术方案与系统边界.md 第十一章（非功能指标，含"可观测"行）与第十二章
3. Dr.life+开发任务拆分与验收.md F-11 一行
4. backend/internal/platform/server/server.go、httpx/（RequestID、RequestLogger、Recover）、logx/redact.go、gateway/（注意：网关对非 /v1 路径 fail-closed，只放行 /healthz、/readyz）、config/config.go、cmd/server/main.go

【背景】
上线后要能回答三个问题：现在是否健康（指标）、哪里慢/哪里错（延迟与错误）、某个报错对应哪次请求（请求编号）。本任务搭建观测的"管道"和"仪表盘"，业务指标先留好埋点位，具体业务代码以后接入。

【文件归属】backend/internal/platform/observability/（新增）、deploy/observability/（新增：Prometheus、Grafana 配置与仪表盘 JSON、本地 compose）、server/config/main 的最小接线；更新 backend/README 或新增 docs 说明；允许在《技术方案与系统边界》第十一章末尾追加"指标清单"链接说明。不要接真实 Sentry DSN、不要提交任何密钥。

【范围——要做】
1. **Prometheus 指标（`prometheus/client_golang`，版本固定）**：
   - HTTP：请求总数、请求延迟直方图（桶要覆盖 P95 < 500 ms 与写操作 < 1 s 的判断）、错误数（按状态码类别）、进行中请求数；标签只用 **角色（buyer/merchant/driver/wms/admin/public/其他）、路由模式（chi 的 RoutePattern，如 `/v1/buyer/orders/{id}`，**绝不能用带 id 的原始路径**，防止基数爆炸）、方法、状态码类别**；未匹配路由统一标成 `unmatched`。
   - 网关：限流命中次数（按规则类别）、鉴权失败次数（401/403）。
   - 数据库：pgx 连接池指标（获取等待、使用中/空闲连接数）与慢查询计数（阈值可配置，默认 200 ms；用 pgx 的 tracer/等价钩子；**日志与指标里不得出现 SQL 参数值**，只记录语句指纹或命名查询）。
   - Go 运行时与进程指标（默认采集器）。
   - **业务指标埋点位（定义并注册，暂不接业务）**：订单创建、签收、提现申请、提现出款、AI 请求与 AI 回退次数（按层级）、Bot 发送成功/失败（按端、按 Bot 序号、失败原因）、登录校验失败（按端与原因）、账务入账次数与失败原因、账务对账不一致次数。提供一个小的 `observability.Business` 接口/结构体，业务模块以后只调用 `Business.OrderCreated()` 之类的方法；本任务给每个方法写一个调用测试。
2. **`/metrics` 的暴露方式（重要）**：网关对非 /v1 路径 fail-closed，**不要**为了暴露指标放宽网关规则，也**不要**把 /metrics 挂在对外端口上。做法：单独的内部监听地址（环境变量 `METRICS_ADDR`，默认 `127.0.0.1:9100`；为空则关闭），独立的 `http.Server`（有 ReadHeaderTimeout 等超时），只提供 `/metrics`；README 写明生产由 Prometheus 通过内网或 WireGuard 抓取，**不经 Cloudflare Tunnel 暴露**。测试：对外主端口请求 /metrics 得到统一的 403/404；内部端口能取到指标；`METRICS_ADDR` 为空时不监听。
3. **请求编号贯穿**：沿用现有 RequestID 中间件；确保 ① 响应头返回 X-Request-ID；② 请求日志（slog JSON）带 request_id；③ panic 恢复、未处理错误、Sentry 事件都带同一 request_id；④ 客户端传入的 X-Request-ID 做长度与字符校验（防日志注入），不合规则重新生成。
4. **Sentry（`getsentry/sentry-go`，版本固定）**：DSN 由环境变量 `SENTRY_DSN` 注入，**为空则完全不初始化、不发任何请求**；初始化带环境名（`APP_ENV`）与发布版本；`BeforeSend` 里**脱敏**：移除 Authorization/Cookie/Idempotency-Key 头、请求体、查询参数中的 init_data/token/phone 等；用户标识只保留角色与账号 public_id（不带手机号、姓名、IP 原文可去掉或哈希）；每个事件打 request_id 标签；panic 中间件上报后仍返回统一 500 响应。测试：用假的 transport 验证脱敏（给事件塞进手机号、Authorization、init_data，断言发出的事件里没有）。**不要在测试里访问真实 Sentry。**
5. **日志**：确认 logx 的脱敏仍生效（密钥、完整手机号、聊天原文不落日志）；新增日志字段不得破坏它；写一个回归测试，向日志里写入含手机号/令牌形态的字段并断言被打码。
6. **Grafana 仪表盘与告警规则（deploy/observability/）**：
   - `compose.yaml`（本地 Prometheus + Grafana，镜像版本固定，仅本地开发使用，不含任何真实凭据；Grafana 管理员口令用环境变量占位）。
   - Prometheus 抓取配置、Grafana 数据源与仪表盘 provisioning（JSON，纳入版本管理）。
   - 仪表盘至少包含第十一章列出的核心指标：请求量、延迟（P50/P95/P99，常规查询 P95 < 500 ms 与写操作 P95 < 1 s 的阈值线）、错误率；业务指标面板：下单、签收、提现、AI 回退率、Bot 发送成功率、登录校验失败率；账务面板（入账数、失败原因、对账不一致）；数据库面板（连接池、慢查询）。
   - Prometheus 告警规则：5xx 错误率、P95 超阈值、登录校验失败率突增、Bot 发送成功率下降、账务对账不一致（任何一次即告警）、慢查询突增。写成规则文件并用 `promtool check rules` 校验（容器里跑，写进 README 命令；CI 里加一个轻量检查步骤如能做到，不强求）。
7. **文档**：deploy/observability/README.md：怎么本地启动、指标清单与标签说明、基数规则、/metrics 暴露约束、Sentry 配置与脱敏规则、日志字段约定。

【验收标准】
① Grafana 仪表盘（JSON provisioning）包含第十一章核心指标，并能在本地 compose 启动后看到数据（给出一段本地验证步骤与截图说明或指标示例输出）；
② 任意 500 错误能凭响应头 X-Request-ID 在日志与 Sentry（假 transport 测试）中找到同一编号；
③ `/metrics` 只在内部监听，不经对外网关暴露；指标标签无高基数（测试：请求 1000 个不同 id 的路径，时间序列数量不增长）；
④ Sentry 为空 DSN 时零外联；脱敏测试通过；
⑤ 慢查询与连接池指标可用，日志中无 SQL 参数值；
⑥ `go vet`、`go test -race`、OpenAPI 无差异、`ci-gate` 全绿；govulncheck 无新增高危；
⑦ 新增依赖版本固定、说明理由。

【红线】不提交任何 DSN、口令、令牌；不放宽网关规则；不在指标标签里放账号、手机号、订单号、原始路径；性能开销要克制（指标中间件的基准测试结果写进 PR：每请求额外延迟与分配数）。

【交付】PR 描述：指标清单（名称、类型、标签）、/metrics 暴露方案、脱敏规则与测试、仪表盘截图说明或本地验证输出、基准数据、遗留事项（前端 Sentry、日志收集 Loki/ELK 选型、告警通知渠道）。
```

---

## 评审要点（给我自己用）

| 任务 | 重点 |
|---|---|
| L-03 | 分录与第六章逐条一致；**按单托管检查是否在同一事务内**（不是事务外查再入账）；Allocate 溢出与余额份；恒等式随机序列；SoD；StorageFee 不扣成负数；PENDING_PAYOUT/ESCROW 单例与文档不一致是否如实列出；MemberFee 是否标"待确认"；没有改 00004/00005；没有写 hold/withdrawal |
| F-07 | 缺词条是否真的让**每个应用的 build** 失败（负向测试）；有没有引入外部字体 CDN 或 Playwright；高棉语行高/不截断；去硬编码脚本白名单是否过宽；新增高棉语是否带 TODO-KM-REVIEW；有没有整表搬原型 |
| F-11 | /metrics 是否在对外端口；标签基数（原始路径）；Sentry 空 DSN 零外联、脱敏测试；慢查询日志有没有 SQL 参数；dashboard 是否含第十一章全部指标；基准开销 |
