# 波次 5：账务收尾与运维（L-08 至 L-11、F-09、Q-02 至 Q-08）

**用法：** 整块复制代码块内容粘贴给 Codex，每个任务一个独立 worktree 与分支。
**外部阻塞：** L-08 依赖 P0-6；F-09 需要你先提供机柜与服务器信息；Q-03 需要异地备份位置；Q-07 依赖前端主线完成；Q-08 多数是人工核对。
**L-11 澄清（回复 CMP-33 的提问）：** 以当前已实现的 ledger 能力为范围；银行文件导入测试登记为『待 L-08』，灾备恢复只做 testcontainers 的 pg_dump/pg_restore 最小演练，备份工具与 PITR 属于 Q-03。

---

## L-08 银行对账文件导入与充值匹配

> 银行格式未定：先交付『可插拔解析器接口 + 通用 CSV 解析器 + 模拟银行适配器』，拿到真实样例后再加专用解析器。

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】L-08 银行对账文件导入与充值匹配
【分支】task/L-08-bank-reconciliation
【依赖】P0-6 结论（银行接口与对账文件格式）、L-03、L-07b、Q-01、S-03
【先阅读】
1. Dr.life+数据库表结构与账务详细设计.md 第七章（三层对账、bank_statement_line）与第六章 6.1、6.6、6.19、第十二章待确认 1、2；Dr.life+平台后台权限与审计规范.md（审计 #1–#4、#16）
2. backend/internal/ledger/doc.go（Topup、BankDiff、SUSPENSE）、backend/internal/ledger/recon.go（内部对账如何与本任务衔接）；P0-6 结论文档；Dr.life+开发任务拆分与验收.md：L-08、P0-6
【范围——要做】
1. 适配层 `bank.StatementParser` 接口（输入文件字节 → 规范化行列表：bank_ref、booked_on、amount_e4、direction IN/OUT、counterparty、raw）；**通用 CSV 解析器**（列映射配置、UTF-8/BOM、小数点与千分位、时区、金额精度检查——按 e4 整数转换，**拒绝无法精确转换的金额**）；模拟银行适配器（生成样例文件、入金/出款回调模拟）；真实银行解析器占位并列待确认。
2. **导入**：`POST /v1/admin/bank/statements`（`fin` 权限 edit，step-up，审计；文件走 F-14，purpose=bank_statement，不经 API 转发）→ 异步任务（S-03）：计算文件 SHA-256 → **同一文件重复导入不产生重复**（文件哈希唯一 + 行唯一键 `(bank_ref, booked_on, amount, direction)` 已有）→ 逐行入 `ledger.bank_statement_line`（imported_batch、match_status=unmatched）。导入结果报告：总行数、新增、重复跳过、无法解析（行级错误可下载）。
3. **充值意图与匹配**：迁移 `ledger.topup_intent`（account、reference_code 唯一高熵、amount_expected 可空、status open/matched/expired、expires_at）：买家/商家发起充值得到**转账附言码**（`POST /v1/{end}/wallet/topups`，幂等）；银行入金行按 **附言码 → 金额 + 日期窗口 → 手工** 的优先级匹配：匹配成功 → 调用 `ledger.Topup`（幂等键 `topup:bankline:<line id>`），line=matched 并记录 matched_txn_id；**不确定的入金一律先入 SUSPENSE**（`ledger.BankDiff` direction IN）而不是猜测入账；出款行（OUT）与 `payout` 交易按 bank_ref/金额匹配，匹配成功回填提现的 bank_ref（**经 L-05 的 ConfirmPayout 流程，不得绕过状态机**）。
4. 三种状态：matched / unmatched / ignored（人工忽略需理由）；差异处理：人工界面接口（`fin` 权限）对 unmatched 行『认领到某用户充值』（需复核人 ≠ 操作人）、『记为银行差异』（BankDiff 入 SUSPENSE，理由必填）、『忽略』；全部写审计。
5. **SUSPENSE 超期未清零告警**：阈值天数配置（默认 N=3，待确认），每日检查（挂 S-03 scheduled），产生事件与指标；与 L-07 的对账互补（内部恒等式不变）。
6. 银行余额核对：每个导入批次读取对账文件的期末余额（若格式提供）与 `BANK_DEDICATED` 账务余额比较，差异产生 recon 事件（只告警，不自动调整）。
7. 接口：账务/后台对账页所需查询（行列表、筛选、批次、未匹配列表）；与 L-09 复用分页与导出机制。
【验收标准】
① 重复导入同一文件不产生重复（文件级与行级）；三种匹配状态正确
② 不确定入金进 SUSPENSE，不猜测入账；匹配入金调用 Topup 且幂等；出款匹配回填走 ConfirmPayout
③ SUSPENSE 超期告警；差异处理需理由、复核人 ≠ 操作人、写审计
④ 金额无法精确转换的行被拒并报告；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 真实银行文件格式、入金/出款回调与冻结接口（P0-6）
- SUSPENSE 清零期限 N
- 人工认领是否需要二人复核（默认需要）
【红线】不得为了对平而自动调整账务；不得绕过 L-05 状态机；文件内容不得写日志。
```

---

## L-09a 账务只读查询接口（L-09b 导出、L-09c 凭证并列另卡）

> 2026-10-26 董事会授权收窄：原 L-09 拆为 L-09a（本节，CMP-32）、L-09b 导出（CMP-485，待 F-14/S-03/Q-01）、L-09c 货款凭证并列（CMP-486，待 T-06）。

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】L-09a 账务只读查询接口（流水、详情、对账表、本人钱包流水）
【分支】task/L-09a-ledger-queries
【依赖】L-02、L-07、S-02（report 池）。A-06b（RBAC）、T-06、F-14 未合并：用可注入接口 + 生产 fail-closed / 默认空实现
【先阅读】
1. Dr.life+交互文档.md 平台『资金流水与对账』与买家/商家钱包页；prototype/index.html 资金流水与对账页；Dr.life+平台后台权限与审计规范.md（资金权限、脱敏）
2. backend/internal/ledger/（API 表面白名单守卫测试：新增导出只读方法必须更新白名单并说明）；Dr.life+开发任务拆分与验收.md：L-09、T-06
【范围——要做】
1. 只读查询服务（ledger 包内，走 `report` 池，**用户视角与后台视角分开**）：流水列表（按类型/关键词/时间范围/账户；**keyset 分页**，page_size ≤ 100）、详情（交易头 + 全部分录 + 分账明细 + 关联单据引用 + 操作人）、账户对账表（账户、系统余额、流水推算余额、差异）、用户钱包流水（只含本人账户的分录，对方信息脱敏）、提现与充值记录。
2. **无资金权限的角色看不到**：后台 `fund`/`fin` 模块 view 才能访问资金接口；商家员工按 `fin` 模块权限；买家/司机只看自己的钱包。
3. **可注入接口（本卡定义）**：`AccountDirectory.Resolve(ctx, accountPublicID)`（ledger 内定义，允许 iam 包补**只读**实现，查 iam.account 取 id/role/merchant_id/status）；`AdminPermissionChecker.Can(ctx, adminID, module, level)`（A-06b 合并前生产接线**全部拒绝**，仅测试 fake 放行，不得猜权限）；`ProofLister`（默认返回空，响应结构保留 `kind: txn|proof`，凭证不计入任何合计，真实接入属 L-09c）。
4. **不做**：CSV 导出（L-09b）、凭证真实对接（L-09c）、/wallet 余额汇总与提现（L-13）。
5. **HTTP 合同（OpenAPI 先行）**：
   - 后台：`GET /v1/admin/ledger/txns`、`/txns/{txn_no}`（含全部分录）、`/accounts`（对账表：account_ref 脱敏、kind、system_balance、recomputed_balance、diff、last_checked_at，支持 diff!=0 过滤，读 ledger.snapshot_daily / recon_run，缓存 60 秒）、`/accounts/{account_ref}/postings`。
   - 用户：`/v1/{buyer|merchant|driver}/wallet/transactions`、`/{txn_no}`；只含本人账户分录，对方显示通用标签（平台、配送托管、银行、装修资金）；商家按 `fin` view，无权限员工 403；他人资源 404。
   - 分页与过滤：keyset，page_size ≤ 100；默认近 30 天、区间 ≤ 366 天、时区 Asia/Phnom_Penh；过滤 type/from/to/amount_min/amount_max/ref_type/ref_id/reversed，account_ref 仅后台。
   - 关键词 `q` 只匹配：txn_no 前缀（不区分大小写）、ref（`order:123` 或纯数字 = ref_id）、bank_ref（精确，仅充值/出款类）；后台有 `sens` view 时可按所有者 public_id 精确检索。**不检索** idem_key、note、收款账户。
   - 响应金额为 e4 整数；不返回 idem_key、outbox、复核/发起人身份。
   - 脱敏：无 `sens` 的后台——所有者 public_id 只显示末 4 位、actor 显示角色+末 4 位、bank_ref 末 4 位；有 `sens` 完整，且每次查看未脱敏详情写审计事件（audit.Writer + fake）；收款账户任何角色只显示末 4 位（明文仅走 L-13 `/payee`）。
   - data_scope 非空的管理员访问 admin 账务接口一律 403（保守默认）。
   - 错误：无资金权限 403、他人资源 404、参数非法 400。
6. 性能：常规列表 P95 < 500 ms（本地 100 万分录基准写进 PR）；索引评审（EXPLAIN）；账户对账表分块计算，不在请求内全表扫描（可读快照/缓存 60 秒）。
【验收标准】
① 参照原型资金流水与对账页；用户视角只含本人分录且不泄露他人信息
② 权限与脱敏正确（无资金权限 403，他人资源 404）；关键词只命中允许字段（idem_key/note 不可检索）
③ keyset 分页稳定（并发写入下不重复不遗漏）；基准达标
④ go vet、-race、ci-gate 全绿；API 表面白名单已更新并说明理由
【待确认（不要自己拍板，PR 里列出）】
- 流水页对用户展示的对方信息粒度（默认只显示业务类型与单据号）
- data_scope 管理员是否可看分区域账务（默认 403）
【红线】不得返回他人账户明细；不得把凭证计入余额或合计；A-06b 前不得把权限检查接成放行。
```

---

## L-10 财务中心接口

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】L-10 财务中心接口
【分支】task/L-10-finance-center
【依赖】L-09、L-07、W-03、N-08
【先阅读】
1. Dr.life+产品需求文档PRD.md 平台财务中心；Dr.life+交互文档.md 平台『财务中心/总览』；Dr.life+平台后台权限与审计规范.md（审计 #13 收入与流水一致）；Dr.life+数据库表结构与账务详细设计.md 第三章恒等式
2. Dr.life+开发任务拆分与验收.md：L-10；backend/internal/ledger/recon.go
【范围——要做】
1. 接口（/v1/admin/finance/*，`fin` 模块 view，走 report 池）：按周期（日/周/月/自定义，时区 Asia/Phnom_Penh）的**平台收入五类**（仓储费 storage、配送留存 delivery_margin、广告 ads、结算服务费 settle_fee、会员 member，来自 PLATFORM_HELD 分录的 category 汇总）；**资金头寸**（客户负债合计、平台自有、应付中通、过渡 SUSPENSE、银行专用账户余额，**头寸 = 恒等式各项**）；提现概况（待复核/待出款/超时、金额分布）；商户资金排行（Top N）。
2. 一致性：收入汇总**必须与流水一致**（审计 #13）：提供 `FinanceConsistencyCheck`（Q-02 调用），测试里故意篡改汇总缓存能被检出；汇总可由 `snapshot_daily` + 当日增量组成（避免全表扫描），**以分录重算为真相**，缓存带版本与生成时间。
3. 缓存：结果缓存 60 秒（S-02 缓存层，键含周期与权限范围），**主动失效**于入账事件；响应声明数据截至时间。
4. 导出与权限同 L-09；脱敏。
【验收标准】
① 收入按 category 汇总，与流水一致（对比测试：随机业务序列后两种算法相等）
② 周期筛选正确（跨月/时区边界用例）；头寸等于恒等式各项
③ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 结算服务费第一版恒为 0，接口仍保留该分类
【红线】不得读缓存当真相；不得浮点。
```

---

## L-11 账务测试套件（设计文档第十一章全部用例）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】L-11 账务测试套件（设计文档第十一章全部用例）
【分支】task/L-11-ledger-test-suite
【依赖】L-03..L-07b、L-12
【先阅读】
1. Dr.life+数据库表结构与账务详细设计.md 第十一章（测试清单：平衡、幂等、并发、分账、不可变、冲销、提现、对账、权限、灾备）；backend/internal/ledger/ 全部测试；AGENTS.md 第 6 节
2. Dr.life+开发任务拆分与验收.md：L-11；.github/workflows/ci.yml
【范围——要做】
1. **用例映射表**：新增 `backend/internal/ledger/TESTMATRIX.md`，把第十一章每一行拆成具体用例，指向已有测试或新增测试（缺口补齐）；每个用例有编号，测试函数名或注释引用该编号；CI 里有一个检查：矩阵里的每个编号都能在测试代码中找到引用（防止测试被悄悄删掉）。
2. 补齐缺口：**权限**——非账务角色无法直接写 `ledger` 表（用 app_ops/app_report/app_wms 等其他登录身份逐表尝试 INSERT/UPDATE/DELETE 全部被拒）；**灾备**——测试里 pg_dump/pg_restore（或基于 testcontainers 的卷快照）从备份恢复后跑 `RunDailyReconciliation`，恒等式成立、balance 与分录重算一致；**并发**——同一商户并发发货/提现/广告划扣（用业务方法）余额不为负、不重复扣款；交叉账户死锁测试；**分账**——属性测试大样本（≥10 万随机）。
3. **稳定性（不抖动）**：所有并发测试不得依赖 sleep，用同步原语；CI 中账务集成测试 job 以 `-race -count=1` 运行，另设 nightly（schedule）job `-count=5` 与 `-cpu 1,2,4` 重复运行；失败时输出足以复现的日志（随机种子、并发度）；把之前出现过的偶发失败排查结论写进 README。
4. 测试耗时预算：全套账务测试在 CI 中 ≤ 8 分钟（超出则分片并说明）。
5. CI 接入：确保 `backend-tests` 与 `backend-integration` 会运行账务全部集成测试（目前 integration job 只跑部分，请核对并修正），`ci-gate` 依赖这些 job。
【验收标准】
① 第十一章每一行都有映射到的自动化用例，缺失即 CI 失败
② 全部通过；账务集成测试纳入 CI 必需检查；并发测试连续 5 次、不同 CPU 数下稳定
③ 灾备恢复演练测试通过；非账务角色写 ledger 全部被拒
④ go vet、-race、ci-gate 全绿
【红线】不得用放宽断言/加长 sleep 让测试通过；不得删除已有测试。
```

---

## F-09 环境与部署脚本（托管机柜）

> ⛔ 需要用户先提供：机柜位置/带宽、服务器数量与配置、是否有专职运维、域名与 Cloudflare 账号状态。信息不全时先做 test 环境的 Compose 版本。

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】F-09 环境与部署脚本（托管机柜）
【分支】task/F-09-deploy
【依赖】机柜与服务器信息（由用户提供）、S-03（两个 Redis）、F-11
【先阅读】
1. Dr.life+第一版交付形态与技术架构.md 第七章（托管机柜、服务器清单、网络、Cloudflare Tunnel、WireGuard、域名）；Dr.life+技术方案与系统边界.md 第十二章（环境、发布、测试）；Dr.life+API安全协议与容量及队列设计.md 第六章（两个 Redis）
2. backend/Dockerfile、backend/Makefile、deploy/observability/、.github/workflows/ci.yml；Dr.life+开发任务拆分与验收.md：F-09、Q-03
【范围——要做】
1. 目录 `deploy/`：test / staging / prod 三套配置**物理分离**（独立 env 文件模板、独立数据库、独立 Bot、独立密钥、独立 Redis 实例、独立域名前缀），模板只放占位符（不提交真实值）；SOPS（age）加密的配置约定与示例；**测试环境不得使用正式 Bot 与真实资金账户**（启动校验 APP_ENV 与配置一致，不一致拒绝启动）。
2. 部署形态：Docker Compose（单机 test）与 systemd + 容器（prod 两台应用服务器滚动发布：先摘除 → 迁移 → 部署 → 健康检查 `/readyz` → 放量）；一台应用服务器故障不中断；服务清单：app（API）、worker（队列，按队列类别独立进程/并发）、relay（发件箱转发，可与 worker 同进程由配置决定）、redis-cache、redis-queue（noeviction + AOF）、PostgreSQL 主从（只给配置模板与说明，不自动搭建生产库）、cloudflared（Tunnel，**不开放入站端口**）、Prometheus/Grafana/Alertmanager。
3. **一条命令部署到 test**（`make deploy ENV=test` 或 `scripts/deploy.sh test <version>`）：构建/拉取镜像（版本号 + 变更说明）、数据库迁移（**迁移前自动备份、失败自动停止**，只用迁移角色）、种子（生产禁止加载演示数据）、冒烟测试（健康、登录接口 ping、一次幂等写）。**生产部署必须人工批准**（GitHub Environments 的 required reviewers 工作流 + 部署脚本里的二次确认），部署记录与审计。
4. **回滚**：`scripts/rollback.sh <env> <version>`：回滚应用版本；迁移只向前兼容（先加列 → 改代码 → 再删列），**数据库回滚只在显式确认下做 Down**；回滚演练脚本在 test 环境验证。
5. 加固：容器非 root、只读根文件系统（可行范围内）、资源限制、最小 capabilities、镜像固定版本与摘要；机器上只允许通过 WireGuard 登录（给出 wg 配置模板与防火墙规则：入站全拒、仅放行 WireGuard 与内网）；asynqmon/Prometheus/Grafana 仅内网；日志轮转；时间同步。
6. 运行手册 `deploy/RUNBOOK.md`：部署、回滚、扩缩容、密钥轮换（主密钥两位管理员分管、不落盘、启动注入）、证书、故障处理入口；『不要做的事』清单。
【验收标准】
① 一条命令部署到 test，冒烟通过；回滚可用且在 test 演练过
② 三个环境数据库、Bot、密钥、Redis 互相独立；错环境配置拒绝启动
③ 生产部署需人工批准；机柜防火墙无入站端口（配置审查清单）；主密钥不落盘（启动注入流程文档与测试）
④ go vet/脚本 shellcheck 通过；ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 机柜/带宽/服务器配置与专职运维（用户提供）
- PostgreSQL 主从切换方式（手动/半自动，建议手动 + 演练）
- 异地备份位置（数据是否必须留在柬埔寨境内）
【红线】不得提交任何真实密钥/令牌/IP/账号；不得在生产脚本里默认加载演示数据；不得开放入站端口。
```

---

## Q-02 18 项审计检查（自动运行）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】Q-02 18 项审计检查（自动运行）
【分支】task/Q-02-audit-checks
【依赖】Q-01、L-07/L-07b、W-01、L-05、A-06、S-03
【先阅读】
1. Dr.life+平台后台权限与审计规范.md 第四章（18 项逐条：条件与级别、可配置阈值）；Dr.life+数据库表结构与账务详细设计.md 第七章 2（与审计 18 项的对应）
2. backend/internal/ledger/recon.go（已有的账务内部核对，复用而不重写）；Dr.life+开发任务拆分与验收.md：Q-02
【范围——要做】
1. 新增 `backend/internal/audit/checks/`：18 个检查，各自一个文件与一个 `Check` 实现（id、名称、级别规则正常/关注/异常、阈值来自 ops.setting：大额出款阈值默认 1000 美元、至少保留超管数默认 1、欠费阈值默认 200 美元）；**账务类检查（#1、#2、#4、#12、#13）复用 ledger 的对账/恒等式查询，不重复实现不同口径**；其余按各模块只读接口取数（不跨 schema 直接 JOIN）。**依赖模块尚未实现的检查（如 #14 库存、#11 配送托管）按当前已有表实现，缺表时返回『未启用』状态并在 PR 里列出**。
2. 运行：每日定时（S-03 scheduled，业务日幂等）+ 后台手动『立即运行』；结果写 `audit.check_result`（day、check_id、severity、detail 摘要、run_at；**只追加**）；`异常` 级触发事件与告警（Alertmanager 规则），`关注` 级进入后台待办；后台接口（/v1/admin/audit/checks，`aud` 权限）：18 项当前状态列表、历史、详情（脱敏）。
3. **故意破坏测试（每项至少一个）**：测试里用管理员连接制造问题并断言被检出且级别正确——改余额（#2）、改分账（#12）、重复流水号（#12，需绕过唯一约束的场景用禁用触发器的方式）、改库存使库位之和≠库存（#14）、改收入累计（#13）、把集成设为异常（#15）、造成余额为负（#4，用允许为负的 SUSPENSE 之外的账户需绕过 CHECK，测试里可临时去掉约束）、让提现超时（#16）、同一收款账户被多个账户使用（#17：用收款账户**哈希**比较，不解密）、欠费超阈值（#18）等；同时**正常业务流程跑完 18 项无『异常』**。
4. #17 收款账户重复：对收款账户明文做带密钥的 HMAC 指纹（新增 `payee_fingerprint` 列的迁移与回填策略，**不得解密比较或把明文写进检查结果**）。
5. 性能：全套检查在目标规模下 ≤ 60 秒；读走 report 池；不得持有长事务。
【验收标准】
① 与《权限与审计规范》18 项逐项一致（用表驱动测试逐项核对条件与级别）
② 级别正常/关注/异常正确；阈值可配置；故意破坏每项都能检出；正常流程无『异常』
③ #17 不解密收款账户；结果表只追加；异常级触发告警事件
④ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 尚未实现模块对应的检查启用时间
【红线】不得为通过测试放宽条件；账务口径必须与 ledger 对账一致。
```

---

## Q-03 备份与恢复演练

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】Q-03 备份与恢复演练
【分支】task/Q-03-backup-restore
【依赖】F-09、L-07b、Q-01
【先阅读】
1. Dr.life+第一版交付形态与技术架构.md 第七章（备份与监控、RPO ≤ 15 分钟、RTO ≤ 4 小时、异地加密）；Dr.life+技术方案与系统边界.md 第十一章；Dr.life+数据库表结构与账务详细设计.md 第九章（备份）与第十二章待确认 6（境内留存）
2. Dr.life+开发任务拆分与验收.md：Q-03、L-11
【范围——要做】
1. 工具选型与说明（pgBackRest 或 WAL-G，二选一并写理由）：主库**每日全量 + WAL 连续归档**到异地加密对象存储（**客户端加密**，密钥与主密钥分开保管）；保留策略（如 7 日 + 4 周 + 12 月，可配置）；备份完成与失败告警；备份文件校验（恢复前校验和）。
2. **恢复演练脚本**（`scripts/restore-drill.sh`，在隔离的临时实例上运行，绝不触碰生产）：从最近全量 + WAL 恢复到指定时间点（PITR）→ 迁移版本核对 → 运行 `ledgerctl recon`（恒等式成立、balance 与分录重算一致）、`auditctl verify`（审计链连续）、`wms` 库存一致性检查 → 输出演练报告（耗时、数据丢失窗口 RPO 实测、总耗时 RTO 实测）。每季度演练的日程与记录模板放 `docs/ops/`。
3. 灾难场景手册：主库故障切换（手动/半自动，步骤与检查清单）、误删数据的 PITR 流程、Redis 队列实例丢失后的恢复（发件箱重放）、密钥丢失的后果与离线备份要求。
4. 在 CI 中提供缩小版演练（testcontainers：造数 → 备份 → 恢复 → 校验恒等式），作为 nightly job，失败告警。
【验收标准】
① 从备份恢复后恒等式成立、余额与分录重算一致（脚本与 nightly 都验证）
② RPO ≤ 15 分钟（WAL 归档间隔配置与实测）、RTO ≤ 4 小时（实测记录）
③ 备份在异地且客户端加密；备份失败告警；演练报告完整
【待确认（不要自己拍板，PR 里列出）】
- 异地存储位置与是否必须境内（用户提供）
- RPO/RTO 的真实达标需要在机柜环境实测
【红线】演练只在隔离实例；备份密钥不得与备份存放同处；不得把生产数据拷到开发机。
```

---

## Q-04 监控告警与值班流程

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】Q-04 监控告警与值班流程
【分支】task/Q-04-alerting
【依赖】F-11、S-03、L-07b、A-07
【先阅读】
1. Dr.life+技术方案与系统边界.md 第十一章『可观测』；deploy/observability/（已有告警规则与仪表盘）；Dr.life+API安全协议与容量及队列设计.md 6.9；Dr.life+开发任务拆分与验收.md：Q-04
【范围——要做】
1. 汇总并补齐告警规则（Prometheus 规则文件 + `promtool check rules` + 单元测试 `promtool test rules`）：**账务异常**（对账失败、出款冻结生效、发件箱滞后）、**对账差异**（SUSPENSE 超期）、**提现超时**（>24h）、**Bot 受限/发送失败率**、**AI 回退率高/全部提供商失败**、**5xx 率与 P95 超阈值**、**队列等待/死信增长/Redis 内存**、**登录校验失败突增**、**限流命中激增**、**备份失败/锚定缺失**；每条规则有 severity、说明、runbook 链接。
2. Alertmanager 路由与抑制（deploy/observability/alertmanager.yml 模板，接收渠道占位：**运维 Telegram Bot/邮件，具体渠道待确认**；P0 资金类 5 分钟内必达、重复告警合并、夜间升级策略）；**告警通知里不得包含用户个人信息或金额明细**，只放事件类型与编号。
3. 值班流程文档 `docs/ops/ONCALL.md`：分级（P0/P1/P2）、响应时限、升级路径、每类告警的第一步处置清单（账务异常：先确认冻结生效、再查 recon_issue；Bot 被封：切换备用等）、事后复盘模板。
4. Grafana：补齐业务与运维面板（已有总览基础上增加队列、对账、Bot 池、AI 网关、提现）；面板 JSON 纳入版本管理并可 provisioning。
【验收标准】
① 规则通过 promtool 校验与规则单元测试（模拟指标序列触发/不触发）
② 关键告警都有 runbook；告警内容不含个人信息；值班文档完整
③ go vet/校验脚本进 CI（轻量）；ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 告警接收渠道（运维 Telegram Bot / 邮件 / 短信）与值班人员
【红线】不得在告警里放个人信息；不得提交真实 Webhook 地址与令牌。
```

---

## Q-05 安全测试与依赖扫描

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】Q-05 安全测试与依赖扫描
【分支】task/Q-05-security-tests
【依赖】A-02、A-03、S-01、L-05
【先阅读】
1. Dr.life+技术方案与系统边界.md 第十二章『安全测试』与第十章；Dr.life+API安全协议与容量及队列设计.md 第一章（威胁表）；AGENTS.md 规则 9–14；Dr.life+开发任务拆分与验收.md：Q-05
【范围——要做】
1. 自动化安全测试套件（`backend/internal/securitytest/` 与 CI job）：**越权**（S-01 的授权矩阵扩展到所有已有接口，含 IDOR：他人资源 id → 404）；**重放 initData**（过期/篡改/他端池令牌）；**令牌攻击**（alg=none、算法混淆、错误 aud/kid、过期、被撤销会话、刷新令牌复用检测）；**提现并发**（并发申请不超额、幂等重放、复核人=发起人被拒、冻结账号被拒、对账冻结时拒绝）；**幂等绑定**（同键不同体 422）；**回调伪造**（无签名/过期/重放）；**CORS 跨端**；**限流与封禁**；**输入畸形**（超大体、错误类型、深层 JSON、Unicode 边界、NUL 字节）；**日志脱敏扫描**（在测试里触发各类请求，捕获全部日志输出，断言不含令牌、initData、完整手机号、密钥、收款账户、聊天原文的样本串）。
2. 依赖与镜像扫描进 CI：`govulncheck`（已有，保持阻断）、前端 `pnpm audit --prod`（高危阻断，允许带到期日的豁免清单）、容器镜像扫描（Trivy，镜像构建 job 里运行，高危阻断）、密钥扫描（已有脚本，补 gitleaks 规则）、GitHub Actions 版本固定检查；扫描结果摘要写入 PR 注释或 job summary。
3. （可选，手动触发 workflow）OWASP ZAP baseline 对 test 环境 API 的被动扫描，结果归档；不对生产运行。
4. 产出 `docs/security/TESTPLAN.md`：威胁→测试映射表，已覆盖与未覆盖项；发现的高危问题单独列出建议修复任务（本任务内修复仅限测试基础设施，业务缺陷另开任务）。
【验收标准】
① 威胁表（API 安全设计文档第一章）每一行至少有一个自动化测试对应
② 越权、重放、令牌攻击、提现并发、日志脱敏全部通过；依赖漏洞扫描无高危（或有带到期日的豁免）
③ 日志脱敏扫描在测试里真实捕获日志输出
④ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- ZAP 是否纳入常规流程
【红线】不得对生产或第三方系统发起扫描；不得提交真实令牌作为测试夹具。
```

---

## Q-06 压测（20 倍规模）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】Q-06 压测（20 倍规模）
【分支】task/Q-06-load-20x
【依赖】S-05、W-06（主线接口齐全）
【先阅读】
1. loadtest/（S-05 的脚本与 REPORT.md）；Dr.life+技术方案与系统边界.md 第十一章（目标：常规查询 P95 < 500 ms、写 < 1 s、可用性 99.5%）；Dr.life+开发任务拆分与验收.md：Q-06、S-05
【范围——要做】
1. 在 S-05 脚本基础上加入**真实业务主线**场景：入仓 → 下单 → 记凭证 → 发货托管 → 签收分账 → 月度对账单 → 提现 的混合负载（20 倍规模：约 100 家商户、1 万买家、数百司机的等效并发与数据量，造数脚本通过业务接口生成，不直接插表）；账务并发（同商户并发发货/提现/广告划扣）。
2. 数据量：造数到 20 倍规模（商品 50 万、订单 50 万、分录 500 万、搜索日志等）后测量查询与写入 P95；EXPLAIN 回归；慢查询清单与索引建议。
3. 结论写入 `loadtest/REPORT-20x.md`：各场景结果、瓶颈、容量上限估计、调参建议（连接池、并发上限、队列并发、缓存）；账务并发测试结束后跑 `ledgerctl recon`，恒等式成立。
【验收标准】
① 20 倍规模下接口 P95 达标（查询 < 500 ms，写 < 1 s）；账务并发无异常、对账通过
② 报告完整，瓶颈与建议明确
【待确认（不要自己拍板，PR 里列出）】
- 压测环境的硬件应尽量贴近机柜配置（需用户提供配置）
【红线】只在本地/测试环境；不得直接插表造数。
```

---

## Q-07 端到端主线（Playwright）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】Q-07 端到端主线（Playwright）
【分支】task/Q-07-e2e-main-flow
【依赖】U-03..U-07（前端主线页面）、W-09、Q-01
【先阅读】
1. Dr.life+开发任务拆分与验收.md：Q-07；Dr.life+产品需求文档PRD.md 第 4 章核心流程；Dr.life+交互文档.md 各端关键流程
【范围——要做】
1. 引入 Playwright（版本固定，仅 devDependency，浏览器下载走 CI 缓存）：`e2e/` 目录；用**测试 Telegram 环境模拟器**（开发模式下的 initData 模拟，参照 U-02 的 dev 模式，**只在测试/本地构建启用，生产构建里不得存在**）登录三个 Mini App；wms/admin 用测试管理员账号（种子仅测试环境）。
2. 主线用例：商家入仓预约 → 仓库收货（wms Web）→ 买家下单 → 商家记付款凭证 → 商家发货（配送费托管）→ 司机接单与签收（含凭证）→ 分账 → 月度仓储费对账单 → 司机/商家提现 → 平台复核与出款；**每一步校验页面状态与账务流水**（通过后端 ledgerctl/只读接口断言：托管金额、分账、余额、恒等式）。
3. **多语言**：关键页面在 zh/en/km 下各跑一遍，截图基线（高棉语不得截断关键文字）；**越权**：用买家令牌打开商家页面/接口应被拒；弱网：模拟离线（司机补传）。
4. CI：独立 workflow（nightly + 手动），失败上传 trace/视频；**不是每个 PR 必需检查**，但上线门槛要求最近一次通过。
【验收标准】
① 主线端到端跑通并校验账务（恒等式成立）
② 三语截图基线；越权用例通过；弱网补传用例通过
③ 生产构建中不存在测试登录入口（构建产物扫描测试）
【红线】测试登录入口不得进入生产构建；不得把测试账号写进生产种子。
```

---

## Q-08 试点上线检查

> 多数是人工核对与演练；Codex 的部分只是把清单自动化、产出核对报告。

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】Q-08 试点上线检查
【分支】task/Q-08-pilot-readiness
【依赖】Q-01..Q-07
【先阅读】
1. Dr.life+产品需求文档PRD.md 第 9 章（试点上线前规划 P0 三批）；Dr.life+技术方案与系统边界.md 第十二章『上线门槛』与第十四章（风险）；Dr.life+开发任务拆分与验收.md：Q-08、第十二章清单
【范围——要做】
1. `docs/launch/READINESS.md`：逐条对照 PRD 第 9 章三批功能与《数据库》第十一章账务用例、18 项审计、安全测试、备份演练、压测报告的**状态表**（已完成/证据链接/责任人/待办），缺失项一目了然。
2. 自动化上线前检查脚本 `scripts/preflight.sh`：校验环境（APP_ENV=prod 时：无演示数据、无测试密钥、两个 Redis 策略正确、迁移版本一致、审计锚定近期、备份近期、对账昨日已完成且通过、无未解除的出款冻结、18 项审计无『异常』、所有 `TODO-KM-REVIEW` 高棉语文案清单与法律文本『需律师审核』确认状态）；输出可读报告并以非零退出码阻止上线。
3. 试点方案执行清单：3 家试点商户的开通步骤（入驻、核验、员工、仓储入仓、首单）、回退预案、支持联系方式与每日巡检清单（对账、队列、告警、客服）。
【验收标准】
① READINESS.md 覆盖 PRD 第 9 章全部条目并链接证据
② preflight 在未满足条件时拒绝上线（用故意破坏的测试环境验证）
【待确认（不要自己拍板，PR 里列出）】
- 高棉语文案与法律文本的人工审校结果（用户提供）
- 3 家试点商户名单与上线日期
【红线】不得为了让检查通过而放宽检查项。
```

---

## 评审要点（给我自己用）

| 任务 | 重点 |
|---|---|
| L-08 | 文件级/行级幂等；SUSPENSE 的使用而非猜测；金额精度；人工处理的 SoD；与 ConfirmPayout 的衔接 |
