# Codex 任务提示词：S-01 至 S-06（API 安全协议、容量保护、队列）

**依据文档：** `Dr.life+API安全协议与容量及队列设计.md`（以下简称"设计文档"，**每个任务都要先读相应章节**）。
**用法：** 整块复制代码块内容粘贴给 Codex。基线 `origin/main` 最新提交。已合并迁移一律不得修改，变更只能新增迁移（先看 `backend/migrations/README.md` 取下一个可用序号；L-07b 可能占用 00011）。
**依赖与并行：**
- **S-01、S-02、S-03 互不依赖，可以同时开**（S-01 与 S-02 都动 `backend/internal/platform/gateway/` 与 `server/`，**建议 S-01 先合并，S-02 再 rebase**；S-03 在独立目录）。
- **S-04 依赖 S-03；S-05 依赖 S-01、S-02、S-03；S-06 是前端，依赖 S-02 的 `Retry-After` 约定，可与 S-02 并行开发。**
- A-02（登录）若还没开工，请在它的提示词末尾追加"令牌与会话以设计文档第三章为准"（见文件末尾"A-02/A-03 追加说明"）。
合并前确认 `ci-gate` 为绿。评审重点见文末。

---

## S-01 API 安全协议落地（网关）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】S-01 API 安全协议落地（网关层）
【分支】task/S-01-api-security，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（规则 9、10、11、13、14）
2. Dr.life+API安全协议与容量及队列设计.md：第一、二、三、四章（本任务的规范来源）
3. backend/internal/platform/gateway/（access.go、cors.go、identity.go、idempotency.go、limiter.go 及测试、README）、backend/internal/platform/server/server.go、httpx/、config/config.go、backend/api/openapi.yaml 与 backend/internal/api/
4. Dr.life+开发任务拆分与验收.md：F-05、A-02、A-03、A-06、Q-05

【背景】
F-05 已有：角色-路径守卫、统一 CORS 白名单、IP/账号限流、幂等头解析。设计文档要求在此基础上补齐"各端与服务端之间的接口安全协议"。本任务**只做网关层与接口规范**，不实现登录与会话本身（A-02/A-03），但要把它们需要的接口位置留好。

【文件归属】backend/internal/platform/gateway/、server/、httpx/、config/、openapi.yaml（只增加通用组件：错误码、头、x-roles 校验）、新增 backend/internal/platform/securityhdr/ 等小包均可；更新 gateway/README 与 backend/.env.example。不要改 iam、ledger。

【范围——要做】
1. **按端分组的 CORS（设计文档 3.3）**：把全局 `CORS_ALLOWED_ORIGINS` 改为"路径前缀 → 允许的 Origin 列表"的配置（默认值按设计文档表格；dev 环境额外允许 localhost）。规则：无 Origin 的请求不受 CORS 影响；有 Origin 但不在该路径分组的白名单 → 预检与实际请求都拒绝（不返回 CORS 头）；`Vary: Origin`；预检缓存 `Access-Control-Max-Age: 600`；不返回通配符；`Access-Control-Allow-Credentials: true` 只对"带 Cookie 的刷新端点"启用（配置里用显式路径列表声明，当前为空）。测试：买家 Origin 访问 /v1/merchant/* 被拒；官网 Origin 只能访问 /v1/public/*；各分组正反例；预检。
2. **统一安全响应头（设计文档 2.3）**：中间件为所有响应加 nosniff、Referrer-Policy、CSP（`default-src 'none'; frame-ancestors 'none'`）、CORP、HSTS（仅在 `APP_ENV` 非 dev 时）、`Cache-Control: no-store`（已鉴权路径；`/v1/public/*` 的 GET 由路由显式声明才允许缓存，默认 no-store）；移除 Server 等指纹头；错误响应、404/405/429/503 也要带；测试逐头断言。
3. **请求体与头限制（设计文档 2.4）**：`http.Server.MaxHeaderBytes = 16KB`；全局 `MaxBytesReader` 中间件（默认 64 KB，可按路由类覆盖，超限返回统一错误码 413 对应的业务码）；`Content-Type` 强制 `application/json`（对声明为 multipart/webhook 的路由豁免，配置里显式列出）；对 JSON 写接口提供严格解码辅助函数（拒绝未知字段与尾随内容），供后续各模块使用；测试：超大体、错误 Content-Type、畸形 JSON、未知字段。
4. **幂等键绑定请求内容（设计文档第四章）**：在已有 `RequireIdempotency` 的基础上实现完整存储：`Store` 接口（Redis 实现 + 内存实现用于测试/降级），键 = (账号, 方法+路由模板, 幂等键)，值 = (请求体 SHA-256、状态 processing|done、响应状态码、响应体摘要或完整小响应、创建时间)，TTL 24 小时。行为：首次 → 标记 processing，放行，完成后保存响应；同键同体且已完成 → 直接返回第一次的响应并加 `Idempotent-Replayed: true`；同键不同体 → 422（新增业务错误码）；同键处理中 → 409 + `Retry-After`；处理失败（5xx）不缓存成功响应，允许重试。**资金类路由（配置列表，默认含 /withdraw*、/ledger*、/admin/finance*）在 Redis 不可用时返回 503，不得无保护执行**；非资金类降级为放行并记录指标。响应体缓存要限制大小（默认 64 KB，超出只存状态码与摘要并在重放时返回统一的"已处理"响应）。测试：并发 50 个同键同体只执行一次；同键不同体 422；Redis 故障（用假 Store）下资金/非资金行为；跨账号同键互不影响；过期。
5. **Step-up（二次验证）令牌中间件骨架（设计文档 3.4）**：定义 `StepUpVerifier` 接口（验证 `X-Step-Up-Token`：签名、`scope` 与路由声明的动作一致、账号一致、5 分钟内、`jti` 单次有效）；路由级声明 `RequireStepUp("withdraw")`；验证通过后在 request context 里设置 `StepUpVerified` 标记（**不可由请求头直接设置**，测试证明伪造头无效）；缺失/失效返回专用错误码。**验证器的具体实现（签发）属于 A 系列任务**，这里提供接口、内存 fake 与 Redis 的 jti 去重实现，并在 README 写明对接方式。幂等重放不消耗新的 step-up（已完成的幂等结果直接返回）。
6. **后台路径 IP 白名单与更严规则（设计文档 3.2）**：为 /v1/admin/*、/v1/wms/* 增加路径级配置：来源 IP 白名单（CIDR 列表，默认空 = 在 dev 放行、在非 dev 环境**空白名单即全部拒绝**，防止忘记配置而对公网开放）、更严的限流规则集。IP 取自已有的可信代理解析。测试：白名单内/外、IPv6、可信代理头伪造。
7. **Webhook 验签框架（设计文档第一章回调伪造）**：在 `PUBLIC_WEBHOOK_PREFIX`（或明确的 `/v1/webhook/*` 前缀）下，提供 `WebhookVerifier` 接口与三种内置实现：①固定头密钥（Telegram `X-Telegram-Bot-Api-Secret-Token`，恒定时间比较）；②HMAC-SHA256(body + 时间戳) 签名（头里带时间戳与签名，容差默认 5 分钟，随机数/事件号去重用 Redis 保存 10 分钟）；③来源 CIDR 限制。每个 webhook 路由必须绑定一个验证器，**未绑定验证器的 webhook 路径一律 404/拒绝**（fail-closed）；请求体大小上限；验证失败只返回统一错误、记录日志（不含密钥与完整请求体）。测试：正确、篡改、过期、重放、缺头、时序（用恒定时间比较的单测）。
8. **OpenAPI 与授权矩阵测试（设计文档 3.5 第 4 点）**：要求 openapi.yaml 每个操作声明 `x-roles`（已有标签级声明，扩展为操作级校验）；写一个测试读取 openapi.yaml，对**每个操作**自动生成请求并断言：无令牌 → 401；错误角色令牌 → 403（用测试认证器伪造各角色令牌）；没有 `x-roles` 的操作使测试失败。当前只有 /v1/public/ping，测试要能随接口增加自动覆盖（新增接口无需改测试，缺声明即失败）。把该测试纳入 CI 的 backend-tests。
9. **统一认证失败处理**：401 的响应体与时间对"无令牌/格式错/过期/签名错"保持一致的外部表现（只在日志里区分原因，且不含令牌），加测试；`WWW-Authenticate: Bearer` 头。
10. **配置与文档**：所有新增阈值走 config（有默认值与校验）；`backend/.env.example` 只放占位符；gateway/README 增加"协议总览"，并在 PR 里列出对《设计文档》的任何偏离与待确认项。

【测试】真实依赖：用 testcontainers 起 Redis 测幂等存储与 jti/nonce 去重；其余用内存实现。全部 `-race`。每条规则正反例；新增中间件的基准测试（每请求增量延迟与分配，写进 PR）。

【验收标准】
① 按端 CORS 分组正确，跨端 Origin 被拒；② 所有响应带安全头；③ 超大体/错 Content-Type/未知字段被拒；④ 同幂等键不同内容 422，同内容重放原响应，并发只执行一次，资金路由在 Redis 故障时 503；⑤ Step-up 骨架可用且头部无法伪造；⑥ /v1/admin/*、/v1/wms/* 白名单默认拒绝（非 dev）；⑦ webhook 未绑定验证器即拒绝，三种验证器有测试；⑧ 授权矩阵测试自动覆盖 openapi 全部操作并进 CI；⑨ go vet、-race、OpenAPI 无差异、ci-gate 全绿；性能开销数据已记录。

【红线】不放宽现有任何守卫；不在日志/错误里放令牌、幂等响应原文、webhook 密钥；不引入重型框架；不改已合并迁移。

【待确认（不要自己拍板）】资金路由列表的准确前缀（现在只能按路径约定写，业务路由出现后需要同步）；幂等响应缓存大小；HSTS preload。

【交付】PR 描述：每章规则对应的实现与测试、配置项清单、基准数据、对设计文档的偏离、待确认事项。
```

---

## S-02 过载保护与容量（分舱、降级、连接池、缓存）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree（若 S-01 已合并，基于其合并后的 main；否则请先 rebase 约定：S-01 先合并）。

【任务】S-02 过载保护与容量（路由分舱、过载丢弃、数据库连接池分舱与超时、缓存层）
【分支】task/S-02-overload-protection，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md；Dr.life+技术方案与系统边界.md 第十一章（非功能指标）
2. Dr.life+API安全协议与容量及队列设计.md：第五章全部
3. backend/internal/platform/{gateway,server,db,config,observability}/、cmd/server/main.go、db 包的集成测试风格
4. backend/internal/ledger/ 里对 pgxpool 的使用方式（Service 持有的连接接口），不要破坏

【范围——要做】
1. **路由分类与并发上限（设计文档 5.3）**：中间件 `Bulkhead`：路由类别 `critical/auth/write/read/heavy/health`（用路由元数据声明——先用配置里"路径前缀/模板 → 类别"的映射，默认规则按设计文档；OpenAPI 里可用扩展 `x-class` 声明，已存在的 ping 归 read）。每类一个信号量（并发上限可配置，默认见设计文档），全局另有总在途上限（默认 512）；`critical` 预留总量的 25%（即其他类合计最多占用 75%，critical 可使用全部）；**获取许可最多等待 100 ms（可配置），超时返回 503 + `Retry-After`（1–3 秒随机抖动）+ 统一错误码**；`/healthz`、`/readyz` 不受限；每个请求在类别对应的**截止时间**内处理（`context.WithTimeout`），超时返回 503/504 统一错误，且 handler 要能感知 ctx 取消。指标：各类在途数、等待时间直方图、拒绝数（按类别）。
2. **自适应降级 brownout（设计文档 5.3 末）**：监控信号（最近 30 秒 P95 延迟、数据库连接池等待时间、在途占比）超过阈值时，自动把 `heavy` 并发上限减半（可降到 1），并暴露"降级等级"给业务层（`overload.Level(ctx)`，供搜索/AI/推荐降级使用）；恢复需连续 60 秒低于恢复阈值，逐级放开，避免抖动；全部阈值可配置；每次状态变化写 INFO 日志与指标。提供可注入的时钟与信号源以便测试。
3. **数据库连接池分舱与超时（设计文档 5.4）**：`oltp`（接口与账务，主库）、`report`（只读从库/或同库只读账号，REPORT_DATABASE_URL 已有）、`worker`（队列工作进程）三个 pgxpool，各有 MaxConns 配置与默认值（先 oltp 10、report 4、worker 4，并在 README 给出"每台应用服务器合计 ≤ max_connections 的 40%"的预算公式与示例）；连接建立时设置 `statement_timeout`（oltp 5s、report 30s、worker 60s）、`lock_timeout=3s`、`idle_in_transaction_session_timeout=10s`（用 RuntimeParams 或 AfterConnect）；**账务模块的长事务例外用事务内 `SET LOCAL statement_timeout` 提高到 15s，不要全局放宽**；获取连接超时要返回可重试的统一错误，而不是无限等待。更新 observability 的连接池指标为按池标签。**不得改变 ledger 包对数据库的接口**（它要的是 `beginner` 接口，传 oltp 池即可）。
4. **缓存层（设计文档 5.5）**：新包 `backend/internal/platform/cache/`：接口 `Cache`（Get/Set/Delete，TTL 带随机抖动 ±10%）；Redis 实现 + 内存实现；`Remember(key, ttl, load)` 带 **singleflight**（同键并发回源只一次，测试 100 并发回源只调用 1 次）；**stale-while-revalidate**（返回过期不超过 X 的旧值并后台刷新，刷新失败保留旧值）；负缓存（短 TTL 缓存"不存在"，防穿透）；键命名规范与前缀（含环境名与版本）；**禁止缓存资金/库存/订单状态**——提供 `NoMoney` 键前缀白名单检查（键必须以允许的命名空间开头，如 `catalog:`、`config:`、`i18n:`、`hot:`，其余 panic/返回错误），并在 README 写明；Redis 故障时降级为直接回源并记录指标（不得让缓存故障拖垮接口）。
5. **公开 GET 的缓存头辅助（与 S-01 协调）**：为声明可缓存的 `/v1/public/*` GET 提供 `CacheControl(public, maxAge, swr)` 与 ETag 中间件（If-None-Match → 304）；默认仍是 no-store。
6. **优雅关闭（设计文档 5.7）**：收到终止信号时先让 `/readyz` 返回不就绪（可配置的"摘除等待"默认 5 秒，让 Tunnel/负载均衡摘除），再 Shutdown 等待在途请求；Bulkhead 关闭期间对新请求立即 503。测试用真实 http.Server 验证：关闭期间新请求被拒、在途请求完成。
7. **查询保护辅助**：分页参数校验器（page_size ≤ 100、page ≥ 1、keyset 游标工具）供各模块复用；`filter`、`sort` 白名单校验辅助（未知字段拒绝）；测试。
8. **配置与文档**：新增配置带默认值与校验；README 写清分类表、预算公式、降级信号、如何调参；`backend/.env.example` 占位。

【测试】
- 单元与集成（-race）：Bulkhead 在 1000 并发下各类互不挤占、critical 预留生效、等待超时返回 503 且带 Retry-After；降级状态机（注入信号与时钟）上行/下行/防抖；连接池参数确实生效（查询 `SHOW statement_timeout` 等）；cache 的 singleflight/SWR/负缓存/Redis 故障降级；优雅关闭；
- 用 testcontainers（PostgreSQL + Redis）；
- 基准：Bulkhead 与缓存中间件每请求开销。

【验收标准】
① 一类路由打满不影响其他类，critical 始终可用；② 过载时 503 + Retry-After 快速返回（等待 ≤ 100 ms），无请求堆积；③ 降级与恢复按阈值自动发生且有日志/指标；④ 三个连接池各有上限与语句超时，账务可局部提高；⑤ 缓存层防击穿、可降级，资金命名空间被拒；⑥ 优雅关闭正确；⑦ go vet、-race、ci-gate 全绿；基准数据已记录。

【红线】不改 ledger 的对外接口与行为；不缓存资金/库存/订单状态；不放宽任何超时到"无限"；不用 sleep 实现等待（用上下文与定时器）。

【待确认】各类并发默认值（压测后调整）；report 池是否指向真正的只读从库（部署相关）。

【交付】PR 描述：分类表与默认值、降级信号与阈值、连接池预算、基准数据、测试对照、待确认事项。
```

---

## S-03 队列平台（asynq、发件箱转发、背压、死信）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】S-03 队列平台（asynq 接入、队列隔离、发件箱转发、消费幂等、背压、死信与运维命令）
【分支】task/S-03-queue-platform，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（规则 8、14）；Dr.life+第一版交付形态与技术架构.md 第六章技术选型中"队列/定时"与 Redis 行
2. Dr.life+API安全协议与容量及队列设计.md：**第六章全部**（本任务的规范）
3. backend/internal/ledger/outbox.go（ClaimOutbox、OutboxBatch：已有的 SKIP LOCKED 取批与 MarkSent/Release）、ledger 里所有 outbox topic（grep `ledger.outbox` 的 INSERT，列出现有 topic：`ledger.txn.posted`、`ledger.txn.reversed`、`ledger.txn.adjusted`、`ledger.withdrawal.*`、`ledger.recon.failed`、`ledger.payout_freeze.released` 等）
4. backend/internal/platform/{config,observability,server}/ 与 cmd/server/main.go、deploy/observability/

【背景】
账务等模块已在**同一事务里写 outbox**（至少一次的前提）。现在需要真正的转发与消费平台：可靠、隔离、可观测、不无限膨胀。asynq 要求 Redis 使用 noeviction，因此**队列用独立的 Redis 实例/配置**（设计文档 6.1）。

【文件归属】backend/internal/platform/queue/（新增，核心）、backend/cmd/queuectl/（运维命令）、backend/cmd/worker/（工作进程入口，或在 cmd/server 增加 `--role=worker`，二选一并说明）、config 新增配置、deploy/ 下的 compose 示例（redis-cache 与 redis-queue 分开）、迁移（`ops` schema 的 processed_events 等，见下）、observability 指标接入。**不要修改 ledger 的对外接口**；转发进程通过 ledger 现有的 `ClaimOutbox`/`MarkSent` 工作（若发现接口不足，在 PR 里提出需要的最小扩展，不要擅自改账务语义）。

【范围——要做】
1. **配置与两个 Redis（设计文档 6.1）**：新增 `QUEUE_REDIS_URL`（与 `REDIS_URL`（缓存/限流）分开）；启动时对队列 Redis 检查 `maxmemory-policy`，**不是 noeviction 则拒绝启动 worker 并给出明确错误**（可用配置项在 dev 放宽）；deploy 示例 compose 里起两个 Redis（队列实例 AOF everysec + noeviction + maxmemory）。
2. **队列与隔离（设计文档 6.2）**：队列 `critical/default/notify/ai/batch/scheduled`，每类**独立的 asynq Server**（各自并发池，默认并发见表，可配置）；统一的 `queue.Enqueue(ctx, task, opts...)` 封装：任务类型命名 `domain:action:v1` 校验、载荷大小上限（默认 4 KB）、必须声明 Timeout/MaxRetry/队列（通过任务类型注册表 `Register(TypeSpec{Name, Queue, Timeout, MaxRetry, Backoff, SkipRetryOn})` 集中声明，**未注册类型不能入队/消费**）；重试退避用 asynq `RetryDelayFunc`：指数 + 抖动 + 上限（按类型可覆盖）；永久性错误用 `asynq.SkipRetry` 包装（提供 `queue.Permanent(err)`）；panic 捕获中间件（记录类型与任务编号，不记载荷）；日志脱敏沿用 logx。
3. **入队去重与背压（设计文档 6.3 / 6.5）**：支持 `TaskID`/`Unique` 选项；入队前检查目标队列深度上限（`asynq.Inspector`；默认 notify 50000、ai 2000、batch 10000，可配置）：`critical` 永不拒绝；超限时按类别策略（`ErrQueueFull` 或丢弃低优先级并计数——策略在 TypeSpec 里声明）；检查结果可缓存 1 秒避免每次入队都查 Redis。Redis 内存使用率采样（INFO memory），超过 70% 记警告指标、85% 对 batch/notify 营销类停止入队。
4. **发件箱转发进程（设计文档 6.4，最关键）**：`outbox.Relay`：循环 ClaimOutbox（批量默认 100，可配置）→ 对每个事件按 topic 路由表入队（`TaskID = "outbox:<id>"`）→ 全部入队成功后 `MarkSent`；**任一入队失败则 Release 整批**（下次重来，靠 TaskID 去重）；空闲退避（最长 2 秒）、繁忙连续拉取；未知 topic **进入死信/隔离（记录并 MarkSent 到 `dead` 标记，或写入专门的 quarantine 记录，由你选一种并说明；不得静默丢弃也不得无限重试）**；暴露指标：最老未发送事件年龄、转发速率、批次耗时；**滞后 > 60 秒告警规则**写进 deploy/observability/prometheus/alerts.yml。多实例同时运行 Relay 必须安全（SKIP LOCKED）。
5. **消费幂等（设计文档 6.3 第 2 点）**：迁移新增 `ops.processed_event(consumer text, event_id text, processed_at timestamptz, primary key (consumer,event_id))`（schema 以 `ops` 为准，授权给相应角色，只增；清理策略：保留 30 天，提供清理任务）；提供 `queue.Idempotent(consumer, handler)` 包装：在同一数据库事务里"插入已处理记录 + 执行业务"，唯一冲突视为已处理直接成功；对账务类事件文档中注明"以账务幂等键为准"。
6. **定时任务（设计文档 6.6）**：封装 Scheduler：每个定时任务声明 (名称, cron, 队列, 任务 ID 生成规则 = 名称 + 时间窗)，**多实例同时运行不会重复入队**（TaskID 去重 + 处理函数内 advisory lock 兜底）；补跑机制：启动时检查最近 N 天（可配置，默认 3）未完成的"日任务"（通过业务幂等键或任务记录判断）并补入队；为现有的每日对账（`Service.RunDailyReconciliation`，注意只能处理**已结束的日期**，见 L-07b）注册一个示例定时任务但**默认关闭**（配置开关）。
7. **优雅关闭与崩溃恢复（设计文档 6.10）**：先停止取新任务，再等待在途任务至 ShutdownTimeout；演示（测试）"杀掉工作进程后任务被其他实例重新执行"；长任务提供"心跳/分批保存游标"的辅助说明（不必实现具体长任务）。
8. **运维命令 `queuectl`**：子命令 `stats`（各队列深度、等待时间、处理/失败/重试/死信数）、`pause/resume --queue`、`dead list/replay/delete`（重放前打印警告，需 `--yes`）、`outbox lag`（最老未发送事件年龄与数量）；输出不含载荷敏感内容；退出码规范；asynqmon 之类网页管理**不内置**，README 写明只能放 WireGuard 内网。
9. **指标与告警（接 F-11）**：队列深度、最老任务等待时间、处理速率、失败/重试/死信数、任务耗时直方图（按类型，**标签只用任务类型和队列，不用任务编号**）、Relay 滞后、Scheduler 漏跑、Redis 内存；告警规则写入 deploy/observability/prometheus/alerts.yml（死信增长、各队列等待超 SLA：critical 10s、default 60s、notify 120s、Relay 滞后 > 60s、Redis 内存 > 70%）并用 promtool 校验（README 写命令）。
10. **一个端到端示例消费者**：为 `ledger.txn.posted` 写一个"示例处理器"（只是计数/日志，用 Idempotent 包装），证明从账务入账 → outbox → Relay → asynq → 消费者的完整链路；不实现真实通知。

【测试（真实 Redis + 真实 PostgreSQL，testcontainers，-race）】
1. **至少一次且无副作用**：在"入队成功、MarkSent 之前"注入崩溃（用可注入的故障点），重启 Relay 后事件被重新入队，因 TaskID 去重只产生一个任务；消费者幂等，业务效果只一次；
2. **多实例 Relay**：3 个 Relay 并发，1000 个事件，每个事件恰好被消费一次（以幂等记录计数）；
3. **隔离**：向 notify 队列灌入 10 万个慢任务，critical 队列的任务等待时间仍低于 SLA（独立 Server 并发池）；
4. **重试与退避**：暂时性错误按退避重试、永久性错误立即进死信；panic 进失败并不崩溃进程；
5. **背压**：超过队列深度上限时按策略拒绝/丢弃并计数，critical 永不拒绝；
6. **死信与 queuectl**：重试耗尽进入死信，replay 后成功；未注册类型无法入队；未知 outbox topic 被隔离；
7. **定时任务**：两个 Scheduler 实例同时运行同一定时任务，同一时间窗只执行一次；补跑；
8. **优雅关闭/崩溃**：关闭时在途任务完成；强杀工作进程后任务被重新执行（至少一次，消费者幂等）；
9. **Redis 配置检查**：非 noeviction 时 worker 拒绝启动；
10. **端到端**：账务入账 → 示例消费者收到恰好一次；
11. **指标**：关键指标存在且标签低基数（测试 1000 个不同任务编号后时间序列数不增长）；
12. promtool 校验告警规则。

【验收标准】
① 发件箱转发至少一次、无重复副作用，崩溃可恢复；② 各队列相互隔离，洪水不影响 critical；③ 任务类型集中注册、有超时/重试/退避/永久性错误区分；④ 死信可见、可重放、告警；⑤ 背压生效，Redis 内存有告警；⑥ 定时任务多实例不重复；⑦ queuectl 可用；⑧ 队列 Redis 配置被强制检查；⑨ go vet、-race、集成测试、ci-gate 全绿。

【红线】不修改 ledger 的账务语义；载荷不放个人信息/令牌/金额；指标标签不用任务编号/用户编号；不用 sleep 实现限速（令牌桶在 S-04）；不把队列 Redis 与缓存 Redis 合用一个策略。

【待确认】队列深度上限与并发默认值；quarantine 方案；是否接受两个 Redis 实例（设计文档 8 节第 1 条）。

【交付】PR 描述：队列/类型注册表清单、Relay 的故障恢复说明、测试对照、告警规则、待确认事项。
```

---

## S-04 Telegram 通知发送（令牌桶限速、429 处理、Bot 池故障转移）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。S-03（队列平台）已合并（未合并则先等）。请基于 origin/main 最新提交新建独立 worktree。

【任务】S-04 Telegram 通知发送（限速、429 处理、故障转移、群发拆分）
【分支】task/S-04-telegram-sender，只提交到这个分支并发起 PR，不要推 main。

【先阅读】
1. AGENTS.md（规则 11、14）；Dr.life+第一版交付形态与技术架构.md 第二章（Bot 池、通知授权、应急切换）
2. Dr.life+API安全协议与容量及队列设计.md：6.7、6.2、6.3
3. backend/internal/platform/queue/（S-03）；backend/migrations/00003_iam_tables.sql（iam.bot、iam.account_bot）；poc/telegram-initdata/RESULT.md 里对 go-telegram/bot 的选型结论与限额备注；Telegram Bot API 官方文档关于 flood limits（以官方为准，数值可配置）

【范围——要做】
1. **适配层（规则 14）**：`backend/internal/comms/telegram/`：接口 `Sender`（SendMessage(ctx, botID, chatID, text, opts) → 结果：成功/限流(retry_after)/用户屏蔽(403)/聊天不存在/临时错误/永久错误），底层用 `go-telegram/bot`（PoC 选型）或直接 HTTP；**超时、熔断、重试策略在适配层声明**；调用日志不含消息正文与令牌；令牌通过 F-10 的 Secret 与解密缓存获取（沿用 A-02 的做法，若 A-02 未合并则定义接口并用假实现）。
2. **按 Bot 令牌桶（Redis 实现，多实例共享）**：每个 Bot 全局速率（默认 25 条/秒，可配置）+ 每个聊天最小间隔（1 秒）；`Acquire` 不够时返回"需等待 d"，**调用方据此用 `ProcessIn(d)` 重新排期，禁止在处理函数里 sleep 占用 worker**；提供 Lua 脚本原子实现；时钟可注入便于测试。
3. **429 处理**：收到 `retry_after`：该 Bot 进入"降速期"（速率临时减半，持续 retry_after × 2，可配置），当前任务用 `ProcessIn(retry_after + 抖动)` 重排，**不计入重试次数**（或单独计数上限，避免无限）；记录指标。
4. **403/用户不可达**：标记 `iam.account_bot.unreachable_at`（通过 iam 提供的接口/事件，不直接跨 schema 写；若接口不存在先定义接口 `ReachabilityRecorder` 并用 fake），**不重试**；自动改用该用户绑定的其他可用 Bot（按池内优先级，跳过 banned；limited 的 Bot 降低优先级）；全部不可达 → 记"不可达"指标与事件（`comms.notification.unreachable`），不再重试；
5. **任务类型（注册到 S-03 的注册表）**：`comms:notify_user:v1`（单发，queue=notify，载荷只有通知编号）、`comms:broadcast_chunk:v1`（群发块，queue=notify 低优先级）、`comms:broadcast_plan:v1`（父任务，queue=batch：拆块、保存游标、可暂停/取消/续跑）。**事务性通知（资金、订单）与营销通知使用不同优先级/队列权重**，营销在积压时先被丢弃（S-03 背压策略）。通知按 (用户, 事件) 去重；语言取账户登记语言（模板接口，三语文案不在本任务）；消息正文从数据库/模板在发送时渲染，**不存入队列载荷**。
6. **Webhook（Telegram 入站，可选最小版）**：用 S-01 的 webhook 验签框架（固定头密钥）接收 `my_chat_member` 等更新，用于更新"用户是否屏蔽/重新开始"（调用 ReachabilityRecorder）；若 S-01 未合并，先只做 Sender 部分并在 PR 里说明。
7. **假 Telegram 服务器（测试用）**：在测试里起 httptest 服务器模拟：正常、429 + retry_after、403、超时、5xx，计时精度可控。
8. **指标**：每个 Bot 的发送成功率、429 次数、403 次数、令牌桶等待时间、群发进度；告警规则（发送成功率 < 95%、429 激增）加入 prometheus alerts 并校验。

【测试】
- 令牌桶：并发 1000 请求不超过每秒上限（用注入时钟与假 Redis 时间验证）、多实例共享；
- 429：不 sleep、重排期、降速；
- 403：不重试、故障转移到备用 Bot、全部不可达终止；
- 群发：10 万用户拆块、整体速率受限、暂停/取消/续跑（游标）、事务性通知不被营销阻塞（同时灌营销与事务通知，事务通知延迟低于 SLA）；
- 去重：同 (用户, 事件) 只发一次；
- 日志/指标/载荷不含消息正文、令牌、完整手机号（子串断言）；
- 全部 -race，用 testcontainers 起 Redis（+ PostgreSQL 如需）。

【验收标准】
① 任一 Bot 的发送速率不超过配置上限；② 429/403 按规则处理，worker 不被 sleep 占用；③ 主 Bot 被限制/封禁时自动改用备用 Bot；④ 群发可暂停、取消、续跑，事务性通知优先；⑤ 载荷与日志无敏感内容；⑥ go vet、-race、ci-gate 全绿。

【红线】不直接跨 schema 写 iam；令牌与正文不进日志/载荷/指标；不在 worker 里 sleep 限速；不绕过 S-03 的类型注册与背压。

【待确认】Telegram 限速具体数值（官方数值可能变化，默认保守并可配置）；是否需要短信兜底的接口位（只留接口，不实现）。

【交付】PR 描述：限速算法说明、故障转移规则表、群发拆分与游标设计、测试对照、待确认事项。
```

---

## S-05 容量与韧性验证（k6 + 故障演练）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。S-01、S-02、S-03 已合并（未合并则先等）。请基于 origin/main 最新提交新建独立 worktree。

【任务】S-05 容量与韧性验证（k6 压测脚本、过载与故障演练、报告）
【分支】task/S-05-load-resilience，只提交到这个分支并发起 PR，不要推 main。

【先阅读】
1. Dr.life+技术方案与系统边界.md 第十一章；Dr.life+API安全协议与容量及队列设计.md 第五章（5.8）、第六章
2. deploy/observability/（Prometheus、Grafana）、backend/Makefile、cmd/server、docker 相关
3. Dr.life+开发任务拆分与验收.md：Q-05、Q-06

【范围——要做】
1. 目录 `loadtest/`（k6 脚本 + README + 本地 compose：应用、PostgreSQL、两个 Redis、worker、Prometheus/Grafana，镜像版本固定；**只用于本地/测试环境，禁止对生产运行，README 与脚本里用环境变量守卫（TARGET_ENV 必须是 local/test，否则脚本拒绝执行）**）。令牌用测试签发工具（若 A-02 未合并，用测试认证器/固定测试密钥，不使用任何真实令牌）。
2. 场景（每个场景是一个脚本，有明确的阈值断言 `thresholds`）：
   a. **登录风暴**：60 秒内 N（默认 2000，可配置）个用户同时打开 Mini App 登录（用合成 initData）；断言 auth 类 P95、拒绝率、`Retry-After` 行为，期间 critical 与 health 不受影响；
   b. **日常混合**：读 85% / 写 10% / 重 5%，持续 10 分钟；断言常规查询 P95 < 500 ms、写 P95 < 1 s、错误率 < 0.5%；
   c. **资金写压力**：并发提现申请/入账类写（用测试路由或 ledger 测试入口），断言不超扣、幂等、无死锁，critical P95 < 1 s；
   d. **3 倍过载**：把 b 的负载拉到基线 3 倍，断言：`critical` 与 `health` 仍正常；`heavy` 类被降级/拒绝（503 + Retry-After）；拒绝响应本身很快（P95 < 200 ms）；无请求堆积；**进程 goroutine、内存、连接池使用在压力结束后恢复**（用 Prometheus 指标采样断言）；
   e. **重试风暴**：模拟客户端遇到 503 后无退避立即重试 vs 有指数退避+抖动，展示两者对系统的影响（报告对比）；
   f. **缓存击穿**：热点键过期瞬间 1000 并发，断言回源次数 ≈ 1；
   g. **队列洪水**：向 notify 灌 10 万任务，同时发 critical 任务与 API 请求，断言 critical 等待 < 10 s、API 不受影响；发件箱滞后 < 60 s；
   h. **长稳（soak）**：1 小时（默认缩短版 10 分钟，参数可调），断言无内存/goroutine/连接泄漏趋势。
3. **故障演练脚本**（docker compose 控制）：①停缓存 Redis（资金接口应 503、普通读应降级可用、限流回退内存）；②停队列 Redis（API 仍可入账，发件箱堆积，恢复后自动转发且无丢失无重复——用对账数据断言）；③PostgreSQL 从库延迟/不可用（报表池失败不影响 oltp）；④杀掉 worker（任务被其他实例/重启后恢复）；⑤杀掉一台应用实例（另一台继续服务，`/readyz` 摘除生效）。每项记录"期望行为/实际行为/结论"。
4. **安全回归场景**：在压力下仍然——越权请求 100% 被拒（401/403/404）、幂等重放只执行一次、限流对攻击者生效而对正常用户影响有限；简单的慢速攻击（slowloris 类，限速地发送请求头）不会占满连接（服务端读头超时生效）。
5. **报告**：`loadtest/REPORT.md` 模板与一次本地运行的结果（机器配置、版本、各场景结果表、瓶颈与建议、参数调整建议）；CI 里只做轻量烟雾版（短时、低负载，验证脚本可运行与阈值断言有效），完整压测手动触发（workflow_dispatch），**不得放进每个 PR 的必需检查**。

【验收标准】
① 全部场景脚本可运行且阈值断言有效（故意放宽服务端上限时脚本应报失败，证明断言不是摆设）；② 3 倍过载下 critical 正常、heavy 被降级、资源恢复；③ 五项故障演练都有结论且账务数据无丢失/重复；④ 报告完整，列出瓶颈与调参建议；⑤ 脚本有环境守卫，无法对生产运行；⑥ 不引入真实令牌或真实用户数据；⑦ CI 烟雾版绿。

【红线】不得对非本地/测试环境发压；不提交真实令牌；不为通过阈值而放宽阈值（需要调整的在报告里说明依据）。

【交付】PR 描述：场景表、故障演练表、本地运行结果摘要、发现的问题（若有，单独列出建议的修复任务）。
```

---

## S-06 前端接口客户端的韧性（packages/api-client）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree。

【任务】S-06 前端接口客户端的韧性与令牌处理
【分支】task/S-06-api-client-resilience，只提交到这个分支并发起 PR，不要推 main。

【先阅读】
1. Dr.life+API安全协议与容量及队列设计.md：3.1、3.2、第四章、5.6
2. packages/api-client/（由 OpenAPI 生成，README、测试）、packages/ui/src/telegram.ts、apps/*/src/main.tsx、packages/i18n

【范围——要做】
1. **请求包装层**（不改生成代码，在其外包一层）：统一 baseURL（沿用 `VITE_API_BASE_URL`）、统一错误模型（按后端错误码分段映射为类型化错误，**错误提示走 i18n 词条，不显示后端内部信息**）、请求超时（按类别默认：读 8s、写 12s、重操作 30s，可覆盖）、`X-Request-ID` 生成（8–64 位安全字符）。
2. **重试策略（设计文档 5.6）**：只重试幂等请求（GET，或带 Idempotency-Key 的写）；指数退避 + 随机抖动（全抖动），上限（默认最多 3 次、总时长 ≤ 15 秒）；**尊重 `Retry-After`**（取较大者并加抖动）；对 429/503/网络错误重试，对 4xx（除 408/429/409 的 Retry-After 情形）不重试；写请求**自动生成并保存 Idempotency-Key**，同一次用户动作的重试使用同一个键；用户重复点击同一按钮时复用同一键（提供 `useIdempotentAction` 之类的辅助）。
3. **熔断**：连续 N（默认 5）次 5xx/网络错误后进入"暂停期"（默认 10 秒，指数增长上限 60 秒），期间新请求快速失败并提示"稍后再试"（i18n），半开探测恢复；按"端点类别"而非全局熔断，避免一个接口坏了全部停摆。
4. **在途去重**：相同 GET（相同 URL+参数+语言）并发只发一次，结果分发给所有等待者；写请求不去重。
5. **令牌管理（Mini App 与 Web 两种模式，设计文档 3.1/3.2）**：
   - 访问令牌与刷新令牌**只放内存**（模块级私有变量，不写 localStorage/sessionStorage/Cookie；测试断言不写任何持久存储）；
   - **单飞刷新**：令牌过期（401 且错误码表示过期）时只允许一个刷新请求在途，其他请求等待其结果，刷新成功后重放；刷新失败（含复用检测导致的会话作废）→ 清空令牌并触发"需要重新登录"事件（Mini App：用最新 initData 重新登录；Web：回到登录页）；
   - 提供 `AuthProvider` 抽象接口（login、refresh、logout），具体登录实现由各应用注入（A-02/A-03 的接口生成后再接，这里用接口 + fake 实现与测试）；
   - **step-up**：收到"需要二次验证"错误码时，触发注入的 `StepUpHandler`，验证成功后带 `X-Step-Up-Token` 重放原请求一次（幂等键不变）。
6. **登录风暴缓冲（可选开关，默认关闭）**：Mini App 启动时登录请求加 0–2 秒随机延迟的辅助函数。
7. **日志与隐私**：客户端日志不输出令牌、initData、手机号；错误上报（如后续接 Sentry）前脱敏的辅助函数。
8. 文档：packages/api-client/README 写明用法、策略默认值、如何覆盖。

【测试（Vitest，假 fetch/假计时器，不引入重依赖）】
- 重试：退避序列与抖动范围、Retry-After 生效、写请求同键重试、4xx 不重试、最大次数/总时长；
- 熔断：触发、暂停期快速失败、半开恢复、按端点类别隔离；
- 去重：100 个并发相同 GET 只发 1 次；
- 单飞刷新：50 个并发请求同时遇到过期，只发 1 次刷新，全部成功重放；刷新失败触发重新登录事件；
- 令牌不落持久存储（spy localStorage/sessionStorage/document.cookie）；
- step-up 重放；
- i18n：错误提示走词条，且三语齐全（check-i18n 通过）；新增词条的高棉语带 TODO-KM-REVIEW。

【验收标准】
① 重试风暴不会发生（退避+抖动+Retry-After）；② 并发过期只刷新一次；③ 令牌不持久化；④ 熔断按类别；⑤ 错误提示三语、无内部信息；⑥ typecheck、lint、test、build、i18n:check 全绿；无新增外部 CDN；依赖版本固定。

【红线】不在客户端存令牌到持久存储；不显示后端原始错误信息；不无限重试；不为了测试方便暴露令牌到全局变量。

【交付】PR 描述：策略默认值表、令牌流程图、测试对照、待确认事项（二次验证 UI 由各应用实现）。
```

---

## A-02/A-03 追加说明（粘贴在 A-02 提示词【范围】末尾；A-03 开工时同样给）

```text
【令牌与会话以《API安全协议与容量及队列设计》第三、四章为准，以下覆盖 A-02 提示词中的相应条目】
1. 访问令牌 `aud` 按端区分（drlife:buyer / drlife:merchant / drlife:driver；后台 drlife:admin / drlife:wms），认证器按路径角色校验 aud；头部 typ=at+jwt，带 kid；只接受配置的算法。
2. Mini App 令牌与刷新令牌只在客户端内存（服务端无需任何 Cookie）；刷新令牌 7 天绝对、24 小时空闲（可配置）。**A-03**：刷新令牌每次使用即轮换，复用检测 → 整条会话链作废并告警；会话撤销与账号纪元号放 Redis，认证器每个请求 O(1) 检查，资金/后台路由 Redis 不可用时 fail-closed。
3. Web（admin/wms）刷新令牌走 HttpOnly+Secure+SameSite=Strict 的 host-only Cookie（仅 adminapi 域、Path 限定到各自的 auth 路径），刷新端点必须带自定义头并校验 Origin。该部分属于 A-06，A-02 不实现，但令牌签发接口要能为其复用。
4. 二次验证令牌（step-up）的**签发**留给后续任务；A-02 只需保证网关可挂接 S-01 的 `StepUpVerifier`。
```

---

## 评审要点（给我自己用）

| 任务 | 重点 |
|---|---|
| S-01 | CORS 是否真按路径分组且无 Origin 反射；幂等：同键不同体 422、并发只执行一次、资金路由 Redis 故障 503；Step-up 头能否被伪造；admin 白名单默认拒绝；webhook 未绑定验证器是否 fail-closed；授权矩阵测试是否真能在缺 x-roles 时失败；是否放宽了已有守卫；性能开销 |
| S-02 | critical 预留是否真的生效（压测式测试）；等待超时是否 ≤100 ms；降级状态机抖动；连接池参数是否真设置到连接上；账务局部提高 timeout 而非全局放宽；缓存是否拒绝资金命名空间；singleflight；关闭流程 |
| S-03 | "入队成功、MarkSent 前崩溃"的测试是否真注入了故障；TaskID 去重；多实例 Relay 恰好一次；队列隔离（独立 Server）；背压；未知 topic 不静默丢弃；noeviction 检查；指标标签低基数 |
| S-04 | 是否在 worker 里 sleep；令牌桶多实例共享；429 重排期；403 故障转移；载荷无正文/令牌；事务性通知优先 |
| S-05 | 阈值断言是否有效（放宽服务端能让脚本失败）；是否有环境守卫；3 倍过载结论；故障演练后账务数据无丢失 |
| S-06 | 令牌是否落持久存储；单飞刷新；重试只用于幂等请求；熔断按类别；错误信息不泄露 |
