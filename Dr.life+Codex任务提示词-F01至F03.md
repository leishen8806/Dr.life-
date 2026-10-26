# Codex 任务提示词：F-01 至 F-03（项目骨架第一批）

**用法：** 每个任务一个"提示词"，整块复制（代码块内全部内容）粘贴到 Codex 的任务输入框。三个提示词都**自包含**，不依赖你再补充背景。
**顺序：** 先做 **F-01**，合并后再做 **F-02**；**F-03 依赖 F-02**（要用到配置、日志和错误结构）。F-02、F-03 不要同时让两个任务改同一个文件（见各提示词里的"文件归属"）。
**合并前你要做的：** 看 PR 描述里的验收逐条结果；在本机亲自跑一遍"如何验证"里的命令；按 `AGENTS.md` 和《开发任务拆分与验收》第一章的规则评审。
**仓库：** `leishen8806/Dr.life-`（私有）。让 Codex 在自己的分支上提交，分支名 `task/F-0x-短名`，**不要让它直接推 `main`**。

---

## F-01 仓库结构与规范

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。这是一个 Telegram Mini App 电商平台的项目，目前仓库里是设计文档和可点击原型，还没有生产代码。

【任务】F-01 仓库结构与规范
【分支】task/F-01-repo-structure，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（全文；第 4 节"不可违反的规则"和第 5 节"约定"必须遵守）
2. Dr.life+开发任务拆分与验收.md 第一章（任务模板、分支与评审规则）和第四章 F-01 一行
3. Dr.life+第一版交付形态与技术架构.md 第六章（技术选型）

【背景】
后续所有开发任务都会按 AGENTS.md 里建议的目录结构放代码。本任务只建立"空骨架 + 规范文件"，让后面的任务有统一的位置和规则。本任务不写任何业务代码。

【范围——要做】
1. 建立目录（每个目录放一个 README.md，用 3–6 行说明该目录用途、技术栈、对应哪份设计文档；不要放其他文件）：
   backend/            Go 后端（说明：模块化单体，chi + pgx + sqlc，goose 迁移，OpenAPI 先写规范再生成）
   backend/cmd/
   backend/internal/
   backend/migrations/
   backend/api/        （放 openapi.yaml 的位置）
   apps/buyer/  apps/merchant/  apps/driver/  apps/admin/  apps/wms/
   packages/ui/  packages/api-client/  packages/i18n/
   site/               官网（Astro 静态站，settlecore.org）
   poc/                技术验证代码（说明：PoC 代码不进生产目录，结论写进技术方案文档）
2. 根目录 README.md：项目一句话介绍；文档索引（列出所有 .md 文档，一行一个，写明用途）；目录结构说明；"从哪里开始读"（先读 AGENTS.md）；注明 historical/ 不入库。不要删除或修改任何已有设计文档的内容。
3. .editorconfig：UTF-8、LF 换行、缩进（Go 用 tab，其余 2 空格）、文件末尾保留换行、去掉行尾空格。
4. .gitattributes：`* text=auto eol=lf`，并把 *.png *.jpg *.pdf 等标为 binary。（目的：避免 Windows/Mac 换行冲突，当前提交时已出现 LF/CRLF 警告。）
5. 完善 .gitignore（保留已有的 historical/ 条目）：增加 Go（二进制、coverage）、Node（node_modules、dist、.turbo、.vite）、环境文件（.env、.env.*，但保留 .env.example）、编辑器目录（.idea、.vscode，但允许 .vscode/extensions.json）、系统文件。
6. .github/pull_request_template.md，必须包含这些小节：
   - 对应任务编号
   - 改动内容摘要
   - 对文档/表结构/接口的影响（勾选：未改 / 已同步更新《数据库表结构与账务详细设计》/ 已同步更新 openapi.yaml）
   - 验收标准逐条结果（用复选框列表，由作者逐条填写）
   - 安全检查（不含密钥/令牌/真实手机号；日志不含敏感信息）
   - 已知遗留与"待确认"事项
7. .github/CODEOWNERS：默认负责人写 @leishen8806；为 backend/internal/ledger/、backend/internal/iam/、backend/migrations/、backend/api/、.github/ 这几个路径单独列出，同样先指向 @leishen8806，并在文件顶部用注释写明"账务、权限、登录、迁移、接口规范相关改动必须由负责人评审；团队确认分工后在此替换为实际负责人"。（这些目录现在还不存在也要写规则。）
8. CONTRIBUTING.md：分支命名（task/<编号>-<短名>）、提交信息（一次提交一个主题，中英文均可）、PR 规则（不超过约 800 行有效改动；账务/权限/登录/密钥相关需 2 人评审）、迁移规则（只向前兼容）、合并门槛。内容直接来自《开发任务拆分与验收》第一章，不要自己发明规则。

【范围——不要做】
- 不要写任何 Go 或前端业务代码，不要初始化 go.mod 或 package.json（那是 F-02、F-06 的事）。
- 不要移动、重命名或改写现有的设计文档、prototype/、AGENTS.md。
- 不要创建 CI 工作流（F-08）。
- 不要提交 historical/ 目录或任何密钥。

【验收标准——逐条自测并在 PR 描述里写结果】
□ 上述目录都存在，且每个目录有 README.md，内容准确指向对应设计文档
□ 根 README.md 的文档索引与仓库里实际的 .md 文件一一对应，没有遗漏或失效名称
□ 新增 .gitattributes 后，在仓库根目录运行 `git add --renormalize .` 再 `git status`，不应出现意外的大量改动；如有 CRLF→LF 的转换，在 PR 描述里列出受影响文件
□ .gitignore 不会忽略 .env.example，会忽略 .env；historical/ 仍被忽略
□ PR 模板在 GitHub 新建 PR 时自动出现（本地检查文件路径为 .github/pull_request_template.md）
□ CODEOWNERS 语法正确（每行 路径 + 空格 + @用户名）
□ CONTRIBUTING.md 的规则与《开发任务拆分与验收》第一章一致
□ 没有改动任何设计文档的正文

【如何验证】
tree -L 3（或 ls -R）查看目录；git status；检查 README 里的每个文档链接文件名真实存在。

【遇到不确定】
不要猜测，把问题写进 PR 描述的"待确认"列表；已有规则以 AGENTS.md 为准。

【完成后】
在 PR 描述里按模板填写，并附一段 5 行以内的总结：新增了什么、有什么需要负责人决定的事。
```

---

## F-02 Go 服务骨架

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。这是一个 Telegram Mini App 电商平台，后端用 Go 的模块化单体。F-01（仓库结构与规范）已合并。

【任务】F-02 Go 服务骨架
【分支】task/F-02-go-skeleton，只提交到这个分支并发起 PR，不要推 main。

【先阅读（按顺序）】
1. AGENTS.md（第 3 节技术栈、第 4 节规则 8–14、第 5–6 节）
2. Dr.life+开发任务拆分与验收.md 第一章、第四章 F-02 一行
3. Dr.life+第一版交付形态与技术架构.md 第三章"总体架构"里的"接口约定"和第六章
4. Dr.life+技术方案与系统边界.md 第八章（接口约定、错误码分段、限流）和第十章（脱敏、日志）

【背景】
这是后端的最小可运行骨架：启动、配置、日志、统一错误与响应结构、健康检查、优雅停机。后面的所有模块都会在这个骨架上加。本任务不连接数据库（那是 F-03），不写业务逻辑，不引入重框架。

【文件归属】只在 backend/ 下改动：backend/go.mod、backend/cmd/server/、backend/internal/platform/（config、logx、httpx、server 子包）、backend/Makefile、backend/Dockerfile、backend/.env.example。不要碰 backend/migrations/（F-03）、backend/api/（F-04）。

【技术要求】
- Go 版本：使用当前稳定版（≥ 1.22），在 go.mod 里写明。模块路径：github.com/leishen8806/Dr.life-/backend
- 路由：github.com/go-chi/chi/v5；日志：标准库 log/slog（JSON 输出）；配置：只用标准库 os.Getenv 读取环境变量，封装成强类型 Config 结构并在启动时校验，缺必填项直接报错退出。尽量少依赖第三方库，每个第三方库都要在 PR 描述里说明理由。

【范围——要做】
1. backend/cmd/server/main.go：读取配置 → 构建日志 → 构建路由 → 启动 HTTP 服务；监听 SIGINT/SIGTERM 做**优雅停机**（停止接收新请求，等待在途请求最多 N 秒后退出，N 来自配置，默认 15）。
2. internal/platform/config：Config 结构（HTTP_ADDR 默认 :8080、APP_ENV 取值 dev|test|staging|prod、LOG_LEVEL、SHUTDOWN_TIMEOUT_SECONDS；预留 DATABASE_URL、REDIS_URL 字段但本任务可选）。提供 backend/.env.example（只放占位示例，不放真实值）。
3. internal/platform/logx：slog JSON 日志；每条请求日志带 request_id、method、path、status、duration_ms、role（暂为空）；**脱敏**：提供 Redact 函数/handler，字段名包含 token、secret、password、key、authorization、phone、initdata（不区分大小写）的值一律输出 "[REDACTED]"；不得记录请求体。
4. internal/platform/httpx：
   a) 统一响应结构：成功 {"ok":true,"data":...,"request_id":"..."}；失败 {"ok":false,"error":{"code":1001,"message":{"zh":"…","en":"…","km":"…"}},"request_id":"..."}。
   b) 错误码分段常量：1xxx 通用、2xxx 账号与权限、3xxx 商品订单、4xxx 仓储物流、5xxx 账务资金、6xxx AI、7xxx 外部服务；先定义通用的几个（内部错误、参数错误、未登录、无权限、资源不存在、请求过多、服务暂时不可用），每个都有 zh/en/km 三语提示（高棉语若无把握，写英文并在代码注释里标 TODO-KM-REVIEW，同时列入 PR 描述）。
   c) 一个 AppError 类型和统一的 WriteError/WriteOK；未预期的 panic 不泄露内部细节，返回 1001 并记录日志。
   d) 请求编号中间件：读取或生成 X-Request-ID（ULID 或 UUID），写入上下文和响应头。
   e) Recover 中间件、访问日志中间件。
5. internal/platform/server：组装路由与中间件；提供 GET /healthz（存活，始终 200 {"ok":true}）和 GET /readyz（就绪，调用一组"就绪检查函数"，任一失败返回 503；本任务注册空列表，F-03 会接入数据库检查）。
6. backend/Makefile：make run、make test、make vet、make build、make docker。
7. backend/Dockerfile：多阶段构建，最终镜像最小化，非 root 运行，只暴露端口，健康检查指向 /healthz。
8. 单元测试：配置校验（缺必填项报错）、脱敏（各类字段名被遮蔽、嵌套值）、统一响应与错误结构（JSON 形状、三语齐全）、请求编号中间件、panic 恢复不泄露细节、/healthz 与 /readyz（含就绪失败返回 503）、优雅停机（在途请求完成）。

【范围——不要做】
- 不连接数据库、Redis；不写业务接口；不做鉴权（F-05）；不做 OpenAPI（F-04）；不做限流（F-05）；不写 CI（F-08）。
- 不要引入 Gin/Echo/Fiber 等框架或 ORM。
- 日志和响应里绝不输出密钥、令牌、完整手机号、请求体。

【验收标准——逐条自测并在 PR 描述里写结果】
□ `cd backend && go vet ./... && go test ./...` 全部通过，且 `go test -race ./...` 通过
□ `go run ./cmd/server` 启动后：GET /healthz 返回 200；GET /readyz 返回 200（就绪检查为空时）
□ 发送 SIGTERM 后进程在超时内优雅退出，在途请求不被中断（有自动化测试）
□ 错误响应的 JSON 形状符合上述结构，error.message 含 zh/en/km 三个键
□ 响应头和 JSON 都带 request_id；调用方传入 X-Request-ID 时原样回显
□ 触发 panic 的测试路由返回 1001 且响应体不含栈或内部信息
□ 日志为 JSON；含 token/secret/password/key/authorization/phone/initdata 的字段值为 [REDACTED]（有测试）
□ 缺必填配置时进程以非零状态退出并给出清晰错误
□ docker build 成功（make docker），镜像以非 root 运行
□ PR 描述列出所有新增的第三方依赖及理由，以及所有 TODO-KM-REVIEW 项

【如何验证（你会亲自跑）】
cd backend && make vet test && make run
curl -i localhost:8080/healthz ; curl -i localhost:8080/readyz
kill -TERM <pid> 观察日志；make docker

【遇到不确定】
不要猜测，写进 PR 描述"待确认"；规则冲突以 AGENTS.md 为准。

【完成后】
在 PR 描述里按模板填写，并附 5 行以内总结。
```

---

## F-03 数据库与迁移框架

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。F-01（仓库结构）、F-02（Go 服务骨架）已合并，main 当前在 de884eb。

【任务】F-03 数据库与迁移框架
【分支】task/F-03-db-migrations，只提交到这个分支并发起 PR，不要推 main。
【工作目录】请基于 origin/main（de884eb 或更新）新建一个**独立 worktree** 在其中工作，不要在用户的 E:\Dr.life+ 主工作目录里直接改。

【开工前必须先做：环境预检（Docker 不可用时不要空转）】
本任务强依赖 Docker（docker-compose 开发数据库 + testcontainers 集成测试）。写任何代码之前，先在你的运行环境里依次执行并记录结果：
  1. docker version          → 必须同时看到 Client 和 Server 两段；只有 Client、报 "failed to connect to the docker API" 说明引擎没起来
  2. docker info --format "{{.ServerVersion}} | {{.OperatingSystem}} | mem={{.MemTotal}}"
  3. docker run --rm hello-world
内存要求：Docker 的 mem 值（MemTotal）应 ≥ 6 GiB（约 6442450944 字节）。低于这个值，PostgreSQL + 测试容器 + 迁移并发时容易被内核杀掉（OOM）。在 Windows 上，WSL 的内存上限来自 %USERPROFILE%\.wslconfig 的 memory=…，改完需要执行 wsl --shutdown 再重启 Docker Desktop；这些是用户的系统设置，**你不要自己改**，只提示用户。
如果预检失败（引擎不可用、hello-world 失败、内存不足）：
  - **立即停止，不要重试超过 2 次，不要反复等待或轮询，也不要用 SQLite、内存数据库、mock 代替真实 PostgreSQL 来"凑"测试。**
  - 向用户报告：失败的是哪一条命令、完整错误文本、docker info 的内容（如有）、你判断的原因和需要用户做的事（例如"启动 Docker Desktop 并等待 Engine running""把 .wslconfig 的 memory 调到 8GB 后执行 wsl --shutdown"）。
  - 此时只允许做**不需要 Docker 的部分**并明确标注：SQL 迁移文件、backend/internal/platform/db/ 的代码、Makefile、docker-compose.dev.yml、README；并在 PR 描述里把所有依赖 Docker 的验收项逐条标为"未验证（原因：…）"。**绝不能声称依赖 Docker 的验收项已通过。**
预检通过后，把 docker version / docker info 的关键行（Server Version、OS、MemTotal）贴进 PR 描述的"运行环境"小节，作为验证证据。

【先阅读（按顺序）】
1. AGENTS.md（第 4 节规则 8 和账务规则 1–7、第 5 节迁移约定）
2. Dr.life+数据库表结构与账务详细设计.md 的第一章（模块与 schema、通用约定）和第十章（迁移与种子数据）
3. Dr.life+开发任务拆分与验收.md 第四章 F-03 一行
4. Dr.life+技术方案与系统边界.md 第四章（模块边界）

【背景】
数据库用 PostgreSQL，**每个业务模块一个 schema，从数据库层强制"模块只访问自己的 schema"**。本任务只建立：迁移框架、10 个 schema、各模块数据库角色与权限、开发/测试用数据库环境、数据库连接与就绪检查、边界测试。**不创建任何业务表**（iam 表是 A-01，账务表是 L-01，等等）。

【文件归属】backend/migrations/、backend/internal/platform/db/、backend/Makefile（只追加 db-* 目标）、backend/.env.example（只追加数据库变量）、仓库根目录 docker-compose.dev.yml、backend/internal/platform/server/（只在 F-02 留的 readyz 注册点接入数据库检查，最小改动）。不要改 F-02 已有的其他逻辑。

【技术要求】
- PostgreSQL 16；迁移工具 goose（github.com/pressly/goose/v3），用 SQL 迁移文件，序号递增（00001_xxx.sql）；驱动 pgx/v5（pgxpool）；测试用 testcontainers-go 启动真实 PostgreSQL（不要用 SQLite 或内存模拟）。
- 不引入 ORM。

【范围——要做】
1. docker-compose.dev.yml：postgres:16（命名卷、健康检查、仅绑定 127.0.0.1）+ redis:7（仅绑定 127.0.0.1）。账号密码从 .env 读，仓库里只放 .env.example 占位，不要写真实或"看起来像真的"的密码。
2. 迁移 00001_schemas：创建 10 个 schema：iam、catalog、trade、wms、logistics、ledger、comms、ai、ops、audit；创建扩展 pg_trgm（放在 public 或专用 ext schema，说明选择理由）。
3. 迁移 00002_roles：创建**无登录**的权限角色（NOLOGIN）：iam_rw、catalog_rw、trade_rw、wms_rw、logistics_rw、ledger_rw、comms_rw、ai_rw、ops_rw、audit_writer、report_ro；然后规定权限：
   - 每个 *_rw 角色对**自己**的 schema 拥有 USAGE 和完整读写权限（含默认权限，对该 schema 将来新建的表自动生效），对**其他任何业务 schema 无任何权限**；
   - audit_writer：对 audit schema 只有 USAGE、INSERT、SELECT（**没有 UPDATE、DELETE**，默认权限同样如此）；
   - report_ro：对所有 schema 有 USAGE 和 SELECT（含默认权限），无任何写权限；
   - 所有角色对 public schema 不得有建表权限（REVOKE CREATE ON SCHEMA public FROM PUBLIC）。
4. 登录角色（带登录能力、密码来自环境变量）**不写在迁移文件里**（避免密码入库）：提供 backend/scripts/db-bootstrap（shell 或 Go 小程序均可），读取环境变量，创建每个模块的登录角色并加入对应权限角色（如 app_iam LOGIN 继承 iam_rw），在开发环境一条命令完成。
5. internal/platform/db：pgxpool 构造（从配置读 DATABASE_URL、连接数、超时），Ping 检查函数，供 /readyz 注册；暴露一个只读连接池构造函数（连接报表角色/从库，本任务只提供函数，不接业务）。
6. Makefile 追加：make db-up（compose 启动并等待健康）、make db-down、make db-migrate（goose up）、make db-rollback（goose down 一步）、make db-reset（销毁并重建后迁移，仅限 APP_ENV=dev，其他环境拒绝执行）、make db-bootstrap。
7. 迁移约束：每个迁移文件必须有 Up 和 Down；Down 必须真正可逆（本任务的迁移全部可回滚）；在 backend/migrations/README.md 写明迁移规则（只向前兼容：先加列→改代码→再删列；账务表变更需双人评审；序号冲突时后合并者重新编号）。
8. 测试（用 testcontainers 起真实 PostgreSQL，放在 backend/internal/platform/db/ 的测试里，或 backend/migrations/ 旁的集成测试包）：
   a) 从空库迁移到最新成功；再 goose down 到 0 全部回滚成功；再 up 一次成功（可重复）；
   b) 10 个 schema 都存在；
   c) **边界测试**：用 ledger_rw 登录身份尝试在 iam schema 建表、读写 → 全部被拒；iam_rw 尝试访问 ledger → 被拒；
   d) **审计表权限测试**：在 audit schema 里（测试内临时创建一张表）用 audit_writer 身份 INSERT、SELECT 成功，UPDATE、DELETE 失败；
   e) report_ro 能 SELECT 任一 schema 的表、不能 INSERT/UPDATE/DELETE/建表；
   f) 就绪检查：数据库可用时 /readyz 返回 200；停掉数据库后返回 503。

【范围——不要做】
- 不创建任何业务表（iam/catalog/trade/wms/logistics/ledger/comms/ai/ops/audit 里都不要有业务表；测试里的临时表测完要清理）。
- 不把任何密码、连接串真实值提交仓库。
- 不做 Redis 业务使用（只在 compose 里启动）。
- 不改 backend/api/、不写 CI（F-08）。

【验收标准——逐条自测并在 PR 描述里写结果】
□ `make db-up && make db-migrate` 在全新环境一条命令从空库迁移到最新
□ `make db-rollback` 逐步回滚到 0 全部成功，再 `make db-migrate` 成功
□ 10 个 schema 与 11 个权限角色全部存在，权限与上述规则一致（测试覆盖）
□ ledger_rw 无法访问 iam（反之亦然）；audit_writer 对 audit 只有 INSERT/SELECT；report_ro 只读（测试覆盖）
□ 迁移文件里没有任何密码或连接串；.env.example 只有占位
□ `make db-reset` 在 APP_ENV 不是 dev 时拒绝执行（测试或手动验证并写在 PR 里）
□ /readyz 在数据库不可用时返回 503，可用时 200
□ `go vet ./... && go test ./...` 通过（集成测试需要 Docker，在 PR 里说明运行条件）
□ PR 描述含"运行环境"小节：docker version / docker info 关键行（Server Version、OS、MemTotal ≥ 6 GiB）；若预检失败，则所有依赖 Docker 的项逐条标为"未验证（原因）"，不得声称通过
□ backend/migrations/README.md 写清迁移规则；PR 描述列出新增依赖及理由

【如何验证（你会亲自跑；按顺序，前一步失败就停下报告，不要硬往下做）】
0. 预检（见上文，必须先通过）：
   docker version
   docker info --format "{{.ServerVersion}} | {{.OperatingSystem}} | mem={{.MemTotal}}"
   docker run --rm hello-world
   （MemTotal 应 ≥ 6 GiB；Docker 引擎刚启动时可能需要 1–3 分钟，**最多等待 3 分钟**，仍不可用就停下报告）
1. cd backend && make db-up db-migrate
   在 compose 里确认两个容器 healthy：docker compose -f ../docker-compose.dev.yml ps
2. 用 psql（可经 docker exec 进入容器）检查：\dn（10 个 schema）、\du（11 个权限角色 + 各模块登录角色）
3. make db-rollback 回滚到 0，再 make db-migrate
4. go vet ./... && go test ./...（集成测试会用 testcontainers 起 PostgreSQL，可能需要拉取镜像，首次较慢；单个测试包超时请设为 5 分钟并说明）
5. 停掉数据库容器后：curl -i localhost:8080/readyz 应为 503；恢复后应为 200
6. 清理：make db-down，确认没有遗留测试容器（docker ps -a）
Docker 内存紧张时的现象（出现就如实报告，不要掩盖）：容器反复重启、`docker info` 偶发超时、测试里数据库连接被重置、日志里出现 "Out of memory" / "Killed"。

【遇到不确定】
不要猜测，写进 PR 描述"待确认"；权限规则以《数据库表结构与账务详细设计》第一章为准；与文档冲突时以文档为准并提出。

【完成后】
在 PR 描述里按模板填写，并附 5 行以内总结。
```

---

## 给你的评审提示（合并前对照）

| 任务 | 重点看什么 |
|---|---|
| F-01 | 有没有误改设计文档正文；`.gitattributes` 是否导致大量文件被改；CODEOWNERS 路径是否覆盖账务/权限/迁移/接口规范 |
| F-02 | 错误响应三语是否齐全（高棉语 TODO 是否列出）；日志脱敏是否真的有测试；有没有偷偷引入框架或 ORM；优雅停机测试是否稳定 |
| F-03 | **权限边界测试是否真的用不同登录身份做**（不是只查语句）；迁移里有没有密码；Down 是否真的可逆；有没有顺手建了业务表；PR 里有没有"运行环境"证据（docker info、MemTotal）；有没有用 SQLite/内存库/mock 代替真实 PostgreSQL；有没有擅自改 .wslconfig 等系统设置 |

## 后续任务的提示词怎么来
其余任务（F-04 起）按同一格式生成：**角色说明 → 任务与分支 → 先阅读的文档章节 → 背景 → 文件归属 → 范围（做/不做）→ 验收标准 → 如何验证 → 遇到不确定 → 完成后**。内容来自《开发任务拆分与验收》里该任务的交付物与验收标准。需要时告诉我下一批是哪些任务。
