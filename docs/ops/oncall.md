# Dr.life+ 值班与告警处置（Q-04）

- 告警规则：`deploy/observability/prometheus/alerts.yml`（20 条）
- 指标与标签口径：`deploy/observability/README.md`
- 适用期：试点上线前与试点期

## 一、当前监控能力与缺口

- 规则在 Prometheus 里求值；**通知渠道（Telegram / 邮件 / PagerDuty）尚未接入**（公司规则：接外部渠道需董事会审批）。因此告警靠值班人工巡检，响应时限见第三节。
- 巡检入口：
  - Prometheus 告警页 `http://127.0.0.1:19090/alerts`（生产仅内网或 WireGuard 可达）
  - `curl -s http://127.0.0.1:19090/api/v1/alerts`
  - Grafana 面板 `Dr.life+ / Dr.life+ Overview`
- 以下 3 条规则依赖**尚未部署**的采集端，指标不存在时不会触发——那是「未监控」，不是「正常」：
  - `QueueBacklogHigh` → asynq 指标（队列模块接入后）
  - `DiskSpaceLow` → node_exporter
  - `CertificateExpiringSoon` → blackbox_exporter

## 二、值班与职责（角色制，不锁定人名）

| 层级 | 角色 | 职责 | 响应时限（草案） |
|---|---|---|---|
| 一级 | OPS 值班（轮值，名单由董事会排班维护） | 确认告警、按本文处置、记录处置过程 | critical 15 分钟内确认；warning 当班处理 |
| 二级 | 后端高级工程师（BackendSenior） | 账务、登录、数据库、队列类升级；评审修复方案 | 一级升级后 30 分钟内 |
| 三级 | 董事会（苏晨、大旺） | 资金、对外沟通、停机、发布回滚的决策 | 二级升级后 30 分钟内 |

- 交接班：交接未关闭的告警与未完成的处置。
- 值班期间的写操作（后台）自动进审计日志；处置记录写在值班记录里（时间、告警名、现象、动作、结果、是否升级）。

## 三、告警分级

| severity | 级别 | 含义 | 处置 |
|---|---|---|---|
| critical | P1 | 资金、对账或可用性已受损 | 立即确认；必要时立刻升级，不等 `for` 窗口走完 |
| warning | P2 | 指标劣化，尚未影响资金 | 当班处理；重复出现（1 小时内 ≥3 次）按 P1 处理 |

Prometheus 只有 critical / warning 两级；分级到 P3（仅记录）暂由值班自行判断。

## 四、处置纪律（违反即事故）

- 余额只能由 `ledger` 模块的强类型业务方法改动：**禁止**手工 UPDATE 余额、禁止删改流水与分录、禁止绕过触发器；更正一律冲销。
- 不在生产库直接跑 DML 修数据；需要脚本时走代码评审。
- 日志、工单、截图中不写密钥、Bot 令牌、完整手机号、initData 原文、聊天原文。
- 凭据、通道、阈值的变更都要走审批：值班无权当场改。

## 五、逐条告警处置

### BackendDown

- 含义：Prometheus 抓不到 `drlife-backend`（进程退出、崩溃循环、`METRICS_ADDR` 变更、网络或防火墙变化）。
- 先查：后端进程状态；`curl -s http://127.0.0.1:9100/metrics | head`；Prometheus `/targets` 看 last error。
- 处置：重启进程；反复崩溃看 panic 日志与 Sentry，用同一个 `request_id` 关联请求。
- 升级：15 分钟未恢复 → 二级；疑似数据损坏 → 董事会。

### HTTP5xxErrorRateHigh

- 含义：5xx 占比超过 5%，持续 5 分钟。
- 先查：Grafana `HTTP error rate`（按 `status_class`）与 `HTTP requests / second`（按 `route` 定位接口）。
- 处置：判断是单接口还是全局；单接口先回滚最近发布（发布动作由 DevOps 执行）。
- 升级：订单、提现等资金路径出现 5xx → 立即二级 + 董事会。

### QueryP95LatencyHigh

- 含义：查询（`method="GET"`）P95 超过 500 ms，持续 10 分钟（Q-06 验收线）。
- 先查：Grafana 延迟面板；同期 `Slow queries` 与连接池面板。
- 处置：按慢查询指纹取日志里的无参数 SQL 模板，评估索引/统计信息；加索引走评审，不在值班当场执行。
- 升级：持续 1 小时超线 → 二级。

### WriteP95LatencyHigh

- 含义：写请求（`method!="GET"`）P95 超过 1 秒，持续 10 分钟（Q-06 验收线）。
- 先查：Grafana `Write latency P95`；连接池与慢查询。
- 处置：区分锁等待、连接池等待、单条慢 SQL；账务写入变慢时先看是否长事务。
- 升级：超过 30 分钟 → 二级。

### RequestP99LatencyHigh

- 含义：全量请求 P99 超过 2 秒，持续 10 分钟（**草案阈值**，待 Q-06 压测校准）。
- 先查：Grafana 延迟面板；是否存在少量超长请求（导入、导出、对账）。
- 处置：区分「尾部长」还是「整体变慢」；尾部长多为单接口问题，按 `route` 定位。
- 升级：与 P95 同时超线 → 按 P1 处理。

### DBConnectionPoolExhausted

- 含义：连接池没有空闲连接且已有被占用的连接（`idle == 0` 且 `acquired > 0`），持续 10 分钟。
- 先查：Grafana `Database pool`；同期长事务与慢查询。
- 处置：找长事务/慢查询（事务未提交会让连接不释放）；判断是否连接泄漏（`acquired` 只增不减）；调整连接数属配置变更，需评审后由 DevOps 执行。
- 升级：已影响写路径或账务 → 二级。

### DBConnectionPoolWaitHigh

- 含义：申请连接的等待时间占 5 分钟窗口的 25% 以上（`rate(drlife_db_pool_acquire_wait_seconds_total[5m]) > 0.25`），持续 10 分钟。
- 先查：Grafana `Database pool` 的 `wait seconds/s`；数据库主机 CPU/IO；连接池上限。
- 处置：先降负载（暂停非关键导出/对账批量任务），不要直接调大连接数掩盖问题。
- 升级：与 `DBConnectionPoolExhausted` 同时触发 → 二级。

### SlowQueriesHigh

- 含义：慢查询速率超过 0.2 次/秒，持续 5 分钟。
- 先查：Grafana `Slow queries` 按 `fingerprint`；日志中该指纹首次出现的无参数 SQL 模板。
- 处置：登记为性能待办（Q-06 压测复核）；只在指纹维度讨论，**不记录参数值**。

### QueueBacklogHigh

- 含义：队列 pending 任务超过 1000，持续 15 分钟（异步任务堆积：通知、对账、导出）。
- 先查：asynq 指标 / asynqmon（如已部署）；worker 进程与 Redis 连通性；同期业务告警（如 `BotSendSuccessRateLow`）。
- 处置：先确认 worker 是否在跑；再看失败重试原因；**不得清空队列**（会丢通知与对账任务）。
- 前置：队列模块接入前该指标不存在，规则不会触发。

### LedgerPostFailuresHigh

- 含义：入账失败速率超过 0.1 次/秒，持续 5 分钟（`reason`：`insufficient_funds` / `idempotency_conflict` / `unbalanced` / `database`）。
- 先查：Grafana `Ledger posts and failures` 按 `status`/`reason`。
- 处置：`insufficient_funds` 集中出现 → 查并发提现与冻结；`database` → 查连接池与磁盘；`idempotency_conflict` → 多为重复请求，确认幂等键来源。不改余额、不删流水。
- 升级：失败原因不明或涉及资金 → 二级。

### LedgerUnbalancedPost

- 含义：有分录未通过借贷平衡校验（数据库延迟触发器兜底拒绝）。**P1**。
- 先查：后端日志按 `request_id` 与 `idem_key` 定位业务方法；确认该笔是否落库（被触发器拒绝则未落库）。
- 处置：停用触发该路径的入口（后台按钮或定时任务），保留证据；修复走代码评审，**不得**绕过触发器。
- 升级：立即二级 + 董事会。

### LedgerReconciliationMismatch

- 含义：对账发现差异，`check_code` ∈ `BALANCE_CACHE` / `TXN_BALANCED` / `IDENTITY` / `HOLD_FROZEN` / `PENDING_PAYOUT` / `REVERSAL` / `ORPHAN` / `SNAPSHOT_DRIFT`。**P1**。
- 先查：按 `check_code` 定位首个差异账户或分录；对照恒等式（客户负债合计 + 平台自有 + 应付 = 银行专用账户）。
- 处置：冻结相关出账（提现进复核），不改流水；更正一律冲销，双人评审。
- 升级：立即二级 + 董事会。

### LedgerReconciliationRunFailed

- 含义：1 小时内出现对账任务失败（`drlife_ledger_reconciliation_runs_total{status="failed"}`）——差异未知，按 P1 对待。
- 先查：对账任务日志；数据库可达性与连接池；上一轮是否卡住未结束。
- 处置：手动重跑对账（幂等，重复执行安全）；仍失败则查依赖（数据库、磁盘、队列）。
- 升级：1 小时仍拿不到当日对账结果 → 二级 + 董事会。

### WithdrawalOverdue

- 含义：提现超过复核或放款时限（服务端统计 `event="overdue"`）。**P1**。
- 先查：后台提现列表按状态与时间；同账户 `event="reviewed"` / `event="paid"` 是否已发生。
- 处置：催复核人补审；确认银行侧是否已放款，**严禁重复放款**；记录超时原因。
- 升级：2 小时未闭环或疑似重复放款 → 二级 + 董事会。

### AuditChainVerificationFailed

- 含义：审计哈希链出现断链或内容被篡改。**P1**。
- 先查：运行 `go run ./cmd/auditctl verify`，记录返回的首个异常 id；保留数据库与备份证据。
- 处置：停止后台高风险写操作，不修改或删除审计行；从备份和异地锚点比对影响范围。
- 升级：立即二级 + 董事会 + 安全审查员。

### AuditAnchorMissing

- 含义：最近一次审计锚定超过 24 小时。
- 先查：锚定任务日志、锚点接收器可用性和 `audit.anchor` 最新时间。
- 处置：恢复接收器后补跑 `go run ./cmd/auditctl anchor`；不得覆盖已有锚点。
- 升级：超过 48 小时或同时有链校验失败 → 二级 + 董事会。

### BotSendSuccessRateLow

- 含义：Bot 发送成功率低于 95%（发送量大于 0），持续 10 分钟。
- 先查：Grafana Bot 面板按 `endpoint`、`bot`、`reason`（`network` / `telegram` / `invalid_request` / `rate_limited`）。
- 处置：区分单端/单个 Bot；`invalid_request` 多为目标用户未启动会话（属可预期业务情况，看量级）。
- 升级：多端同时失败 → 二级。

### BotRateLimited

- 含义：Telegram 限流（`reason="rate_limited"`）持续出现。
- 先查：是否单个令牌被限流；群发或广播任务是否在跑。
- 处置：把该端切到 Bot 池内备用令牌（配置变更走评审）；降低群发频率；**不要**把令牌写进记录或日志。
- 升级：池内令牌全部受限 → 二级。

### AIFallbackRateHigh

- 含义：AI 回退占比超过 50%（有请求量时），持续 15 分钟。
- 先查：`drlife_ai_requests_total{event="fallback",level}` 看回退到哪一级（primary → secondary → local）。
- 处置：判断是供应商故障、密钥失效还是月度预算超额自动停用（N-03）；本地规则兜底期间业务可用但要通知产品；确认发送前脱敏仍生效。
- 升级：所有供应商失败（回退到 local）→ 二级。

### LoginValidationFailuresHigh

- 含义：initData 校验失败速率超过 0.1 次/秒，持续 5 分钟（`reason`：`expired` / `invalid_hash` / `invalid_user` / `unknown_bot`）。
- 先查：按 `endpoint`（buyer/merchant/driver）与 `reason` 分布。
- 处置：`unknown_bot` → 检查 Bot 池配置与令牌轮换；`expired` 集中 → 客户端时间或 initData 有效期；`invalid_hash` 集中 → 疑似伪造或重放。**任何情况下不把 initData 原文写进工单或日志。**
- 升级：疑似重放/篡改 → 二级 + 安全审查员。

### DiskSpaceLow

- 含义：文件系统可用空间低于 10%，持续 15 分钟（PostgreSQL WAL、日志、备份目录）。**P1**。
- 先查：`df -h`；定位大目录（WAL、日志、备份、Docker 卷）。
- 处置：只清理可再生数据（过期日志、临时文件）；备份归档迁移到异地（Q-03）；**禁止**删数据目录或 WAL。
- 升级：无法立即释放 → 二级 + 董事会（可能需停机扩容）。

### CertificateExpiringSoon

- 含义：TLS 证书 14 天内到期（来自探测目标 `probe_ssl_earliest_cert_expiry`）。
- 先查：确认到期域名与签发方（看指标 `instance`）。
- 处置：Cloudflare 托管证书应自动续期 → 检查 DNS 与隧道配置；自有证书提前续期并复验 `/healthz`。
- 升级：7 天内仍未续期 → 二级。

## 六、升级路径

一级 OPS 值班 → 二级 后端高级工程师（BackendSenior） → 三级 董事会（苏晨、大旺）

- 立即升级的情形：P1 超过 15 分钟未确认；同一告警 1 小时内重复 ≥3 次；涉及资金、数据损坏、对外承诺、停机或回滚。
- 升级时必须带：告警名、起始时间、影响面、已做的动作、当前判断。
- 涉及认证 / 授权 / 上传 / 文件系统 / 依赖的问题，必须经安全审查员。

## 七、调整告警

1. 改 `deploy/observability/prometheus/alerts.yml`，每条必须含 `for`、`severity`、`summary`、`runbook_url`。
2. 校验规则：

```sh
docker run --rm --entrypoint=promtool -v "$PWD/deploy/observability/prometheus:/etc/prometheus:ro" prom/prometheus:v3.7.3 check rules /etc/prometheus/alerts.yml
```

3. **同时**在本文新增或更新同名小节（标题就是告警名，`runbook_url` 的锚点依赖它），否则链接悬空。
4. 新告警用到的指标必须先落在 `deploy/observability/README.md` 的指标表里，或在同期补上。
5. 接通知渠道（Telegram / 邮件 / PagerDuty）需要董事会审批，本任务未接入。

## 八、未确认项（需董事会决定）

- 通知渠道接入方式与责任人（当前人工巡检，响应时限能否成立取决于此）。
- 值班名单与轮值表（本文只定角色与层级）。
- 阈值校准：P99、磁盘、队列积压为草案值，待 Q-06 压测与试点实测后调整。
