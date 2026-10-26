# Codex 任务提示词：P0-1（Telegram initData + Bot 池 PoC）、L-02（`Post()` 入账核心）

**用法：** 整块复制代码块内容粘贴给 Codex。两个任务可以同时开，各用独立 worktree，基线 `origin/main` 最新提交（A-01、L-01、F-06 已合并）。
**合并前自己确认 `ci-gate` 是绿的**（GitHub 免费版不强制分支保护）。

---

## 你（人）要先准备的：六个测试 Bot（P0-1 用）

| # | 端 | 池内角色 | 建议用户名（示例，自己取） | 环境变量 |
|---|---|---|---|---|
| 1 | 买家 | 主 | `drlife_test_buyer_bot` | `POC_TG_BUYER_PRIMARY_TOKEN` |
| 2 | 买家 | 备用 | `drlife_test_buyer2_bot` | `POC_TG_BUYER_STANDBY_TOKEN` |
| 3 | 商家 | 主 | `drlife_test_merchant_bot` | `POC_TG_MERCHANT_PRIMARY_TOKEN` |
| 4 | 商家 | 备用 | `drlife_test_merchant2_bot` | `POC_TG_MERCHANT_STANDBY_TOKEN` |
| 5 | 司机 | 主 | `drlife_test_driver_bot` | `POC_TG_DRIVER_PRIMARY_TOKEN` |
| 6 | 司机 | 备用 | `drlife_test_driver2_bot` | `POC_TG_DRIVER_STANDBY_TOKEN` |

准备步骤：
1. 在 @BotFather 用 `/newbot` 建六个**测试专用** Bot（不要用正式 Bot）。
2. 令牌**只放在本机环境变量或不入库的 `poc/telegram-initdata/.env.local`**，不要贴进对话、提示词、仓库、截图。
3. 至少给其中一个 Bot（建议买家主 Bot）用 `/newapp` 或菜单按钮（`/setmenubutton`）配置一个 Mini App，网址指向你能访问的 **HTTPS** 测试页（可用 Cloudflare Tunnel 临时域名）；测试页由 Codex 提供静态文件，你负责暴露 HTTPS。
4. 准备一台**真机**（装有 Telegram）：验收①需要真实客户端产生的 initData，模拟器/脚本伪造的不算。
5. 想验证"主 Bot 令牌作废"：在 BotFather 对买家主 Bot 执行 `/revoke`（会让旧令牌失效），再用新令牌更新环境变量。

---

## P0-1 Telegram initData 校验 + Bot 池

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】P0-1 Telegram initData 校验 + Bot 池（技术验证，PoC）
【分支】task/P0-1-telegram-initdata，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（规则 11、13、14）
2. Dr.life+第一版交付形态与技术架构.md 的"登录（Telegram initData + Bot 池）"相关章节
3. Dr.life+技术方案与系统边界.md 第十三（前置）章 PoC 清单第 1 项
4. Dr.life+开发任务拆分与验收.md 中 P0-1 与 A-02 两行（A-02 会复用本 PoC 的结论）
5. Telegram 官方文档（Mini Apps → Validating data received via the Mini App）。**以官方文档当前内容为准，不要凭记忆实现；实现与文档有出入时在结论里写明。**

【背景】
每个端（买家/商家/司机）有一个 Bot 池（主 + 备用）。服务端必须对该端池内**所有**令牌逐个校验 initData，命中任一即视为合法；账号按 Telegram 用户编号 + 角色保存，不绑定某个 Bot。目的：某个 Bot 被风控或令牌作废时，该端用户的数据与登录不会整体失效。

【文件归属】只新增 poc/telegram-initdata/（独立 Go module 或放 backend 之外，不改 backend/ 生产代码、不改 openapi.yaml、不改迁移）。结论写入 poc/telegram-initdata/RESULT.md，并在 PR 说明里摘要。

【范围——要做】
1. 校验核心（纯函数，可单测）：
   - 按官方算法：secret = HMAC_SHA256(key="WebAppData", msg=bot_token)；data_check_string 按字段名字典序、"key=value" 以换行连接（排除 hash 字段本身；注意官方对 signature 字段的最新说明，按文档处理）；hash 比较必须**恒定时间**比较（hmac.Equal / subtle.ConstantTimeCompare）。
   - 解析 user（JSON）、auth_date；auth_date 过期（默认 1 小时，可配置）拒绝；auth_date 在未来超过时钟容差（如 60 秒）也拒绝。
   - 输入长度上限（如 initData ≤ 4 KB），畸形输入不 panic。
2. Bot 池：从配置读取"端 → 令牌列表（含主/备标记）"，对该端池内令牌**逐个**校验，返回命中的 Bot 标识（用"端 + 池内序号/用户名"，**不返回、不记录令牌**）与尝试次数。买家池令牌**不能**通过商家端校验（端与池严格隔离）。
3. 最小服务（Go，chi 或标准库均可）：`POST /poc/auth/verify`，请求体 {end, init_data}，返回 {ok, hit_bot, tried_count, tg_user_id, auth_date}；失败只返回统一错误码，不暴露哪一步失败的内部细节。
4. 备用 Bot 切换演示：提供脚本或测试，演示"主令牌被剔除/作废后，同一端备用 Bot 签发的 initData 仍通过"，以及"主 Bot 签发的旧 initData 在主令牌被剔除后失败"。
5. 一个极简静态测试页（poc/telegram-initdata/web/index.html，单文件，无 CDN 以外外部依赖；如需 telegram-web-app.js 可从官方地址加载并在 RESULT 里写明）：在 Telegram 内打开后显示 initData 的**长度与前 12 字符**，并提供"复制 initData"按钮和"向 /poc/auth/verify 发送"按钮，方便真机验证。页面不得把 initData 发给除 PoC 服务以外的任何地址。
6. 库选型对比（验收⑤）：用 gotgbot 与 go-telegram/bot 各写一个最小 Webhook 接收示例（只回 /start），并对比：Webhook 与 secret_token 校验、Mini App 按钮/菜单支持、维护活跃度（最近版本与更新时间）、依赖体积、测试友好度、群发/限速处理。给出**明确推荐**和理由。若两者对 initData 校验都没有现成可靠实现，说明本 PoC 自写校验的理由。
7. 通知到达率初探：写出验证方案（不必实现）——向 20 个"已 /start 过"的账号发送、统计被拒原因（用户屏蔽、未 /start、429 限速），并给出备用 Bot 触达策略建议（见架构文档 Bot 池一节）。

【验收标准】
① 用测试 Bot 的**真实客户端 initData**（真机，Mini App 内产生）校验通过——把"通过"的证据写进 RESULT.md（不要贴 initData 原文，贴长度、命中的 Bot 序号、tried_count 即可）；
② 篡改任一字段（user、auth_date、query_id 等逐个改）校验失败，表驱动测试覆盖；
③ 过期 auth_date 被拒；未来时间被拒；
④ 主令牌作废/剔除后，备用 Bot 签发的 initData 仍通过；
⑤ gotgbot 与 go-telegram/bot 对比与明确选型结论；
⑥ 跨端隔离：买家池签发的 initData 在商家端校验失败（测试证明）；
⑦ 恒定时间比较、长度上限、畸形输入不 panic（含模糊/边界测试）；
⑧ `go vet`、`go test -race` 通过；CI 全绿。

【红线】
- 令牌只从环境变量或被 .gitignore 的 .env.local 读取；**不得出现在代码、测试夹具、日志、RESULT.md、PR 描述、截图里**。日志不写完整 initData、不写 user 的姓名与手机号（可写 tg_user_id 的后 4 位）。
- 提交前运行 backend/scripts/check-secrets.sh（或仓库已有密钥扫描），确保没有令牌形态字符串（形如 123456789:AA...）。
- 需要真实令牌/真机的验收项，**如果拿不到就明确标"待用户验证"并给出步骤**，不要伪造结果、不要用自己编的 initData 冒充"真实"。
- 不要顺手改生产目录；PoC 代码结论落档后，A-02 才会正式实现。

【交付】PR 描述包含：做了什么、没做什么、哪些验收已验证（附命令与输出摘要）、哪些待用户在真机验证、选型结论、对文档的影响（需要回写技术方案 PoC 记录的内容）。
```

---

## L-02 `Post()` 入账核心

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。A-01、L-01 已合并。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】L-02 `Post()` 入账核心
【分支】task/L-02-ledger-post，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（第 4 节账务 1–7 条全部、规则 8）
2. Dr.life+数据库表结构与账务详细设计.md 第五章（含"入账流程 Post(ctx, req)"与幂等键规则）、第十一章（账务测试清单）
3. Dr.life+开发任务拆分与验收.md 中 P0-2、L-01、L-02、L-03 四行
4. backend/migrations/00004_ledger_tables.sql（已合并的实际约束；以它为准，设计文档有出入时在 PR 里指出）、backend/internal/ledger/ledger_integration_test.go、backend/internal/platform/db/ 现有集成测试风格

【背景】
`ledger` 模块是唯一能改余额的地方，`Post()` 是唯一入口。L-01 已在数据库层放了借贷平衡延迟触发器、只追加触发器、非负约束与撤销权限；L-02 在其上写 Go 的入账函数。L-03（每类业务的强类型封装）不在本任务范围。

【文件归属】backend/internal/ledger/（Go 代码与测试）；允许新增迁移 backend/migrations/00005_ledger_account_hardening.sql（见下"权限收紧"）；允许小幅更新《数据库表结构与账务详细设计》第五章以与实现一致。不要改 iam、不要改其他模块的 schema，其他模块以后只能调用本模块接口。

【范围——要做】
A. 权限收紧与账户不可变（先做，独立迁移 00005，含可逆 Down）
1. `ledger_rw` 对 `ledger.account` **撤销 DELETE、TRUNCATE、TRIGGER、REFERENCES**；UPDATE 只保留必要列（如 status 的受控变更）——用列级授权，**kind、normal_side、owner_account、allow_negative、created_at 等关键属性不可被 `ledger_rw` 修改**。
2. 再加一个 BEFORE UPDATE 触发器兜底：上述关键属性任何角色（含管理员）都不能改，DELETE/TRUNCATE 一律拒绝（SQLSTATE 55000，与 deny_mutation 风格一致）。
3. 检查 `ledger.balance`、`hold`、`withdrawal`、`outbox`、`bank_statement_line`、`snapshot_daily` 上 `ledger_rw` 的权限（目前默认权限给了 TRUNCATE/DELETE 等）：**凡是设计上不该被删除/清空的表，一并撤销**（balance 已禁 DELETE/TRUNCATE；hold/withdrawal/bank_statement_line/snapshot_daily 不应能 DELETE/TRUNCATE；outbox 只允许应用把已发送事件标记状态，不允许 TRUNCATE）。列出你的取舍，PR 里写明理由。
4. 测试（用 `app_ledger` 登录身份，真实 PostgreSQL）：改关键属性 → 42501 或 55000；DELETE/TRUNCATE account → 被拒；管理员也被触发器拒；允许的 status 变更仍可用；Down/Up 往返。

B. `Post(ctx, req)`
请求结构（强类型，金额一律 int64，单位 0.0001 美元；**禁止 float**）：txn 类型（必须在 txn.type 允许列表内）、idem_key、ref_type/ref_id、note、actor_id、occurred_at（可选）、postings []{account_id、side、amount、role、category(可选)}。
返回结构：txn id、txn_no、是否为重复请求（`Replayed bool`）、各账户入账后余额。
流程严格按设计文档第五章：
1. 校验：金额 > 0；postings ≥ 2；借方合计 = 贷方合计 = 请求总额；账户存在且开放；分类字段规则（category 仅 PLATFORM_HELD）；idem_key 非空且格式符合"业务类型:业务单据:动作[:序号]"（长度上限，防注入）；类型合法。校验失败返回明确的哨兵错误（`ErrUnbalanced`、`ErrInvalidAmount`、`ErrAccountNotFound`、`ErrInvalidRequest` 等），**不触库**。
2. 开启事务（READ COMMITTED）。
3. **按 account_id 升序**对涉及的 balance 行 `SELECT … FOR UPDATE`（同一账户在 postings 里出现多次要合并去重后再锁，不得重复加锁或乱序）。
4. 余额检查：对 `allow_negative = false` 的账户，借记使余额减少（按 normal_side 判断方向，不要写死"借减贷增"）后不得小于 0；不足返回 `ErrInsufficientFunds`（对外错误码 `ERR_INSUFFICIENT_FUNDS`），**整笔回滚，不记账**。
5. INSERT txn（带 idem_key）。**幂等：若 idem_key 唯一冲突，读取并返回已有结果（`Replayed = true`），不再入账。** 若同一 idem_key 但请求内容（类型、金额、分录）与已有交易不一致，返回 `ErrIdempotencyConflict`，**不得悄悄当成同一笔**。并发下两个请求同时用同一 idem_key，只能有一个入账，另一个得到相同结果，不能出现 500 或死锁。
6. INSERT posting（批量），UPDATE balance（balance ± amount，version + 1）。
7. COMMIT（延迟触发器兜底验证借贷平衡）；失败整体回滚。
8. 同事务写 `ledger.outbox`（事件：txn 已入账）；提交后的事件投递失败**不得影响已入账**（本任务只写 outbox 并提供"取出待发/标记已发"的函数，不实现具体通知）。
9. 错误处理：区分可重试（序列化失败/死锁 40001、40P01 → 有限次数带退避的自动重试，次数上限可配置）与不可重试；上下文超时必须能中止并回滚。
10. 日志不写完整账户余额明细之外的敏感数据；不得输出 SQL 参数里的备注原文以外的个人信息。

C. 对外接口
`ledger` 包只暴露 `Post()`、查询必要的只读函数（按 txn id / idem_key 取结果）与错误类型；**不暴露任何"直接改余额"的函数**。其他模块的包不得 import ledger 的内部包（用 internal 目录结构保证）。

【测试（真实 PostgreSQL，testcontainers，禁止 mock 数据库；`-race`）——全部必须有】
1. **1000 并发**对同一客户账户扣款：初始余额 X，每次扣 a，最终成功次数 = floor(X/a)，余额 ≥ 0 且与成功次数严格一致，失败的都是 `ErrInsufficientFunds`，**不超扣**；总账借贷之和相等。
2. **同一 idem_key 重复 100 次**（含 100 个 goroutine 同时发出）：只入账一次，其余全部返回同一 txn、`Replayed = true`；账户余额只变化一次。
3. **idem_key 相同但内容不同**：返回 `ErrIdempotencyConflict`，不记账。
4. **交叉账户并发无死锁**：账户 A→B 与 B→A 的转账同时大量并发（含涉及三个以上账户的多分录），全部完成且无 40P01 未被处理；锁顺序证据（日志/断言）显示升序。
5. **借贷不平**：应用层校验就拒绝（`ErrUnbalanced`）；并且**绕过 Post() 直接用 SQL 插入**不平衡交易，在提交时被数据库拒绝（L-01 已有，这里再确认通过 Post 的事务路径不会被绕开）。
6. **`txn.amount` 与借贷分录合计不一致**时在提交阶段失败：单独写一个测试——绕过 Post()，手工插入 txn.amount=100 但借贷各 200 的分录，COMMIT 必须失败（SQLSTATE 23514），并确认没有残留 txn/posting。同时确认通过 Post() 传入"请求总额 ≠ 分录合计"时返回 `ErrUnbalanced`。（注意：此项在上一轮验收里没有核实清楚，请务必做成明确断言。）
7. **余额不为负**：客户资金账户任何路径都不能为负；`allow_negative = true` 的账户（SUSPENSE 等）可以按设计为负。
8. **非法输入**：金额 0、负数、溢出（接近 int64 上限的相加）、重复账户、只有一条分录、未知账户、未知类型、空 idem_key、超长 idem_key。**金额溢出必须被识别（不能静默回绕）**。
9. **事务回滚完整**：中途注入失败（例如借助超时、取消 ctx、或 outbox 写入失败的受控测试），确认 txn/posting/balance/outbox 全部不留痕。
10. **outbox**：每笔成功入账恰有一条 outbox；重复请求不产生第二条；取出待发/标记已发函数并发安全（`FOR UPDATE SKIP LOCKED`）。
11. **权限路径**：测试里用 `app_ledger` 登录身份跑一遍 Post() 的主要路径，证明只靠 SELECT/INSERT/受限 UPDATE 权限就能完成（说明权限收紧没有把应用锁死）。
12. **性能基线（只记录，不设硬门槛）**：1000 并发场景的吞吐与 p95 延迟写进 PR 说明（机器配置也写上），用于以后对比。

【验收标准】
- P0-2 的五条验收全部满足并有对应测试：① 1000 并发不为负；② 同 idem_key 重复 100 次只入账一次；③ 交叉账户无死锁；④ UPDATE/DELETE 流水被拒；⑤ 借贷不平提交失败；
- 重复 idem_key 返回原结果；余额不足返回 `ERR_INSUFFICIENT_FUNDS` 且不记账；outbox 保证事件至少一次；
- `ledger.account` 关键属性不可变，`ledger_rw` 权限收紧并有测试；
- 全仓 `go vet`、`go test -race`、真实 PostgreSQL 集成测试、OpenAPI 无差异检查、CI（`ci-gate`）全绿；
- 没有 float；没有跨模块直接写 ledger 表；没有 HTTP 接口（L-09 才做查询接口）。

【红线】
- 不要改 L-01 已合并的 00004 迁移（只能新增 00005）。
- 不要为了让测试通过而放宽 L-01 的约束或绕过触发器。
- 测试里的账户/金额用占位值，不要出现真实银行信息。
- 发现设计文档与已合并的 SQL 不一致，**不要自己拍板**：按已合并 SQL 实现，并在 PR 说明里列为"待确认"。

【交付】PR 描述包含：权限收紧的取舍清单、`Post()` 的错误类型与重试策略、各测试对应的验收条目、1000 并发性能数据、对设计文档的改动、遗留与待确认事项。
```

---

## 评审要点（给我自己用，下一轮评审照这个查）

| 任务 | 重点 |
|---|---|
| P0-1 | 恒定时间比较；`signature` 字段处理是否与官方文档一致；是否真的逐个令牌校验而不是只试主 Bot；买家池令牌不能通过商家端；日志/RESULT/PR 里有没有令牌或完整 initData；真机验收是否老实标"待用户验证"；选型结论是否有依据 |
| L-02 | 锁顺序（同账户多分录去重）；溢出检查；幂等冲突（内容不一致）是否真的拒绝；并发同键是否 500；余额方向是否按 normal_side 而不是写死；权限收紧是否让 `app_ledger` 仍能跑通；00004 是否被改动；是否出现 float |
