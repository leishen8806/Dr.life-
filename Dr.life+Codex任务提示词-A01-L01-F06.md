# Codex 任务提示词：A-01（iam 表）、L-01（账务表与硬约束）、F-06（前端工作区）

**用法：** 与前几份相同，整块复制代码块内容粘贴给 Codex。三个任务**可以同时开**，各用一个独立 worktree，基线是 `origin/main` 最新提交（F-05、F-10 合并后）。
**避免冲突的约定：**
- **迁移序号固定：** A-01 用 `00003_iam_tables.sql`，L-01 用 `00004_ledger_tables.sql`。两者合并顺序任意；如果后合并的一方发现序号已被占用，按 `backend/migrations/README.md` 的规则重新编号并重跑测试。
- **前端与后端互不干扰：** F-06 只动前端目录和 `pnpm` 相关文件。
- **L-01 是关键路径，请用最严格的评审**（见文末）。目前 GitHub 不强制分支保护，**合并前自己确认 `ci-gate` 是绿的**。

---

## A-01 `iam` 表与迁移

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。F-01～F-05、F-08、F-10、F-12、F-13 已合并。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】A-01 iam 表与迁移
【分支】task/A-01-iam-tables，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（规则 8、11、13、14；第 5 节迁移约定）
2. Dr.life+数据库表结构与账务详细设计.md 第一章（schema、通用约定）、第二章（iam 全部表）、第十章（迁移与种子）
3. Dr.life+平台后台权限与审计规范.md 第二章（角色矩阵）与第二章末尾"职责分离规则"
4. Dr.life+开发任务拆分与验收.md 第五章 A-01 一行
5. backend/migrations/README.md、00001_schemas.sql、00002_roles.sql（现有 schema 与角色）

【背景】
`iam`（账号与权限）模块的数据结构。本任务**只做表、约束、种子数据和测试**，不写 Go 业务代码（登录是 A-02，会话是 A-03，员工逻辑是 A-05，管理员逻辑是 A-06）。

【文件归属】只新增 backend/migrations/00003_iam_tables.sql 和测试文件（放在 backend/internal/iam/ 或 backend/internal/platform/db/ 下已有的集成测试风格旁）；允许在《数据库表结构与账务详细设计》第二章同步更新字段（若与实际 SQL 有出入）。不要改其他迁移、不要碰 ledger。

【范围——要做】
一个迁移文件 00003_iam_tables.sql（含完整的 Up 和 Down，Down 必须真正可逆），在 iam schema 内创建下列表（字段以设计文档第二章为准；未列出的辅助字段按需补，并说明）：
person、account、account_device、bot、account_bot、login_event、session、merchant、merchant_staff、merchant_role_perm、merchant_op_log、account_link、admin_user、admin_role_perm、actor。

关键约束（逐条用数据库约束实现，不要只靠应用代码）：
1. 主键 bigint generated always as identity；对外 public_id text not null unique（由应用生成 ULID，数据库用 CHECK 限制为 26 位 Crockford Base32 字符）；接口不暴露自增 id。
2. 时间一律 timestamptz；created_at、updated_at 默认 now()。
3. person.tg_user_id bigint not null unique（一个 Telegram 号一个人）。
4. account：UNIQUE (person_id, role, merchant_id) **并且要处理 merchant_id 为 NULL 的情况**（PostgreSQL 唯一约束默认把 NULL 视为不相等，会造成重复；请使用 PostgreSQL 16 的 UNIQUE NULLS NOT DISTINCT 或等价的部分唯一索引，并用测试证明买家账号不能重复创建）；role 取值 buyer、merchant_owner、merchant_staff、driver；status 取值 active、frozen、disabled；**status 为 frozen/disabled 时 status_reason 必须非空（CHECK）**；lang 取值 zh、en、km 或空。
5. bot：end 取值 buyer、merchant、driver；status 取值 active、standby、limited、banned；**同一 end 最多一个 primary（部分唯一索引）**；token_enc、webhook_secret_enc 为 bytea（信封加密密文，注释说明用途与 AAD 约定 "bot_token:<id>"）；token_last4 text；username 唯一。
6. account_bot：(account_id, bot_id) 唯一；记录 write_access、unreachable_at。
7. session：refresh_token_hash bytea（只存哈希）；revoked_at、revoked_reason；对 account_id 与 expires_at 建索引。
8. merchant_staff：同一商户内 phone 唯一；invite_code 唯一；role 取值 manager、sales、warehouse、finance；status 取值 invited、active、off。
9. merchant_role_perm：主键 (merchant_id, role, module)；module 取值 dash、prod、orders、store、fin、ads、prof、staff、ai；level 取值 none、view、edit；**module = 'staff' 时 level 只能是 none 或 view（CHECK）**。
10. account_link：同一 (buyer_account_id, merchant_account_id) 在任一时刻最多一条"未解除"记录（部分唯一索引，条件 unlinked_at IS NULL）。
11. admin_user：password_hash 只存哈希（text），totp_secret_enc bytea，role 取值 super、ops、finance、logi、cs、auditor，data_scope text[]，status。
12. admin_role_perm：主键 (role, module)；module 为 17 个后台模块代码：dash、usr、mer、prod、ord、fund、wd、mod、logi、integ、rbac、sens、cfg、fin、aud、ops、disp；level 取值 none、view、edit；**role = 'super' 的所有行 level 必须为 edit（CHECK）**。
13. actor：kind 取值 account、admin、system、external；ref_id；唯一 (kind, ref_id)。
14. 手机号、证件等敏感字段在注释里标明"应用层加密后存放"，列类型用 bytea；本任务不实现加密逻辑（使用 F-10 的信封加密库的任务会负责）。

种子数据（写在同一个迁移里，使用 INSERT，可重复执行不报错）：
- admin_role_perm 的默认矩阵（6 个角色 × 17 个模块），**必须与下表一致**（这是权限与审计规范定稿的默认值，已与原型核对）：

| module | super | ops | finance | logi | cs | auditor |
|---|---|---|---|---|---|---|
| dash | edit | view | view | view | view | view |
| usr | edit | edit | view | none | view | view |
| mer | edit | edit | view | view | view | view |
| prod | edit | edit | none | none | view | view |
| ord | edit | view | view | view | view | view |
| fund | edit | none | edit | none | none | view |
| wd | edit | none | edit | none | none | view |
| mod | edit | edit | none | none | none | view |
| logi | edit | view | none | edit | none | view |
| integ | edit | none | none | view | none | view |
| rbac | edit | none | none | none | none | view |
| sens | edit | none | view | none | view | none |
| cfg | edit | edit | none | none | none | view |
| fin | edit | none | edit | none | none | view |
| aud | edit | none | view | none | none | edit |
| ops | edit | edit | view | view | view | view |
| disp | edit | edit | view | none | none | view |

- merchant_role_perm 的**默认角色权限模板**放在一个"模板表"里（例如 iam.merchant_role_perm_template，字段同 merchant_role_perm 去掉 merchant_id），内容与设计一致：manager 为 veeeveeve，sales 为 veevnnvne，warehouse 为 vvvennvne，finance 为 vnvvevvne（v=view、e=edit、n=none，列顺序依次为 dash、prod、orders、store、fin、ads、prof、staff、ai）；创建商户时由应用复制模板。
- 不创建任何管理员账号、Bot、商户（这些由应用或运维流程创建）。

权限（迁移里用已有的角色体系）：
- iam_rw 对新表自动获得读写（F-03 的默认权限已覆盖，请用测试确认，不要重复授权造成混乱）；其他模块角色无权限；report_ro 只读；
- **不要给 iam_rw 以外的角色任何写权限**；admin_user.password_hash、bot.token_enc 等敏感列对 report_ro 也不开放：请通过"撤销整表 SELECT 再授予非敏感列"或"为报表单独建视图"的方式，二选一，并在 PR 里说明；测试要证明 report_ro 读不到密文列和密码哈希列。

测试（用 testcontainers 起真实 PostgreSQL，沿用 backend/internal/platform/db 里集成测试的风格）：
a) 迁移 up → down → up 都成功；Down 后 iam 下没有残留表；
b) 每条上面的约束至少有一个"违反即失败"的用例（SQLSTATE 断言：23505 唯一、23514 检查、23503 外键等）；
c) 买家账号重复创建（merchant_id 为 NULL）被拒绝；
d) 同一 end 两个 primary Bot 被拒绝；
e) admin_role_perm 种子与上面表格逐格一致（测试里把表格写成数据逐格比对）；super 的 level 不能改成非 edit；
f) 边界测试：用 app_iam 登录身份能读写 iam；用 app_ledger 登录身份访问 iam 被拒（42501）；report_ro 读不到敏感列；
g) 种子可重复执行（再次运行迁移的种子部分不报错、不重复）。

【范围——不要做】
- 不写 Go 业务代码、不写接口、不改 OpenAPI；不创建 ledger 或其他模块的表；不实现加密；不创建演示数据；不改 F-03 的角色与权限迁移。

【验收标准——逐条自测并在 PR 描述里写结果】
□ `make db-up db-migrate` 一条命令迁移成功；`make db-rollback` 一步回滚成功，再迁移成功
□ 15 张表齐全，约束清单 1–14 全部有"违反即失败"的测试
□ NULLS NOT DISTINCT 或等价方案有效（买家账号不重复）
□ admin_role_perm 种子与表格逐格一致；merchant_role_perm_template 与设计一致
□ 敏感列对 report_ro 不可读；跨 schema 边界测试通过
□ 《数据库表结构与账务详细设计》第二章与实际 SQL 一致（如有出入已同步修改并在 PR 里列出）
□ go vet、go test（含 -race 在 Linux 容器或 CI）通过；PR 上的 CI（ci-gate）通过并附 Actions 运行链接与耗时

【如何验证（你会亲自跑；前一步失败就停下报告，不要空转）】
0. 预检：docker version（要同时看到 Client 和 Server）；docker info --format "{{.ServerVersion}} | mem={{.MemTotal}}"（MemTotal ≥ 6 GiB）；docker run --rm hello-world。预检失败：重试不超过 2 次、等待不超过 3 分钟，然后停止并报告；不要用 SQLite/内存库/mock 代替真实 PostgreSQL；依赖 Docker 的验收项逐条标为"未验证（原因）"。
1. cd backend && make db-up db-migrate，然后用 psql 检查 \dt iam.*、\d iam.account 等
2. make db-rollback 再 make db-migrate
3. go test ./...（需要 Docker）
4. make db-down 并确认没有遗留容器

【遇到不确定】
不要猜测，写进 PR 描述"待确认"；与设计文档冲突时以文档为准并提出。

【完成后】
在 PR 描述里按模板填写，并附 5 行以内总结。
```

---

## L-01 账务表、约束与触发器（关键路径，最严格）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。F-01～F-05、F-08、F-10、F-12、F-13 已合并。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】L-01 账务表、约束与触发器
【分支】task/L-01-ledger-tables，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序，务必逐字读完；这是资金核心）】
1. AGENTS.md 第 4 节"账务"规则 1–7 与"边界与安全"规则 8
2. Dr.life+数据库表结构与账务详细设计.md 第四章（账务总则、科目表、对账恒等式、币种与精度）、第五章（账务表结构与关键约束，含 SQL）、第六章（业务动作与分录，理解 account 种类即可）、第十一章（测试清单）
3. Dr.life+开发任务拆分与验收.md 第六章 L-01 一行（含"REVOKE ledger_rw"的提醒）
4. Dr.life+技术方案与系统边界.md 第七章（账务边界规则）
5. backend/migrations/00002_roles.sql（现有角色与"默认权限"）

【背景】
本任务建立账务的**数据库层硬约束**：复式记账表、借贷平衡校验、只增不改不删、非负余额。**这些规则必须由数据库自己保证，不依赖应用代码**，因为应用代码将来可能有 bug。本任务**只做表、约束、触发器、权限、种子账户与测试，不写 Go 的 Post() 入账逻辑**（那是 L-02）。

【文件归属】只新增 backend/migrations/00004_ledger_tables.sql 与测试文件；可更新《数据库表结构与账务详细设计》中与实际 SQL 不一致之处。不要改其他迁移、不要碰 iam。

【重要提醒（来自 F-03 评审的实测结论）】
F-03 的默认权限会自动给 ledger_rw 授予 ledger schema 内**新建表**的 SELECT、INSERT、UPDATE、DELETE、TRUNCATE、REFERENCES、TRIGGER。所以本迁移**必须对 ledger.txn 和 ledger.posting 显式 REVOKE UPDATE, DELETE, TRUNCATE, TRIGGER**，并用测试证明（以 app_ledger 登录身份）。另外：默认权限只对"执行迁移的那个角色"以后创建的对象生效，迁移必须由 MIGRATION_DATABASE_URL 对应的同一角色执行。

【范围——要做】
一个迁移文件 00004_ledger_tables.sql（含完整 Up 和 Down；Down 必须真正可逆，包含删除触发器和函数），在 ledger schema 内：

1. 表（字段以设计文档第五章为准）：
   - ledger.account：kind（9 种：BANK_DEDICATED、WALLET_AVAIL、WALLET_FROZEN、PENDING_PAYOUT、DELIVERY_ESCROW、AD_BUDGET、PLATFORM_HELD、PAYABLE_CARRIER、SUSPENSE）、normal_side（DEBIT/CREDIT，CHECK 与 kind 对应：BANK_DEDICATED 为 DEBIT，其余为 CREDIT；SUSPENSE 请在 PR 里说明你的选择及理由）、owner_account bigint（对应 iam.account.id，**不要建跨 schema 外键**，在注释里说明原因：模块边界）、merchant_id bigint、currency 固定 'USD'（CHECK）、allow_negative、status。
     **唯一性：** 设计文档写的 unique (kind, owner_account) 对 owner_account 为 NULL 的"单例账户"不起作用（NULL 视为不相等，可重复插入）。请改用 UNIQUE NULLS NOT DISTINCT 或部分唯一索引，使每个单例 kind（BANK_DEDICATED、PENDING_PAYOUT、DELIVERY_ESCROW、PLATFORM_HELD、PAYABLE_CARRIER、SUSPENSE）只能有一个，并用测试证明重复插入被拒。AD_BUDGET 与钱包类账户按 owner 唯一。
   - ledger.txn：txn_no 唯一；type 取值（设计文档第六章列出的全部类型）；amount > 0；idem_key not null **unique**；ref_type、ref_id；reverses_txn_id（自引用外键）；actor_id（对应 iam.actor.id，同样**不建跨 schema 外键**）；occurred_at、created_at。
   - ledger.posting：txn_id 外键；account_id 外键；side（DEBIT/CREDIT）；amount > 0；role；category（仅允许 storage、delivery_margin、ads、settle_fee、member，且**只有当该分录的账户 kind 是 PLATFORM_HELD 时才允许非空**——用触发器或等价约束实现并测试）。
   - ledger.balance：account_id 主键；balance bigint；**把 allow_negative 冗余存一份在本表，并加 CHECK (allow_negative OR balance >= 0)**（客户资金账户永远不能为负；允许为负的账户由 allow_negative 区分；设计文档里的全局 CHECK balance >= 0 不够精确，请按此改进并在 PR 里说明）；version；updated_at。创建账户时自动创建对应的 balance 行（触发器）。
   - ledger.hold、ledger.withdrawal、ledger.outbox、ledger.bank_statement_line、ledger.snapshot_daily：按设计文档第六、七章的 SQL 创建（含 withdrawal 的 CHECK (checker_actor IS NULL OR checker_actor <> maker_actor)）。
   - 所有金额 bigint（单位 0.0001 美元），**不得出现 numeric/float/double 的金额列**。
2. 数据库层硬约束（必须是触发器/约束，不是注释）：
   a) **借贷平衡**：在提交时（DEFERRABLE INITIALLY DEFERRED 的约束触发器）校验每笔 txn：借方分录合计 = 贷方分录合计 = txn.amount；没有任何分录的 txn 也必须失败（提交时至少有 2 条分录）；
   b) **只增不改不删**：ledger.txn、ledger.posting 上的 BEFORE UPDATE OR DELETE 行级触发器直接报错；**另外加 BEFORE TRUNCATE 语句级触发器**（行级触发器不会拦截 TRUNCATE，这是常见漏洞）；
   c) 对 ledger.txn、ledger.posting 显式 REVOKE UPDATE, DELETE, TRUNCATE, TRIGGER FROM ledger_rw（见上方重要提醒）；ledger_rw 对这两张表只保留 SELECT、INSERT；
   d) 冲销交易必须与被冲销交易金额一致、不能冲销自己、不能重复冲销（用约束或触发器；若无法在数据库层完整实现，请实现能实现的部分，并在 PR 里诚实列出剩余由 L-06 应用层负责的部分）；
   e) balance 表允许 ledger_rw 更新（余额缓存），但 balance 行不能删除；
   f) report_ro 对 ledger 所有表只读。
3. 种子数据：创建 6 个单例账户（BANK_DEDICATED、PENDING_PAYOUT、DELIVERY_ESCROW、PLATFORM_HELD、PAYABLE_CARRIER、SUSPENSE）及其 balance 行，可重复执行不报错。钱包类账户、广告预算账户不在这里创建（由应用在用户/商户创建时创建）。
4. 迁移不得写入任何交易或余额（余额恒为 0）。

【测试（用 testcontainers 起真实 PostgreSQL；这是账务，测试比实现更重要）】
a) 迁移 up → down → up 成功；Down 后 ledger 下无残留（表、函数、触发器）；
b) **借贷平衡**：平衡的 txn 提交成功；借贷不等的 txn 在提交时失败；txn.amount 与分录合计不一致失败；只有一条分录失败；没有分录失败；
c) **不可变（以多个身份测试）**：用迁移/管理员角色和用 app_ledger 登录身份，对 ledger.txn、ledger.posting 执行 UPDATE、DELETE、TRUNCATE：app_ledger 得到权限错误（SQLSTATE 42501），管理员角色得到触发器错误；**TRUNCATE 也必须被拦住**；
d) 重复 idem_key 违反唯一约束（23505）；
e) 单例账户重复插入被拒；AD_BUDGET/钱包类账户同 owner 重复被拒；
f) balance 非负：对 allow_negative = false 的账户把余额更新成负数失败（23514）；allow_negative = true 的账户可以；
g) category 只能出现在 PLATFORM_HELD 账户的分录上；
h) 冲销规则（已实现的部分）：自冲销、金额不一致、重复冲销被拒；
i) withdrawal：maker 与 checker 相同被拒（23514）；
j) 权限边界：app_iam 登录访问 ledger 被拒；report_ro 只读；
k) 并发基础（轻量）：用多个并发连接同时对同一账户插入分录并更新 balance，使用 SELECT … FOR UPDATE 的固定顺序，不出现死锁、不出现负数（此处只验证数据库约束在并发下仍然生效；完整的 Post() 并发压测属于 L-02）；
l) 迁移后所有 balance 为 0，txn/posting 为空。

【范围——不要做】
- 不写 Go 的 Post()、业务入账封装、对账、提现流程（L-02 及以后）；不创建 iam 表；不改 F-03 迁移；不加演示数据；不接入应用代码；不使用 numeric/float 金额。
- 不要为了让测试通过而放宽约束或给 ledger_rw 多余权限。

【验收标准——逐条自测并在 PR 描述里写结果】
□ ledger 表齐全，金额列全部是 bigint；9 种 kind 与 CHECK 对应
□ **借贷不平的交易在提交时失败**；txn 与 posting 的 UPDATE/DELETE/TRUNCATE 对 app_ledger（权限错误）和管理员角色（触发器错误）都失败
□ ledger_rw 对 txn、posting 只有 SELECT、INSERT（用 information_schema.role_table_grants 或等价查询在测试里断言）
□ 单例账户唯一性有效（NULLS NOT DISTINCT 或部分唯一索引）；客户资金账户余额不能为负
□ idem_key 唯一；withdrawal 的发起人 ≠ 复核人由数据库拒绝
□ 6 个单例账户已种子，余额均为 0；迁移不产生任何交易
□ 《数据库表结构与账务详细设计》第五章与实际 SQL 一致（凡与文档不同的改进——NULLS NOT DISTINCT、balance 冗余 allow_negative、TRUNCATE 触发器——已同步更新文档并在 PR 里列出）
□ go vet、go test（含 -race 在 Linux 容器或 CI）通过；PR 上的 CI（ci-gate）通过并附 Actions 运行链接与耗时
□ PR 描述含：设计取舍清单、未能在数据库层实现而留给应用层的规则清单、"待确认"

【如何验证（你会亲自跑；前一步失败就停下报告，不要空转）】
0. 预检：docker version（要同时看到 Client 和 Server）；docker info --format "{{.ServerVersion}} | mem={{.MemTotal}}"（MemTotal ≥ 6 GiB）；docker run --rm hello-world。预检失败：重试不超过 2 次、等待不超过 3 分钟，然后停止并报告；不要用 SQLite/内存库/mock 代替真实 PostgreSQL；依赖 Docker 的验收项逐条标为"未验证（原因）"，**账务相关的验收项绝不能声称已通过**。
1. cd backend && make db-up db-migrate
2. 用 psql 手工验证：以管理员身份尝试 UPDATE ledger.posting、TRUNCATE ledger.txn；提交一笔借贷不平的事务（BEGIN; INSERT …; COMMIT;）并展示报错
3. make db-bootstrap 创建登录角色后，以 app_ledger 身份重复上一步，展示权限错误
4. make db-rollback 再 make db-migrate
5. go test ./...；make db-down 并确认无遗留容器

【遇到不确定】
**不要猜测。** 账务规则有任何疑问，写进 PR 描述"待确认"并按更严格的一侧实现；与设计文档冲突时以文档为准并提出。

【完成后】
在 PR 描述里按模板填写，并附 5 行以内总结。
```

---

## F-06 前端工作区（pnpm monorepo）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。F-01～F-05、F-08、F-10、F-12、F-13 已合并。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】F-06 前端工作区（pnpm monorepo）
【分支】task/F-06-frontend-workspace，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（第 3 节技术栈、第 4 节规则 15、第 5–6 节）
2. Dr.life+开发任务拆分与验收.md 第四章 F-06 一行；第十章 U 系列（了解后续任务，不要提前做）
3. Dr.life+第一版交付形态与技术架构.md 第一章（Telegram Mini App 约定）、第六章（Mini App 前端、管理后台选型）、第七章（域名）
4. Dr.life+交互文档.md 第一章（全局交互规范：返回键、主按钮、加载、多语言、高棉语排版）
5. packages/api-client/（F-04 已生成的 schema.d.ts 与 README）、.github/workflows/ci.yml 里的 frontend-check（CI 会执行的命令）
6. prototype/index.html（只作词条与交互的参考，不要复制其单文件结构）

【背景】
建立前端的工程骨架：一个 pnpm 工作区，包含买家、商家、司机三个 Telegram Mini App 的壳，以及平台后台、物流仓储后台两个网页应用的壳，加上共享的 UI、接口客户端、多语言包。**只做骨架和基础设施，不做业务页面**（页面是 U-01～U-10）。

【文件归属】apps/buyer、apps/merchant、apps/driver、apps/admin、apps/wms、packages/ui、packages/api-client（保留 F-04 的生成文件，新增客户端封装）、packages/i18n、根目录的 pnpm-workspace.yaml、package.json、pnpm-lock.yaml、tsconfig.base.json、.nvmrc、eslint/prettier 配置、.npmrc。不要改后端、不要改 CI 工作流（CI 已按 pnpm-workspace.yaml 的存在自动启用前端检查）、不要改 openapi.yaml。

【技术要求】
- Node 22（.nvmrc 与 package.json 的 engines 固定）；pnpm 通过 corepack 使用，package.json 里写 packageManager 字段并固定版本；提交 pnpm-lock.yaml；
- TypeScript 严格模式；React 18/19 + Vite；Mini App 使用 Telegram 官方 SDK 的 TypeScript 封装（@telegram-apps/sdk 或官方等价包，二选一并说明理由）；后台使用 Ant Design；
- 接口客户端用 openapi-fetch（与 openapi-typescript 配套）并固定版本；多语言自建轻量实现（不引入重型 i18n 框架，除非说明充分理由）；
- 所有依赖版本固定（精确版本或锁文件保证），在 PR 里列出新增依赖及理由。

【范围——要做】
1. 工作区：pnpm-workspace.yaml 包含 apps/* 与 packages/*；根脚本 typecheck、build、lint、test、i18n:check（用 pnpm -r 递归）；**这些脚本名必须与 CI 一致**：CI 会执行 pnpm install --frozen-lockfile、pnpm -r typecheck、pnpm -r build、pnpm -r i18n:check。
2. 五个应用壳（每个都能 pnpm --filter <名> dev 启动、build 成功）：
   - 开发端口固定：buyer 5173、merchant 5174、driver 5175、admin 5176、wms 5177；
   - Mini App 三个（buyer、merchant、driver）：入口页面显示应用名与当前语言，演示：读取 Telegram 的 initData 与 languageCode（**不在非 Telegram 环境崩溃**：在普通浏览器里使用明确标注的开发用 mock，并在页面上显示"开发模式"提示）、Telegram 返回键与主按钮的封装（提供 useBackButton、useMainButton 钩子，本任务只演示不接业务）、主题色读取；
   - 后台两个（admin、wms）：Ant Design 的基础布局（左侧菜单占位、顶部栏占位、内容区），路由骨架（react-router），一个"无权限"页和一个"404"页占位；
   - 不做登录、不做业务页面。
3. packages/i18n：
   - 词条文件 src/locales/zh.json、en.json、km.json（只放通用词条：确定、取消、返回、加载中、网络错误、重试、语言名称等少量；**不要迁移原型里的全部词条**）；
   - t(key, params) 与 useI18n 钩子；语言切换；默认语言规则：账号登记语言 > Telegram languageCode（zh/en/km，其他回退 en）> en；
   - **i18n:check 脚本**（在 packages/i18n 里，pnpm -r i18n:check 会运行它）：校验三种语言的键完全一致、没有空字符串、占位符变量一致；高棉语若暂时没有可靠译文，可使用英文并在值前加标记 "[TODO-KM-REVIEW] "，脚本**允许**该标记但要在输出末尾汇总数量与键名（不让它悄悄通过）；缺键、多键、空值一律失败；
   - 高棉语排版：提供全局样式变量（行高不低于 1.6、字号不小于 13px 的基础规则），自托管字体（@fontsource/noto-sans-khmer 或等价），**不要从 Google Fonts 等外部 CDN 加载**；写一个页面演示中文、English、高棉语三行文字不被截断。
4. packages/api-client：保留 F-04 生成的 src/schema.d.ts；新增 createApiClient({ baseUrl, getToken, getLanguage })：使用 openapi-fetch，自动加 Authorization（有令牌时）、X-Request-ID（合法格式：1–64 位字母数字-_，自动生成）、Accept-Language；提供 withIdempotencyKey() 生成合法的 Idempotency-Key（1–64 位字母数字-_）；统一把失败信封解析成带 code、三语 message、request_id 的 ApiError；基础地址来自 VITE_API_BASE_URL（开发默认 http://localhost:8080，生产为 https://adminapi.settlecore.org，**不要把生产地址写死**）；用 vitest 和模拟 fetch 做类型安全的 /v1/public/ping 调用测试。
5. packages/ui：只放一个最小的共享样式令牌与一个示例组件（例如 Loading），证明包可以被应用引用；**不要做完整组件库**（U-01 做）。
6. 质量工具：ESLint（含 react-hooks 规则、禁止未使用变量）、Prettier、vitest 的基础配置；至少 packages/i18n 与 packages/api-client 有单元测试；根目录 README 或 apps/README 说明如何启动、如何新增应用/包。
7. 在 README 里列出各应用本地开发需要加入后端 CORS_ALLOWED_ORIGINS 的来源（http://localhost:5173 到 5177），**只写文档，不要改后端文件**。

【范围——不要做】
- 不做登录、不做业务页面、不做完整 UI 组件库（U-01）；不迁移原型的全部词条与页面；不使用外部 CDN；不提交 node_modules、构建产物（dist）；不改后端、CI 工作流、openapi.yaml；不引入状态管理库（等需要时再决定）。
- 不要在代码里写死生产域名；不要把任何令牌或密钥写进前端代码。

【验收标准——逐条自测并在 PR 描述里写结果】
□ 全新克隆后：corepack enable && pnpm install --frozen-lockfile 成功
□ pnpm -r typecheck、pnpm -r build、pnpm -r i18n:check、pnpm -r test 全部通过
□ 五个应用都能 pnpm --filter <名> dev 启动（附端口与截图或页面文字说明）；Mini App 在普通浏览器里显示"开发模式"而不是报错
□ i18n:check：故意删掉一个键或留空值时失败（演示后恢复）；TODO-KM-REVIEW 的数量被汇总输出
□ 三种语言的排版演示页面中高棉语不被截断
□ api-client 测试通过：自动加请求头、生成合法 Idempotency-Key、失败信封被解析为 ApiError
□ **PR 上的 CI 中 frontend-check 真实执行并通过**（不是 skipped；这是它第一次真正运行，请附 Actions 运行链接与耗时）；若 CI 暴露了 F-13 里 pnpm 初始化顺序等问题，如实记录并只在允许范围内处理（需要改 CI 工作流则在 PR 里说明，不要擅自改）
□ 无外部 CDN 请求（构建产物里搜索 http 外链并说明）；无生产域名写死
□ PR 描述含新增依赖及理由、"待确认"、"未验证"

【如何验证（你会亲自跑；前一步失败就停下报告，不要空转）】
0. 预检：node --version（应为 22.x）；corepack --version；pnpm --version（通过 corepack）；若没有 Node，可用 Docker：docker run --rm -v <目录>:/w -w /w node:22 …（先做 docker version / docker run --rm hello-world 预检）。预检失败：重试不超过 2 次，然后停止并报告。
1. pnpm install --frozen-lockfile
2. pnpm -r typecheck && pnpm -r build && pnpm -r i18n:check && pnpm -r test
3. 分别启动五个应用并打开页面检查
4. 推送分支并创建 PR，等待并记录 Actions 结果

【遇到不确定】
不要猜测，写进 PR 描述"待确认"。尤其：Telegram SDK 的选择、React 版本、Ant Design 版本，请说明理由。

【完成后】
在 PR 描述里按模板填写，并附 5 行以内总结。
```

---

## 合并前的评审提示

| 任务 | 重点看什么 |
|---|---|
| A-01 | `NULLS NOT DISTINCT`（买家账号不能重复）；`admin_role_perm` 种子是否与表格逐格一致（测试里是不是真的逐格比对，而不是只数行数）；敏感列（密码哈希、令牌密文、TOTP 密文）`report_ro` 读不到；`super` 的 level 不能被改；Down 是否可逆；有没有顺手写了 Go 业务代码 |
| **L-01（最严格）** | **借贷不平在提交时失败**（测试要用 BEGIN/COMMIT 真提交，不是只在语句里检查）；**TRUNCATE 被拦住**（BEFORE TRUNCATE 触发器）；`app_ledger` 对 txn/posting 的 UPDATE/DELETE/TRUNCATE 都是权限错误，管理员是触发器错误；`ledger_rw` 只有 SELECT、INSERT（看有没有断言授权表）；单例账户唯一性；`balance` 的 `allow_negative` 与 CHECK；`category` 只在 `PLATFORM_HELD`；金额列有没有出现 numeric/float；`withdrawal` 发起人 ≠ 复核人；迁移是否写入了任何交易或余额；有没有放宽约束或多给权限来让测试通过；PR 里"留给应用层的规则清单"是否诚实 |
| F-06 | **`frontend-check` 是否真实执行**（不是跳过）；有没有外部 CDN；`i18n:check` 的 `TODO-KM-REVIEW` 是否被汇总而不是悄悄通过；有没有写死生产域名；锁文件是否提交；有没有越界改后端或 CI；Mini App 在普通浏览器里的开发模式 |
| 三个都要看 | `ci-gate` 绿不绿（分支保护目前不强制，合并前请自己确认）；"未验证"项是否如实；是否修改了授权范围之外的文件；L-01 与 A-01 合并后迁移序号是否冲突 |

## 合并顺序建议
1. **A-01 与 L-01 先合并哪个都行**；合并第二个时如果提示迁移序号冲突，让 Codex 重新编号并重跑测试。
2. **F-06 与它们无关**，随时可以合并；合并后 CI 的 `frontend-check` 会第一次真正运行。
3. 这三个合并后，下一批任务是：**L-02（`Post()` 入账核心，含 1000 并发压测）**、A-02（Telegram 登录与多 Bot 校验）、A-03（会话与令牌，提供 F-05 要的 `Authenticator`）。其中 A-02 依赖 P0-1（Telegram 登录 PoC），L-02 依赖 L-01。
