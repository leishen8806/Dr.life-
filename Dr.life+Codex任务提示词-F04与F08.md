# Codex 任务提示词：F-04（OpenAPI 骨架）与 F-08（CI）

**用法：** 与上一份相同，整块复制代码块内容粘贴给 Codex。两个任务**互不依赖，可以同时开两个独立 worktree**，但有一个交叉点（见下）。
**基线：** `origin/main@9a1ffe5`（F-01、F-02、F-03 已合并）。
**交叉点：** F-08 的 CI 里有一项"OpenAPI 生成无差异"，需要 F-04 先提供生成命令。所以 F-08 里对它做成**条件步骤**（文件存在才执行），F-04 合并后自动生效，不需要回头改 F-08。
**CI 策略（已定）：** 每个 PR 都跑集成测试；`main` 合并后再跑一次；**纯文档变更跳过重型任务但仍跑轻量检查**。F-08 上线后观察前 10 个 PR：中位耗时 ≤ 8 分钟、基础设施误报 < 5%，否则再加路径过滤和缓存。

---

## F-04 OpenAPI 骨架与代码生成

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。F-01、F-02、F-03 已合并，main 当前在 9a1ffe5。

【任务】F-04 OpenAPI 骨架与代码生成
【分支】task/F-04-openapi-skeleton，只提交到这个分支并发起 PR，不要推 main。
【工作目录】请基于 origin/main 新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【先阅读（按顺序）】
1. AGENTS.md（第 3 节技术栈、第 4 节规则 9–10、第 5–6 节）
2. Dr.life+开发任务拆分与验收.md 第一章、第四章 F-04 一行
3. Dr.life+技术方案与系统边界.md 第三章（端与权限边界、API 路径）和第八章（接口约定、错误码分段、幂等、限流）
4. Dr.life+第一版交付形态与技术架构.md 第三章"接口约定"、第七章（域名与 API 路径）
5. backend/internal/platform/httpx/response.go（已有的统一响应与错误码，规范必须与它一致）

【背景】
接口"先写规范再生成代码"：OpenAPI 是前后端共同的唯一来源。本任务只建立**规范骨架 + 两端代码生成流水线**，不实现业务接口。后续所有接口任务都在这份规范上增加路径。

【文件归属】backend/api/（openapi.yaml 与配置）、backend/internal/api/（生成的 Go 代码与薄封装）、backend/internal/platform/server/（只在路由组装处挂载生成的处理器，最小改动）、backend/Makefile（只追加 generate 相关目标）、packages/api-client/（只放生成的 TypeScript 类型与最小 package.json；monorepo 工作区由 F-06 负责，本任务不要创建 pnpm workspace）。不要改 F-02/F-03 其他已有逻辑，不要写 CI（F-08）。

【技术要求】
- 规范版本：OpenAPI 3.0.x 或 3.1.x（二选一并说明理由；需兼容 oapi-codegen 与 openapi-typescript）。
- Go 服务端接口生成：github.com/oapi-codegen/oapi-codegen/v2（chi 服务端模式，使用严格接口 strict-server 模式更好，若选用请说明）；固定工具版本。
- 前端类型生成：openapi-typescript，固定版本。
- 生成的代码**提交进仓库**（CI 会校验"重新生成后无差异"）；生成命令必须可重复执行且结果稳定（无时间戳、顺序确定）。

【范围——要做】
1. backend/api/openapi.yaml：
   a) info/servers：服务器地址占位（https://adminapi.settlecore.org，本地 http://localhost:8080），版本 1.0.0-draft。
   b) tags 按受众分组：public、buyer、merchant、driver、wms、admin；路径前缀分别为 /v1/public、/v1/buyer、/v1/merchant、/v1/driver、/v1/wms、/v1/admin。每个分组在 description 里写明谁能访问；并用扩展字段 x-roles 标注允许的角色（public 为 none）。
   c) 通用组件：
      - 成功信封 {ok:true, data, request_id} 与失败信封 {ok:false, error:{code, message:{zh,en,km}}, request_id}，与 httpx/response.go 的 JSON 形状完全一致；
      - 错误码：用 enum/说明列出 httpx 里已有的全部码（1001–1005、2001、2002），并写明分段规则 1xxx 通用、2xxx 账号权限、3xxx 商品订单、4xxx 仓储物流、5xxx 账务资金、6xxx AI、7xxx 外部服务；
      - 通用参数：分页（page、page_size，上限 100，默认值）、排序、筛选约定；请求头 Idempotency-Key（写接口必填的约定，格式要求写明）、X-Request-ID；
      - 通用响应：400、401、403、404、429、500、503 的引用；
      - 安全方案：bearerAuth（JWT，说明里写明令牌含角色、角色与路径不匹配返回 403）。
   d) 仅一个示例端点用来证明流水线：GET /v1/public/ping，返回 {ok:true,data:{pong:true},request_id}，无需鉴权。不要添加其他业务路径。
2. backend/api/oapi-codegen.yaml（或等价配置）与 `make generate`（生成 Go 服务端接口与类型到 backend/internal/api/gen/ 之类的固定位置）；`make generate-ts`（生成 packages/api-client/src/schema.d.ts）；`make generate-all`。若本机没有 Node，Makefile 要提供用 Docker 运行 Node 的方式，并在 README 说明。
3. 实现 /v1/public/ping 的处理器并挂载到路由（使用统一响应 httpx.WriteOK）；路径、方法、响应形状必须与规范一致。
4. 测试：
   a) 规范本身合法：用 kin-openapi（或 oapi-codegen 的依赖）加载并校验 openapi.yaml，失败则测试失败；
   b) **一致性测试**：规范里文档化的错误码集合 == httpx 里的错误码常量集合（新增常量忘记写进规范会失败）；
   c) /v1/public/ping 的响应符合规范（用规范校验响应体）；
   d) 生成幂等：连续运行两次 make generate-all，git diff 为空（可写成脚本，供 CI 使用：backend/scripts/check-generated.sh，退出码非 0 表示有差异）。
5. 文档：backend/api/README.md —— 规范的组织方式、如何新增一个接口（改 yaml → make generate-all → 实现 → 测试）、命名约定（operationId 命名规则、snake_case 字段）、版本与兼容规则（新增字段向后兼容，破坏性变更走 /v2）、与 AGENTS.md 规则 9、10 的关系；packages/api-client/README.md 说明用法。

【范围——不要做】
- 不实现鉴权中间件、角色-路径匹配、限流（F-05）；不写任何业务接口；不创建前端工作区（F-06）；不写 CI（F-08）。
- 不手改生成的代码；生成的文件头要有"自动生成，勿手改"的标记。
- 不引入与本任务无关的依赖。

【验收标准——逐条自测并在 PR 描述里写结果】
□ `make generate-all` 可重复执行，连续两次 `git diff --exit-code` 无差异
□ openapi.yaml 通过校验（测试覆盖）；含 6 个 tag 分组与 x-roles；通用信封、错误码、分页、Idempotency-Key、安全方案齐全
□ 规范里的错误码与 httpx 常量一致（一致性测试覆盖，故意漏掉一个能让测试失败——请在 PR 里描述你如何验证了这一点）
□ GET /v1/public/ping 返回符合规范的响应，带 request_id
□ 生成的 Go 代码与 TS 类型都已提交，文件头有自动生成标记
□ `cd backend && go vet ./... && go test ./...` 通过；新增依赖及固定版本列在 PR 描述里并说明理由
□ README 写清"如何新增一个接口"

【如何验证（你会亲自跑；前一步失败就停下报告）】
0. 预检：go version；node --version（没有 Node 就用 Docker：docker run --rm -v <目录>:/w -w /w node:22 …，并先确认 docker info 可用）
1. cd backend && make generate-all && git diff --exit-code && make generate-all && git diff --exit-code
2. make vet test
3. 启动服务后 curl -i localhost:8080/v1/public/ping
4. 把规范里的一个错误码删掉，确认一致性测试失败，再恢复

【遇到不确定】
不要猜测，写进 PR 描述"待确认"；规范与 httpx 不一致时以 httpx 为准并提出，或同时修改两边并说明。

【完成后】
在 PR 描述里按模板填写，并附 5 行以内总结。
```

---

## F-08 CI（GitHub Actions）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。F-01、F-02、F-03 已合并，main 当前在 9a1ffe5。

【任务】F-08 CI（GitHub Actions）
【分支】task/F-08-ci，只提交到这个分支并发起 PR，不要推 main。
【工作目录】请基于 origin/main 新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改。

【先阅读（按顺序）】
1. AGENTS.md（第 5–6 节：约定与完成标准）
2. Dr.life+开发任务拆分与验收.md 第一章（合并门槛）和第四章 F-08 一行
3. CONTRIBUTING.md、.github/pull_request_template.md
4. backend/Makefile、backend/Dockerfile、docker-compose.dev.yml、backend/migrations/README.md
5. Dr.life+Codex任务提示词-F04与F08.md 里的"CI 策略"

【背景】
目前这些验证只能在个人电脑上手工做（Docker 构建、-race、PostgreSQL 集成测试、迁移与权限边界）。CI 要把它们变成每个 PR 的自动门槛。**已定的策略：每个 PR 都跑完整检查，main 合并后再跑一次；纯文档变更跳过 Docker 与 PostgreSQL 等重型任务，但仍跑轻量检查。**

【文件归属】.github/workflows/、.github/dependabot.yml、.github/workflows/README.md、backend/scripts/（只新增 CI 用的检查脚本）。不要改业务代码、迁移、Dockerfile 逻辑；如发现它们有问题，写进 PR 描述"发现的问题"，不要顺手修。

【范围——要做】
工作流 .github/workflows/ci.yml，触发：pull_request（目标 main）和 push 到 main。
全局要求：
- permissions 最小化（默认 contents: read）；每个 job 设 timeout-minutes（轻量 10，重型 20）；
- concurrency：同一 PR 的新提交取消旧的运行；main 的运行不取消；
- 只使用 GitHub 官方 actions（actions/checkout、actions/setup-go、actions/setup-node、docker/setup-buildx-action、docker/build-push-action 等），固定到版本标签，并在 README 里列出；不使用第三方的路径过滤 action，改用 git diff 脚本判断（见下）；
- Go 版本从 backend/go.mod 读取（setup-go 的 go-version-file），启用模块缓存。

jobs：
1. `changes`：用脚本比较 PR 基线与 HEAD 的变更文件，输出 docs_only（是否仅 *.md、docs/、LICENSE 等文档文件）、backend_changed、frontend_changed、api_changed、infra_changed。push 到 main 时一律视为全部变更（不跳过任何重型任务）。
2. `light-checks`（始终运行）：
   - gofmt -l 检查无未格式化文件；go vet；`go mod tidy` 后 git diff --exit-code（依赖整洁）；
   - 密钥检查脚本 backend/scripts/check-secrets.sh：扫描整个仓库已跟踪文件，匹配私钥头（BEGIN … PRIVATE KEY）、疑似 AI 密钥（sk- 开头的长串）、Telegram Bot 令牌格式（数字:35 位字符串）、被跟踪的 .env 文件（.env.example 除外）；发现即失败，输出文件与行号但**不输出命中的密钥内容**；
   - 文档检查：根 README 文档索引里引用的 .md 文件都存在。
3. `backend-tests`（backend 或 infra 变更，或 main）：ubuntu runner，`go test -race -count=1 ./...` 且排除需要 Docker 的集成测试用 `-skip`（名称见 backend/internal/platform/db/db_integration_test.go）。
4. `backend-integration`（backend 或 infra 变更，或 main）：运行包含 testcontainers 的集成测试（GitHub 托管的 ubuntu runner 自带 Docker）；超时 15 分钟；输出每个测试耗时。
5. `docker-image`（backend 或 infra 变更，或 main）：构建 backend 镜像（使用 buildx，开启 GitHub Actions 缓存），然后用 postgres 服务容器启动镜像，设置必要环境变量（APP_ENV、DATABASE_URL 指向服务容器，使用测试用占位凭据），断言：/healthz 返回 200；/readyz 返回 200；容器健康状态在 60 秒内变为 healthy；容器内用户为非 root（uid 10001）。结束时无论成败都清理容器。
6. `openapi-check`（api 或 backend 变更，或 main）：**条件步骤**——若 backend/scripts/check-generated.sh 存在（由 F-04 提供）则运行它并要求退出码 0；不存在则输出"F-04 尚未合并，跳过"并成功。
7. `frontend-check`（frontend 变更，或 main）：**条件步骤**——若存在 pnpm-workspace.yaml（由 F-06 提供）则安装依赖、类型检查、构建、词条完整性检查；不存在则输出"F-06 尚未合并，跳过"并成功。
8. `vuln-scan`（backend 变更，或 main，允许失败时告警而非阻塞，先设为 continue-on-error 并在 README 说明何时改为阻塞）：govulncheck ./...。
9. `ci-gate`（**始终运行**，needs 以上所有 job，if: always()）：汇总结果——任何必需 job 失败或被取消则失败；被"跳过"（因为 docs_only 等条件）视为通过。**这是将来设置为"必需状态检查"的唯一一项**，因为被路径条件跳过的 job 不会产生状态，直接设为必需会让 PR 永远等待。
附加文件：
- .github/dependabot.yml：gomod（/backend）、github-actions、docker（/backend）每周检查，分组更新，限制同时打开的 PR 数量。
- .github/workflows/README.md：每个 job 做什么、什么时候跑/跳过、如何在本地复现（给出对应 make 命令或 docker 命令）、观察指标说明、**如何把 ci-gate 设为必需检查（需要仓库管理员在 Settings → Branches 里操作，你不能代做）**、以及 vuln-scan 改为阻塞的条件。

【范围——不要做】
- 不做部署（F-09）；不使用任何仓库密钥/Secrets；不向仓库写入（不自动提交、不打标签）；不修改分支保护设置；
- 不引入第三方 actions（官方除外）；不在日志里输出敏感信息；
- 不修改业务代码；发现问题只记录。

【验收标准——逐条自测并在 PR 描述里写结果；务必基于真实的 Actions 运行结果，不要凭推测】
□ 在你的 PR 上，所有 job 真实运行并全部通过；PR 描述里附上 Actions 运行链接和每个 job 的耗时
□ 另外做一次**纯文档变更**的演示 PR（或在同一分支上追加一次仅改 .md 的提交并说明）：确认重型 job 被跳过、ci-gate 仍通过、light-checks 仍运行
□ **负向测试**（各做一次后恢复，并在 PR 里附运行链接）：故意让一个单元测试失败 → CI 失败；故意提交一个 gofmt 不合格文件 → CI 失败；故意提交一个形如密钥的测试字符串（使用明显无效的假值）→ check-secrets 失败且输出中不含该字符串；故意让镜像的健康检查指向错误端口 → docker-image 失败
□ ci-gate 在上游任一必需 job 失败时失败，在仅被跳过时通过（负向测试覆盖）
□ 工作流 YAML 通过 actionlint（可用 docker run rhysd/actionlint 本地验证，注明版本）
□ concurrency、permissions、timeout 都已设置；只使用官方 actions 且固定了版本标签
□ README 写清所有 job、本地复现方式、设置必需检查的步骤
□ PR 描述含"发现的问题"小节（没有则写"无"），以及整个 CI 的总耗时（并记录：中位耗时目标 ≤ 8 分钟，若本次明显超过，说明瓶颈和优化建议）

【如何验证（你会亲自跑；前一步失败就停下报告）】
0. 预检：git remote -v 确认能推送分支；gh --version；若不能创建 PR 或查看 Actions 结果，立刻停下并向用户报告需要什么权限，**不要**用推测代替真实运行结果。
1. 本地先跑：cd backend && go vet ./... && go test -race -count=1 -skip TestDatabaseFramework ./...（-race 需要 CGO，Windows 上通常不可用，请用 Linux 容器：docker run --rm -v <目录>:/src -w /src/backend golang:<go.mod 里的版本> …）
2. actionlint 本地校验
3. 推送分支、创建 PR，等待并记录 Actions 运行结果
4. 依次做上述演示 PR 与负向测试

【遇到不确定】
不要猜测，写进 PR 描述"待确认"。尤其：如果 GitHub 托管 runner 的 Docker 行为与本地不同导致集成测试不稳定，如实记录失败日志，不要通过放宽断言或跳过测试来"变绿"。

【完成后】
在 PR 描述里按模板填写，并附 5 行以内总结。
```

---

## 合并前的评审提示

| 任务 | 重点看什么 |
|---|---|
| F-04 | 规范里的信封和错误码是否和 `httpx` 完全一致；一致性测试有没有真的会失败（让它们演示删掉一个码）；生成的代码有没有时间戳导致每次都有差异；`x-roles` 与 AGENTS.md 规则 10 是否对应；有没有顺手加了业务路径 |
| F-08 | 是否基于**真实 Actions 运行**（链接）而不是推测；`ci-gate` 对"跳过"和"失败"的区分；负向测试是否真的做了；有没有为了变绿放宽测试；有没有用到 Secrets 或第三方 actions；`check-secrets` 输出里有没有泄露命中内容 |

## 合并 F-08 之后你要做的一件事
在 GitHub：Settings → Branches → 给 `main` 添加保护规则，勾选 **Require status checks to pass**，只选 **`ci-gate`**，并勾选 **Require a pull request before merging**。这一步需要仓库管理员权限，Codex 和我都不能替你做。
