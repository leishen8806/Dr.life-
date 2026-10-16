# 可观测性本地环境

后端在独立内部监听地址暴露 Prometheus 指标。默认 `METRICS_ADDR=127.0.0.1:9100`；显式设为空值可关闭。主 HTTP 端口仍由网关 fail-closed，`/metrics` 返回统一 403。生产环境只允许 Prometheus 通过内网或 WireGuard 抓取，禁止经 Cloudflare Tunnel 暴露。

## 本地启动

1. 启动后端。若后端运行在宿主机且 Prometheus 运行在 Docker，请设置 `METRICS_ADDR=0.0.0.0:9100`，同时依赖本机防火墙保持该端口仅本机或可信开发网络可达。
2. 设置仅供本地使用的 Grafana 口令：`$env:GRAFANA_ADMIN_PASSWORD='replace-local-only'`。
3. 运行 `docker compose -f deploy/observability/compose.yaml up -d`。
4. 打开 Prometheus `http://127.0.0.1:19090/targets` 和 Grafana `http://127.0.0.1:13000`。仪表盘 `Dr.life+ / Dr.life+ Overview` 由 provisioning 自动载入。
5. 结束后运行 `docker compose -f deploy/observability/compose.yaml down -v`。

规则校验：

```sh
docker run --rm --entrypoint=promtool -v "$PWD/deploy/observability/prometheus:/etc/prometheus:ro" prom/prometheus:v3.7.3 check config /etc/prometheus/prometheus.yml
docker run --rm --entrypoint=promtool -v "$PWD/deploy/observability/prometheus:/etc/prometheus:ro" prom/prometheus:v3.7.3 check rules /etc/prometheus/alerts.yml
```

## 指标与标签

| 范围 | 指标 | 标签 |
|---|---|---|
| HTTP | `drlife_http_requests_total`, `drlife_http_request_duration_seconds`, `drlife_http_errors_total`, `drlife_http_requests_in_flight` | `role`, `route`, `method`, `status_class`；进行中请求只用 `role` |
| 网关 | `drlife_gateway_rate_limit_hits_total`, `drlife_gateway_auth_failures_total` | 固定规则类别或 401/403 |
| 数据库 | `drlife_db_pool_*`, `drlife_db_slow_queries_total` | 慢查询只用语句指纹 |
| 业务 | `drlife_orders_total`, `drlife_withdrawals_total`, `drlife_ai_requests_total`, `drlife_bot_sends_total`, `drlife_login_validation_failures_total`, `drlife_ledger_posts_total`, `drlife_ledger_reconciliation_mismatches_total` | 固定枚举；未知值归为 `other` |
| Go | `go_*`, `process_*` | Prometheus 默认采集器 |

`route` 始终取 chi 路由模式，例如 `/v1/buyer/orders/{id}`；未匹配请求为 `unmatched`。HTTP 方法只允许 `GET`、`POST`、`PUT`、`PATCH`、`DELETE`、`OPTIONS`、`HEAD`，其他方法统一为 `OTHER`；状态码也只归入固定的 `1xx` 至 `5xx` 或 `other`。慢查询指纹最多记录 256 个；超过上限的新指纹统一计入固定的 `overflow` 序列，以保留告警计数，同时不记录其模板日志或创建新指纹标签。禁止把原始路径、账号、手机号、订单号或任意错误文本放进标签。新增业务失败原因必须先扩充代码中的固定枚举。

## Sentry 与日志

`SENTRY_DSN` 为空时不会初始化 Sentry 客户端，也不会发生外联。启用时通过 `APP_ENV` 和 `APP_RELEASE` 标记环境与版本。发送前会清除请求体、Cookie、Authorization、Idempotency-Key，以及查询参数中的 `init_data`、token、phone、secret、password、key；用户只保留 `public_id`，IP、姓名和邮箱被移除。事件消息和异常文本中的手机号、数字加冒号格式令牌及 Bearer 令牌会被遮蔽。panic 响应、JSON 日志和 Sentry 事件使用同一个 `request_id`。

日志继续通过 `logx` 的字段名脱敏。每个被接纳的慢查询指纹第一次出现时会以 INFO 记录指纹和最长 500 字符的无参数 SQL 模板；后续日志只记录指纹、耗时和命令结果。任何慢查询日志都不记录参数值或连接字符串。

## 遗留

- 前端 Sentry 在各前端应用进入业务开发后接入。
- 集中日志尚未选择 Loki 或 ELK。
- 告警通知渠道（Telegram / 邮件 / PagerDuty）未接入，需审批；值班与升级流程见 `../../docs/ops/oncall.md`。
