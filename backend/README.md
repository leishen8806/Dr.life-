# backend

Go 模块化单体后端，采用 chi、pgx、sqlc 与 goose。
接口规范先写 OpenAPI，再生成代码；业务模块边界以独立 schema 为准。
技术依据：`Dr.life+第一版交付形态与技术架构.md` 第三、六章。

Prometheus、Grafana、Sentry 与指标暴露约束见 `../deploy/observability/README.md`。

## Telegram Mini App 登录

`POST /v1/public/auth/telegram` 校验服务端收到的 Telegram `initData`。生产实现从 `iam.bot` 按端读取主、备用 Bot，并使用 F-10 信封密钥以 `bot_token:<id>` AAD 解密令牌。`limited`、`banned` 状态仍参与旧入口校验；管理端更换令牌后必须调用 Bot 池缓存失效接口。

登录配置均从环境变量读取：

- `TELEGRAM_INITDATA_MAX_AGE` 默认 `24h`，未来时间容差固定为 60 秒；
- `ACCESS_TOKEN_TTL` 默认 `15m`，`REFRESH_TOKEN_TTL` 默认 `720h`；
- `LOGIN_FAILURE_LIMIT` 默认 20，`LOGIN_FAILURE_WINDOW` 默认 `10m`；
- `BOT_TOKEN_CACHE_TTL` 默认 `5m`；
- `JWT_SIGNING_KEYS` 格式为逗号分隔的 `kid:base64` 32 字节 HS256 密钥，`JWT_ACTIVE_KEY_ID` 指向当前签名密钥。保留旧 key 可验证轮换前签发的访问令牌；
- `MASTER_KEYS` 与 `ACTIVE_MASTER_KEY_ID` 按 `internal/platform/secrets` 的约定解密数据库中的 Bot 令牌。
- `IAM_DATABASE_URL` 使用仅继承 `iam_rw` 的 `app_iam` 登录角色；登录服务不复用 `app_ops` 的连接，保持 schema 权限边界。

JWT 选择 HS256，是为了复用现有 32 字节密钥提供器和 `kid` 轮换机制。解析器固定允许 HS256，拒绝 `none` 和算法替换。未配置 JWT 密钥时服务仍可用于健康检查，但 Telegram 登录返回 503，受保护路由返回 401；staging/prod 部署应将 JWT 与主密钥作为必填部署配置。

访问令牌默认 15 分钟。刷新令牌使用 32 字节随机数，响应一次后数据库只保存 SHA-256。当前认证器的 `SessionChecker` 默认通过；会话刷新、作废及强制退出由 A-03 接入。
