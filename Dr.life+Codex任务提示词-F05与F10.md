# Codex 任务提示词：F-05（网关中间件）与 F-10（密钥与信封加密）

**用法：** 与前几份相同，整块复制代码块内容粘贴给 Codex。两个任务**互不依赖，可以同时开两个独立 worktree**。
**基线：** `origin/main` 最新提交（F-01～F-04、F-08、F-12、F-13 已合并）。
**避免冲突的分工：** F-05 负责改 `config.go`、`main.go`、`server.go`、`.env.example`；F-10 **不改这些文件**，只新增自己的包和文件（密钥相关环境变量写在 F-10 自己的 `backend/.env.secrets.example` 里）。
**分支保护提示：** 目前 GitHub Free 的私有仓库不强制执行分支保护，**合并前请自己确认 `ci-gate` 是绿的**。

---

## F-05 网关中间件

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。F-01～F-04、F-08、F-12、F-13 已合并。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】F-05 网关中间件
【分支】task/F-05-gateway-middleware，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（第 4 节规则 9、10、11；第 5–6 节）
2. Dr.life+开发任务拆分与验收.md 第一章、第四章 F-05 一行
3. Dr.life+技术方案与系统边界.md 第三章（端与权限边界：硬规则 1–5）、第八章（接口约定、错误码、限流与防刷）
4. Dr.life+第一版交付形态与技术架构.md 第七章（域名与 API 路径、CORS、Cookie 与会话隔离）
5. backend/api/openapi.yaml（tag 的 x-roles、ErrorEnvelope、Idempotency-Key 的格式）
6. backend/internal/platform/httpx/ 与 backend/internal/platform/server/server.go（现有中间件与统一响应）

【背景】
这是所有接口的"门口"：请求先经过中间件，才到业务处理器。本任务只做**通用网关能力**，不做登录（A-02/A-03 发令牌）、不做业务接口。

【文件归属】新增包 backend/internal/platform/gateway/；可修改：backend/internal/platform/config/（新增配置项）、backend/internal/platform/httpx/（新增错误码与 404/405 处理，见下）、backend/internal/platform/server/server.go（组装中间件）、backend/cmd/server/main.go（接线）、backend/.env.example（追加变量）、backend/api/openapi.yaml 及重新生成的文件（新增错误码后同步）。不要写 CI（F-08 已完成）、不要改迁移、不要碰 backend/internal/platform/secrets/（F-10 的目录）。

【技术要求】
- 只用标准库 + 已有依赖；限流的 Redis 实现允许新增 github.com/redis/go-redis/v9（在 PR 里说明理由）；测试里需要 Redis 时用 testcontainers（先做 Docker 预检，见【如何验证】）。
- 所有错误响应一律走 httpx 的统一信封，三语提示；不得泄露内部细节。

【范围——要做】
1. 身份抽象（本任务只做抽象与测试替身，不签发令牌）：
   - type Role string，取值 buyer、merchant、driver、wms、admin（网关层按"受众"粗分；细粒度权限由 A-05/A-06 的模块权限负责）；
   - type Identity struct{ AccountID string; Role Role }，通过 context 传递（提供 IdentityFromContext）；
   - type Authenticator interface{ Authenticate(r *http.Request) (Identity, bool, error) }，从 Authorization: Bearer … 取令牌并校验；本任务只提供接口和测试用假实现（生产实现由 A-03 提供）。
2. 路径-角色守卫（fail-closed）：
   - /v1/public/* 无需登录；/v1/buyer/* 只允许 buyer；/v1/merchant/* 只允许 merchant；/v1/driver/* 只允许 driver；/v1/wms/* 只允许 wms；/v1/admin/* 只允许 admin；
   - 未携带或无效令牌访问非 public 路径 → 401（错误码 2001）；令牌有效但角色与路径不匹配 → **403（错误码 2002）**（AGENTS.md 规则 10）；
   - **未知前缀（不在上述 6 个之内的 /v1/ 路径）默认拒绝**（fail-closed），不是放行；/healthz、/readyz 不受影响；
   - 路径规范化：防止 /v1/public/../admin/…、大小写、双斜杠、URL 编码绕过守卫（要有针对性测试）。
3. CORS 白名单：
   - 配置 CORS_ALLOWED_ORIGINS（逗号分隔，默认**空 = 不允许任何跨域来源**）；.env.example 里给出生产来源示例（https://miniapp.settlecore.org、https://shop.settlecore.org、https://driver.settlecore.org、https://admin.settlecore.org、https://wms.settlecore.org）和本地开发来源（如 http://localhost:5173）；
   - 请求带 Origin 且**不在白名单** → 403（2002）；不带 Origin 的请求（curl、服务器间调用、Telegram Webhook）不受影响；
   - 预检请求（OPTIONS）：白名单来源返回允许的方法与头（Authorization、Content-Type、Idempotency-Key、X-Request-ID）、Access-Control-Max-Age；暴露 X-Request-ID；不使用通配符 *，也不允许带凭据的通配。
4. 限流：
   - 接口 type Limiter interface{ Allow(ctx, key string, rule Rule) (allowed bool, retryAfter time.Duration, err error) }；
   - 两个实现：内存（单实例）与 Redis（多实例共享，滑动窗口或固定窗口二选一并说明理由）；有 REDIS_URL 用 Redis，没有则用内存；Redis 不可用时回退内存并记录告警日志（不要让限流故障导致整站不可用），并在 PR 里说明这个取舍；
   - 两层：① 认证前按**客户端 IP**限流（防刷）；② 认证后按**账号**限流；规则按路径前缀与"敏感路径"可配置（默认值写进配置并给出理由；预留登录、提现、AI 的更严规则位置，本任务不创建这些路由）；
   - 超限返回 **429（错误码 1004）**，带 Retry-After 头；
   - 客户端 IP：默认取 RemoteAddr；配置 TRUSTED_PROXY_HEADER（如生产使用 CF-Connecting-IP）时才信任该头，且**只在请求直接来自可信代理地址时**才信任（配置 TRUSTED_PROXY_CIDRS）；伪造该头的测试必须失败。
5. 幂等头解析：
   - 对写方法（POST、PUT、PATCH、DELETE）要求 Idempotency-Key：1–64 位字母、数字、"-"、"_"（与 OpenAPI 一致）；缺失或格式错误 → 400（1002）；
   - 提供 IdempotencyKeyFromContext；允许在路由注册时声明豁免（例如将来的登录接口）；
   - 本任务**只解析与校验**，不做去重存储（去重属于业务/账务层）。
6. 统一的 404 与 405：
   - chi 的 NotFound / MethodNotAllowed 改为统一信封：404 用 1003；405 新增错误码 **1006（方法不允许）**，三语文案；
   - 新增错误码必须同步：httpx 常量、backend/api/openapi.yaml 的 ErrorCode 枚举与响应说明、重新生成代码；F-04 的一致性测试必须仍然通过。
7. 访问日志补全：RequestLogger 里的 role 字段填入 Identity.Role（无身份则为空）；日志不得出现令牌、完整 IP 以外的敏感信息（IP 允许记录）。
8. 中间件顺序（写进 backend/internal/platform/gateway/README.md 并在 server.go 里体现）：RequestID → 访问日志 → Recover → CORS → IP 限流 → 认证 → 路径-角色守卫 → 账号限流 → 幂等头校验 → 路由。
9. **补 F-04 评审发现的测试缺口：** 对 httpx 的每一个错误码，用 WriteError 实际生成的响应体，通过 kin-openapi 对 openapi.yaml 里的 ErrorEnvelope 做校验；以及 404、405 的真实响应也要通过校验。
10. 配置：在 config 里新增项并写入 .env.example（带注释和安全默认值）：CORS_ALLOWED_ORIGINS、TRUSTED_PROXY_HEADER、TRUSTED_PROXY_CIDRS、限流相关阈值；非法值启动即失败。

【范围——不要做】
- 不做令牌签发/刷新/登录（A-02、A-03）；不做模块级权限矩阵（A-05、A-06）；不创建业务路由；不做去重存储；不做 HTTPS/TLS 终止（由 Cloudflare Tunnel 负责）。
- 不要把 CORS 做成"允许全部"；不要因为测试麻烦而默认放行；不要在日志里输出令牌。

【验收标准——逐条自测并在 PR 描述里写结果；每条都要有对应的自动化测试】
□ 买家令牌访问 /v1/admin/*、/v1/merchant/* 等 → 403（2002）；管理员令牌访问 /v1/buyer/* → 403；各角色访问自己的前缀 → 放行到处理器
□ 无令牌访问非 public 路径 → 401（2001）；/v1/public/* 无令牌可访问
□ 未知前缀（如 /v1/foo/x）→ 拒绝；路径绕过尝试（../、//、大小写、%2e%2e、%2f）全部被拒或被规范化后正确判定
□ 非白名单 Origin → 403；白名单 Origin 的预检请求返回正确的 CORS 头；无 Origin 的请求不受影响；默认配置（空白名单）下任何带 Origin 的请求都被拒
□ 超限 → 429 + Retry-After；内存与 Redis 两种实现都有测试（Redis 用真实容器）；Redis 不可用时回退内存且不致整站失败；伪造 CF-Connecting-IP（来自不可信来源）不能绕过 IP 限流
□ 写方法缺少或格式错误的 Idempotency-Key → 400（1002）；合法值进入 context；豁免声明生效
□ 404、405 返回统一信封（1003、1006）；新增错误码已同步到 openapi.yaml 并通过 F-04 的一致性测试；每个错误码的真实响应通过 ErrorEnvelope 校验
□ 访问日志含 role；日志中无令牌
□ go vet、go test -race（Linux 容器内或 CI）通过；PR 上的 CI（ci-gate）通过并附 Actions 运行链接与耗时
□ PR 描述列出新增依赖及理由、"限流在 Redis 故障时的取舍"、所有"待确认"

【如何验证（你会亲自跑；前一步失败就停下报告，不要空转）】
0. 预检：docker version（要同时看到 Client 和 Server）；docker info --format "{{.ServerVersion}} | mem={{.MemTotal}}"（MemTotal 应 ≥ 6 GiB）；docker run --rm hello-world。预检失败：最多重试 2 次、等待不超过 3 分钟，然后停止并向用户报告，不要用内存 Redis 模拟或跳过测试来"凑"。此时只允许做不需要 Docker 的部分，并把依赖 Docker 的验收项逐条标为"未验证（原因）"。
1. cd backend && go vet ./... && go test ./...
2. -race：Windows 通常没有 CGO，请用 Linux 容器：docker run --rm -v <工作目录>:/src -w /src/backend golang:<go.mod 里的版本> go test -race -count=1 ./...（跳过需要容器套容器的测试用 -skip，名称在测试文件里）
3. make generate-all 之后 git diff --exit-code（确认生成物与规范一致）
4. 启动服务，用 curl 手工验证：403/401/429/CORS/404/405 各一例

【遇到不确定】
不要猜测，写进 PR 描述"待确认"；规则冲突以 AGENTS.md 为准。不要靠放宽检查或跳过测试让 CI 变绿。

【完成后】
在 PR 描述里按模板填写，并附 5 行以内总结。
```

---

## F-10 密钥与信封加密

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。F-01～F-04、F-08、F-12、F-13 已合并。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【任务】F-10 密钥与信封加密
【分支】task/F-10-envelope-encryption，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（第 4 节规则 11；第 5 节"不要提交真实密钥"）
2. Dr.life+开发任务拆分与验收.md 第一章、第四章 F-10 一行
3. Dr.life+第一版交付形态与技术架构.md 第六章"密钥管理"一行与"部署位置与网络"（托管机柜自建、没有云密钥服务）、第七章"密钥与令牌保管"
4. Dr.life+技术方案与系统边界.md 第十章（数据分级、密钥）
5. Dr.life+数据库表结构与账务详细设计.md 第二章里 bot.token_enc、integration 的 key_enc（密文字段会放在这些列里）

【背景】
平台要保管 Telegram Bot 令牌、DeepSeek/ChatGPT 的 API 密钥、银行接口密钥等。这些**必须加密后存数据库**，页面只能看到"已设置 + 末 4 位"，主密钥不落盘。本任务只做**加密库 + 主密钥管理 + 轮换 + 工具和文档**；数据库表和接口由 A-07、N-10 使用它。

【文件归属】只新增：backend/internal/platform/secrets/（库与测试）、backend/cmd/keytool/（离线工具）、backend/.env.secrets.example、.sops.yaml、backend/secrets/README.md（+ 占位示例文件）。**不要修改** config.go、main.go、server.go、.env.example、CI（避免和 F-05 冲突；启动接线留给使用它的任务）。不要创建数据库表。

【技术要求】
- 只用 Go 标准库（crypto/aes、crypto/cipher、crypto/rand、crypto/subtle、encoding/base64 等）；**不要自己发明加密算法**；不引入第三方加密库。
- 算法：AES-256-GCM；随机数一律来自 crypto/rand。

【范围——要做】
1. 信封加密：
   - 每条密文使用**独立的随机数据密钥（DEK）**加密明文；DEK 再由**主密钥（KEK）**加密（"包装"）；
   - 密文打包为单个 []byte（可直接存入 bytea 列），含：格式版本号、KEK 编号（key id）、包装后的 DEK 及其随机数、数据的随机数与密文；格式要有版本号以便将来升级；
   - **AAD 绑定（关键）**：加密与解密都必须传入"用途 + 记录标识"（如 "bot_token:123"）作为附加认证数据，使密文不能被搬到另一条记录上使用；
   - API 示例：Encrypt(ctx, plaintext []byte, aad string) ([]byte, error)；Decrypt(ctx, blob []byte, aad string) ([]byte, error)。
2. 主密钥管理（KeyProvider）：
   - 接口：获取当前"活动"主密钥及其 id；按 id 获取历史主密钥（用于解旧密文）；
   - 实现一：从环境变量加载（MASTER_KEYS="id1:<base64>,id2:<base64>"，ACTIVE_MASTER_KEY_ID="id2"），启动时读入内存，**不写入磁盘、不写日志、不出现在错误信息里**；密钥必须 32 字节，格式错误启动即失败；
   - 实现二：从"两位管理员分管的分片"组合出主密钥：2-of-2 异或拆分（任一分片单独不泄露任何信息）；环境变量 MASTER_KEY_SHARE_A / MASTER_KEY_SHARE_B（均为 base64，等长）；
   - 内存卫生：使用后尽力清零临时切片；在 README 里诚实说明 Go 的局限（GC 可能留有副本，不能保证完全清除）。
3. 轮换：
   - Rewrap：用新的活动主密钥重新包装 DEK（**不重新加密数据**，速度快），旧密钥可随后下线；
   - RotateDEK（可选）：解密后用新 DEK 重新加密；
   - 要有测试证明：轮换后旧密文仍可解（旧密钥仍在时）；Rewrap 后只用新密钥即可解；旧密钥移除后未 Rewrap 的数据解不开（并给出清晰错误）。
4. 防泄露的值类型：
   - type Secret：String()、GoString()、MarshalJSON、MarshalText、LogValue（slog）一律输出 "[REDACTED]"；只有显式的 Reveal() 返回明文；
   - DisplayHint(plaintext)：返回"末 4 位"的展示值（如 "••••a1b2"）；长度不足 8 位时只显示点，不显示任何字符；页面/接口只能使用它。
5. 离线工具 backend/cmd/keytool（给管理员在离线机器上使用）：
   - generate：生成 32 字节随机主密钥并输出 base64（提示"只显示一次，请离线保管"）；
   - split：把主密钥拆成两个分片；combine：用两个分片还原（用于验证，不鼓励常用）；
   - 工具不写文件，输出到标准输出；不在命令行参数里接收密钥（防止进入 shell 历史），改用标准输入或交互提示。
6. SOPS（age）配置加密（用于部署配置文件，非应用内加密）：
   - .sops.yaml：creation_rules 示例，**age 公钥使用明显的占位符**（不放任何真实公钥）；
   - backend/secrets/README.md：如何生成 age 密钥、如何加密/解密配置文件、哪些内容必须加密、哪些文件不得提交明文、密钥丢失后果、与应用内信封加密的区别和分工；backend/secrets/ 下只放 *.example.yaml 占位示例（全部假值）；
   - 一个脚本 backend/scripts/check-sops-encrypted.sh：检查 backend/secrets/ 下除 *.example.yaml 外的 *.yaml 都含 SOPS 元数据（"sops:" 段），否则失败。**只创建脚本，不接入 CI**（接入由评审后另行决定），并在 PR 里说明如何接入。
7. 文档 backend/internal/platform/secrets/README.md：威胁模型（防什么、不防什么）、数据格式说明、AAD 约定（用途命名规则，如 bot_token:<id>、integration_key:<id>）、轮换操作步骤（先加新密钥并设为活动 → 逐条 Rewrap → 验证 → 下线旧密钥）、主密钥丢失或泄露的应急流程、**不得做的事**（日志里打印密钥、把主密钥放进仓库或 Docker 镜像、在 URL 或命令行里传密钥）。

【范围——不要做】
- 不创建数据库表、不连接数据库；不写 Bot 池或集成配置的业务代码（A-07、N-10）；不接入 KMS/HSM；不修改 main.go/config.go；不做 CI 变更。
- 不提交任何真实密钥、令牌或看起来像真的假密钥；测试用的密钥在测试里临时随机生成，或使用明显的占位值。

【验收标准——逐条自测并在 PR 描述里写结果；每条都要有自动化测试】
□ 加解密往返正确；每次加密的 DEK、随机数都不同（同一明文两次加密密文不同）
□ **篡改检测**：改动密文任一字节、改动 AAD、把密文放到另一个 AAD 下解密 → 全部失败
□ 错误的主密钥 → 失败；错误信息中不含任何密钥或明文内容
□ 轮换：新增主密钥并设为活动后旧数据仍可解；Rewrap 后只用新密钥可解；移除旧密钥后未 Rewrap 的数据给出清晰错误
□ 两分片组合与直接密钥得到相同主密钥；单个分片无法还原（至少用测试说明分片是随机的、与主密钥无关联）；分片长度不一致报错
□ Secret 类型：fmt、JSON、slog 三种输出都不含明文（与 backend/internal/platform/logx 的脱敏配合再测一次）；DisplayHint 对短密钥不泄露任何字符
□ 主密钥来自环境变量时，进程不会把它写入任何文件、日志（代码审查点 + 测试断言日志输出不含密钥）
□ keytool：generate/split/combine 工作正常；密钥不通过命令行参数传入
□ check-sops-encrypted.sh：对加密示例通过，对明文文件失败（附演示）
□ go vet、go test -race 通过（-race 在 Windows 上通常不可用，请在 Linux 容器内运行，或看 CI）；PR 上的 CI（ci-gate）通过并附 Actions 运行链接与耗时
□ README 完整；PR 描述含"威胁模型的局限"与"待确认"

【如何验证（你会亲自跑；前一步失败就停下报告）】
0. 预检：go version；本任务不需要 Docker；如要在 Linux 容器里跑 -race，再做 docker version / docker run --rm hello-world 预检，失败则停止并报告，不要空转。
1. cd backend && go vet ./... && go test ./internal/platform/secrets/... ./cmd/keytool/...
2. go test -race：Linux 容器：docker run --rm -v <工作目录>:/src -w /src/backend golang:<go.mod 里的版本> go test -race -count=1 ./internal/platform/secrets/...
3. 手工演示：用 keytool generate 生成密钥 → 加密一个测试字符串 → 轮换 → 解密

【遇到不确定】
不要猜测，写进 PR 描述"待确认"。涉及密码学的取舍（如 AAD 格式、分片方案）请在 PR 里写明理由，评审者会重点审查。

【完成后】
在 PR 描述里按模板填写，并附 5 行以内总结。
```

---

## 合并前的评审提示

| 任务 | 重点看什么 |
|---|---|
| F-05 | **默认是不是"拒绝"**（未知前缀、空 CORS 白名单）；路径规范化的绕过测试是否真的覆盖了 `../`、`%2e%2e`、`//`、大小写；`CF-Connecting-IP` 是否只在可信代理来源时才信任；限流在 Redis 故障时的行为；401 与 403 是否分得对；新增错误码 1006 是否同步了 `openapi.yaml`；Redis 测试是真容器还是模拟 |
| F-10 | **AAD 是否强制**（不能有"不传 AAD 也能解"的路径）；随机数是否都来自 `crypto/rand`；错误信息和日志有没有可能带出密钥；`Secret` 类型的各种输出；轮换测试是否真的验证了"旧密钥下线后"的行为；有没有把任何看起来像真的密钥写进仓库；`keytool` 是否没有通过命令行参数收密钥；有没有擅自改 `config.go`/`main.go` |
| 两个都要看 | CI 的 `ci-gate` 绿不绿（分支保护目前不强制，**合并前请自己确认**）；PR 里"未验证"项有没有如实写出；有没有修改授权范围之外的文件 |
