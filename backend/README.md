# backend

Go 模块化单体后端，采用 chi、pgx、sqlc 与 goose。
接口规范先写 OpenAPI，再生成代码；业务模块边界以独立 schema 为准。
技术依据：`Dr.life+第一版交付形态与技术架构.md` 第三、六章。

Prometheus、Grafana、Sentry 与指标暴露约束见 `../deploy/observability/README.md`。

## 账务对账命令

服务和 `ledgerctl` 都从环境变量读取数据库配置。每日快照日界线由
`LEDGER_RECON_TIMEZONE` 控制，默认 `Asia/Phnom_Penh`。

```sh
go run ./cmd/ledgerctl recon --day 2026-10-14
go run ./cmd/ledgerctl recon --day 2026-10-14 --force --reason "verified repair"
go run ./cmd/ledgerctl snapshot --day 2026-10-14
go run ./cmd/ledgerctl freeze list
go run ./cmd/ledgerctl freeze release --id 1 --by 2002 --reason "finance verified"
```

命令退出码为：`0` 表示成功且对账通过，`1` 表示运行失败或发现异常，`2`
表示参数错误。输出只包含对账、异常计数和冻结元数据，不输出收款账户等敏感信息。

## Telegram Mini App 登录

`POST /v1/public/auth/telegram` 校验服务端收到的 Telegram `initData`。生产实现从 `iam.bot` 按端读取主、备用 Bot，并使用 F-10 信封密钥以 `bot_token:<id>` AAD 解密令牌。`limited`、`banned` 状态仍参与旧入口校验；管理端更换令牌后必须调用 Bot 池缓存失效接口。

登录配置均从环境变量读取：

- `TELEGRAM_INITDATA_MAX_AGE` 默认 `24h`，未来时间容差固定为 60 秒；
- `ACCESS_TOKEN_TTL` 默认 `15m`，`REFRESH_TOKEN_TTL` 默认 `168h`，`SESSION_IDLE_TTL` 默认 `24h`；
- `REFRESH_TOKEN_GRACE` 默认 `10s`，同一旧令牌在宽限期内并发刷新会重放同一对新令牌，超出宽限期再次使用会作废整个令牌族；
- `SESSION_MAX_PER_ACCOUNT` 默认 5；`TOKEN_ISSUER` 默认 `drlife`；
- `LOGIN_FAILURE_LIMIT` 默认 20，`LOGIN_FAILURE_WINDOW` 默认 `10m`；
- `BOT_TOKEN_CACHE_TTL` 默认 `5m`；
- `JWT_SIGNING_KEYS` 格式为逗号分隔的 `kid:base64` 32 字节 HS256 密钥，`JWT_ACTIVE_KEY_ID` 指向当前签名密钥。保留旧 key 可验证轮换前签发的访问令牌；
- `MASTER_KEYS` 与 `ACTIVE_MASTER_KEY_ID` 按 `internal/platform/secrets` 的约定解密数据库中的 Bot 令牌；
- `IAM_DATABASE_URL` 使用仅继承 `iam_rw` 的 `app_iam` 登录角色；登录服务不复用商品服务连接，保持 schema 权限边界。

JWT 选择 HS256，是为了复用现有 32 字节密钥提供器和 `kid` 轮换机制。访问令牌固定 `typ=at+jwt`、`iss=drlife`（可配置），并按 buyer、merchant、driver 端设置受众；解析器固定允许 HS256，拒绝 `none`、算法替换和端路径受众不一致。未配置 JWT 密钥时服务仍可用于健康检查，但 Telegram 登录返回 503，受保护路由返回 401；staging/prod 部署应将 JWT 与主密钥作为必填部署配置。

从 A-02 升级时，旧访问令牌兼容默认关闭。确需无中断部署，可临时把 `TOKEN_LEGACY_ACCEPT_UNTIL` 设为 RFC3339 时间点，窗口不得超过 15 分钟；确认旧实例退出后立即清空。JWT 签名 key 至少保留到用该 key 派生的刷新令牌全部过期。

访问令牌默认 15 分钟。刷新令牌由独立用途标识、会话号和轮换次数经 HMAC 派生，只在响应中返回，数据库只保存 SHA-256。`POST /v1/public/auth/refresh` 仅从 JSON 请求体读取刷新令牌；退出、会话列表和单会话作废分别位于 buyer、merchant、driver 端的认证路径。Redis 保存会话有效位、撤销标记和账号令牌 epoch；数据库仍是权威来源。Redis 故障时普通只读请求可降级放行，资金操作、后台与写请求一律拒绝。

`SessionService.CleanupExpiredSessions` 是可由调度任务调用的过期会话清理入口；S-03 队列接入前需由部署侧显式注册，当前不会自行启动后台清理循环。
