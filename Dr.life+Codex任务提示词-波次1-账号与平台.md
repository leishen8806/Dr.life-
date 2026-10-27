# 波次 1：账号、权限、审计与文件（A-03 至 A-10、Q-01、F-14）

**用法：** 整块复制代码块内容粘贴给 Codex，**每个任务一个独立 worktree 与分支**。基线 `origin/main` 最新提交。
**新增任务（原任务表遗漏）：** A-10 商户入驻与核验、F-14 文件存储与预签名上传。
**顺序提示：** Q-01（审计）与 F-14（文件）是其他任务的基础，建议最先开；A-03 依赖 A-02（含真机验收闸门）；A-05/A-07/A-08/A-10 依赖 A-02/A-06 的部分可先用 fake 接口，合并前再联调。

---

## A-03 会话与令牌（刷新、作废、设备）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】A-03 会话与令牌（刷新、作废、设备）
【分支】task/A-03-sessions
【依赖】A-02 已合并（含真机验收闸门通过）；S-01 已合并；A-01
【先阅读】
1. Dr.life+API安全协议与容量及队列设计.md 第三章 3.1、3.2、第四章
2. Dr.life+开发任务拆分与验收.md：A-02、A-03、A-04；backend/migrations/00003_iam_tables.sql（iam.session、account_device）
3. backend/internal/iam/（A-02 的令牌签发、认证器、SessionChecker 注入点）、backend/internal/platform/gateway/
【范围——要做】
1. OpenAPI 先行：`POST /v1/public/auth/refresh`（请求体 refresh_token；Web 端后续走 Cookie，本任务只实现 Body 方式并让签发代码可复用）、`POST /v1/{buyer|merchant|driver}/auth/logout`（作废当前会话）、`GET /v1/{end}/me/sessions`（列出自己的会话与设备）、`DELETE /v1/{end}/me/sessions/{id}`（作废自己的某会话）。
2. 刷新令牌轮换：每次使用即换新（新令牌哈希入库，旧的标记已用）；**复用检测**：已用过的刷新令牌再次出现且超出宽限窗口 → 整条会话链（同 session family）作废，写安全事件与指标并告警。**宽限窗口**（默认 10 秒，可配置）内重复使用旧令牌（网络重试导致）返回**同一份**新令牌对，避免误判；需要 iam.session 增加 family_id、previous_hash、rotated_at 等列（新增迁移，不改旧迁移）。
3. 并发刷新安全：同一会话 50 个并发刷新只成功产生一对新令牌，其余在宽限窗口内拿到相同结果；用 SELECT … FOR UPDATE 或 advisory lock，不得产生分叉。
4. 有效期：Mini App 绝对 7 天、空闲 24 小时（可配置）；记录 last_used_at；过期会话清理任务（用 S-03 的 scheduled 队列，若未合并则提供可调用函数并预留注册）。
5. 即时作废：Redis（缓存实例）里维护「已作废会话集」与「账号纪元号」；认证器每个请求 O(1) 检查（SessionChecker 的真实实现替换 A-02 的默认放行）；**资金与后台类路由在 Redis 不可用时 fail-closed（拒绝），普通读接口可放行**（因访问令牌仅 15 分钟）；数据库为准，Redis 丢失后能从 iam.session 重建。
6. 提供内部接口 `RevokeAllSessions(ctx, accountID, reason)` 与 `RevokeSession`：账号冻结/停用、员工停用、改权限、复用检测时调用；作废要同时写数据库并更新 Redis；**冻结账号后旧访问令牌立即失效**（不是等 15 分钟）。
7. 设备记录：account_device 随登录/刷新更新 last_seen_at；会话列表返回设备标识、IP 前缀（打码）、最近使用时间，**不返回刷新令牌或其哈希**。
8. 指标与日志：刷新成功/失败/复用检测次数（标签只用端与结果），日志不含令牌。
9. **补齐 A-02 令牌与规范的偏差（访问令牌 aud 与 typ）**：A-02 已合并的令牌（backend/internal/iam/token.go）缺少 `aud` 声明与 `typ` 头，按《API 安全协议》3.1 补齐：①JWT 头增加 `typ: at+jwt`，验证时必须检查（缺失或不等于 at+jwt 拒绝）；②声明增加 `iss`（配置项，默认 drlife）与 `aud`（**按端区分**：buyer→`drlife:buyer`、merchant→`drlife:merchant`、driver→`drlife:driver`；后台留给 A-06：`drlife:admin`、`drlife:wms`），签发时由 end 决定，验证时**必须与请求路径所属的端一致**（/v1/buyer/* 只接受 aud=drlife:buyer；不一致返回与『签名错』相同的 401，不暴露原因），用 `jwt.WithAudience`/`WithIssuer` 之外再加路径级校验放在认证器里；③`role` 声明与 `aud` 必须相互一致（buyer↔drlife:buyer 等），不一致拒绝；④**向后兼容过渡**：已签发的旧令牌（无 aud/typ）最长 15 分钟，过渡期内（配置开关 `TOKEN_LEGACY_ACCEPT_UNTIL`，默认关闭、上线时按需开 15 分钟）可接受旧格式，之后拒绝；开关到期后相应代码路径要有测试证明已拒绝；⑤刷新接口签发的新访问令牌同样带 aud/typ/iss；会话列表接口不暴露 aud 以外的令牌内容；⑥测试：缺 typ、错 typ（如 JWT）、缺 aud、错 aud（买家令牌访问 /v1/merchant/*、/v1/driver/*）、role 与 aud 不一致、alg=none/HS384/RS256 混淆、错误 kid、过期、旧令牌过渡开关开/关两种状态；⑦不得修改 A-02 已合并的登录行为与错误码，只扩展令牌格式；README 与 openapi 的安全方案描述同步更新（bearerFormat 说明 typ/aud）。
【验收标准】
① 冻结账号后其旧访问令牌立即失效（集成测试：先通过一次受保护接口，冻结，再次访问立即 401/403）
② 刷新令牌每次轮换；复用检测触发整链作废；宽限窗口内重试不误判
③ 50 并发刷新同一会话安全（无分叉、无 500）
④ Redis 不可用时资金/后台类路由拒绝、普通读放行；Redis 重建后与数据库一致
⑤ 令牌、哈希不出现在日志、错误、响应（子串断言）
⑥ 访问令牌带 typ=at+jwt 与按端区分的 aud/iss，错端/错 typ/缺 aud/role-aud 不一致的令牌一律 401；旧格式令牌仅在过渡开关打开期间被接受
⑦ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 宽限窗口长度；同一账号最大并发会话数（默认 5，超出踢最旧）
【红线】刷新令牌只存哈希；不得在客户端响应之外返回刷新令牌；不改 A-02 已合并行为（只扩展）；不为通过测试放宽 fail-closed。
```

---

## A-04 账号状态与语言登记

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】A-04 账号状态与语言登记
【分支】task/A-04-account-status-lang
【依赖】A-01、A-03（作废会话）、S-03（事件，可先用 iam outbox 并预留）
【先阅读】
1. Dr.life+数据库表结构与账务详细设计.md 第二章（account 状态与 lang 约束）
2. Dr.life+技术方案与系统边界.md 第六章「账号」状态机；Dr.life+交互文档.md 第一章 2、5（首次进入、多语言）
3. Dr.life+开发任务拆分与验收.md：A-04；backend/internal/iam/
【范围——要做】
1. 账号状态服务：`SetAccountStatus(ctx, accountID, status, reason, actor)`，状态 active ⇄ frozen ⇄ disabled；**冻结/停用必须填原因（数据库 CHECK 已有，Go 层先校验）**；每次变更同事务写 iam 的 outbox 事件 `iam.account.status_changed`（payload：账号 public_id、旧/新状态、角色、merchant public_id、操作人、原因摘要，**不含个人信息**），并调用 A-03 的 RevokeAllSessions。若 iam 还没有 outbox 表，新增 `iam.outbox`（结构同 ledger.outbox 风格）并交给 S-03 的 Relay 路由；Relay 未合并时只写表。
2. 停用商户老板账号时：商家下属员工账号一并停用并作废会话；事件里带 merchant_id，让 catalog 消费后隐藏其商品（消费方在 T-01；本任务只发事件并写契约说明）。
3. 后台接口（/v1/admin/accounts/{id}/status）：需要 RBAC 的 `usr` 模块 edit 权限（A-06 未合并前用可注入的权限检查接口 + fake）、**step-up**（高风险动作，用 S-01 的 RequireStepUp("account_status")）、必填 reason、写审计；返回统一错误。禁止把最后一位启用的超级管理员相关账号冻结的规则属于 A-06，这里只处理业务账号。
4. 语言登记：`PUT /v1/{buyer|merchant|driver}/me/language`（zh/en/km，其他 400）；首次登记写 lang_registered_at，之后可修改（个人中心）；`GET /v1/{end}/me` 返回 lang 与 needs_language；**商户与设备绑定的语言规则**：商户员工各自登记一种语言（每个 account 一种）；出站通知用登记语言（供 N-01 读取的只读接口 `GetAccountLanguage`）。
5. 账号设备：沿用 A-02/A-03 的 account_device，提供 `GET /v1/{end}/me/devices`。
6. 后台账号详情需要的只读聚合：账号状态历史（新增 iam.account_status_log：账号、旧/新状态、原因、操作人、时间，**只追加**，触发器拒绝 UPDATE/DELETE）。
【验收标准】
① 无原因冻结/停用被拒（Go 与数据库双层）
② 语言只能 zh/en/km；未登记返回 needs_language=true；已登记可修改并记录时间
③ 冻结/停用立即作废该账号会话；停用商户老板时员工一并停用，并发出可被 catalog 消费的事件
④ 状态历史只追加；后台状态变更需要 step-up、权限与审计（fake 实现下的集成测试）
⑤ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 解冻是否也需要 step-up 与原因（默认需要）
- 员工语言是否允许老板代改（默认不允许，仅本人）
【红线】事件里不放姓名、手机号、Telegram 用户名；状态变更与事件、会话作废必须同事务或可恢复（outbox）；不直接改 catalog 的表。
```

---

## A-05 商户员工与角色权限

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】A-05 商户员工与角色权限
【分支】task/A-05-merchant-staff
【依赖】A-01、A-02、A-03、商户记录（A-10 合并前用测试夹具创建 merchant，A-10 合并后联调）、S-01
【先阅读】
1. Dr.life+数据库表结构与账务详细设计.md 第二章（merchant_staff、merchant_role_perm、template、merchant_op_log）
2. Dr.life+交互文档.md 第三章（商家端：我的 → 员工管理，三页签、右上「员工管理 +」弹窗）与第一章 2
3. Dr.life+产品说明-五类角色与数据流.md 中商家员工部分；backend/migrations/00003 里 merchant_role_perm_template 的种子矩阵（manager veeeveeve / sales veevnnvne / warehouse vvvennvne / finance vnvvevvne）
4. Dr.life+开发任务拆分与验收.md：A-05
【范围——要做】
1. 员工邀请：老板创建邀请（姓名、手机、备注、角色），手机号规范化后用 `phone_hash`（HMAC，密钥来自 F-10，**不是裸 SHA**）保证同商户内唯一，`phone_enc` 信封加密；生成一次性 invite_code（高熵、72 小时过期、使用即失效、可撤销）；邀请链接用 Telegram 启动参数（startapp=invite_<code>）。
2. 接受邀请：员工用 Telegram 登录（A-02 已解析 start_param），登录成功后在**同一事务**内用邀请码把该 Telegram 账号绑定到 merchant_staff（account role=merchant_staff，merchant_id=该商户），邀请码一次性；邀请已用/过期/撤销返回统一的失败码（不泄露原因）；同一个人已是该商户员工则幂等。需要在 A-02 的登录路径提供**钩子接口**（不要改 A-02 已合并行为，用扩展点）。
3. 角色与权限矩阵：四种角色（manager/sales/warehouse/finance）× 九个模块（dash/prod/orders/store/fin/ads/prof/staff/ai）× none/view/edit；创建商户时复制模板到 merchant_role_perm（A-10 调用本模块的 `InitMerchantPerms`）；老板可调整某角色的模块权限，**staff 模块只允许 none/view（数据库 CHECK 已有）**；老板永远拥有全部权限（不受矩阵限制）。
4. 权限执行中间件 `RequireMerchantPerm(module, level)`：从令牌取账号→查员工/老板→矩阵（Redis 缓存 60 秒，权限变更主动失效）；无权限返回 403；供其他商家侧模块（T/W/N 系列）复用并写入 README；提供表驱动测试覆盖 4 角色 × 9 模块 × 2 级别。
5. 员工管理接口（/v1/merchant/staff*）：列表（三页签：全部/已启用/待接受或已停用，分页）、邀请、改角色、停用/启用（停用立即作废会话，调用 A-03）、删除（软删除，保留操作记录）、撤销邀请；**只有老板可邀请/停用/删除/改权限**；员工上限 10（含已邀请未接受，应用层校验，并发下不得超限——用 merchant 行锁或唯一计数约束）。
6. 员工操作记录 merchant_op_log：关键写操作（改商品价格、发货、改员工等）由各模块通过 `RecordOp(ctx, action, detail)` 写入（只追加，触发器拒绝 UPDATE/DELETE）；老板可查看每位员工的操作记录（分页）。
7. 隐私：员工手机号在非老板视图脱敏；日志不含手机号。
【验收标准】
① 只有老板能邀请/停用/删除/改权限；其他人 403（越权测试）
② 无权限的接口返回 403；staff 模块只允许 none/view（数据库与 Go 双层）
③ 邀请码一次性、过期失效、撤销失效；并发使用同一邀请码只有一个绑定成功
④ 员工上限 10，并发邀请不超限；同商户同手机号不能重复邀请
⑤ 停用员工立即作废其会话；操作记录只追加；矩阵测试 4×9×2 全覆盖
⑥ go vet、-race、ci-gate 全绿；参照《交互文档》商家端流程
【待确认（不要自己拍板，PR 里列出）】
- 邀请码有效期（默认 72 小时）
- 员工被停用后是否保留其历史操作记录可见（默认保留）
【红线】权限与范围校验只在服务端；phone_hash 必须是带密钥的 HMAC；邀请码不进日志；不让员工通过任何接口提升自己的权限。
```

---

## A-06 平台管理员与 RBAC（含 step-up 签发）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】A-06 平台管理员与 RBAC（含 step-up 签发）
【分支】task/A-06-admin-rbac
【依赖】A-01、S-01（IP 白名单、step-up 接口）、A-03（会话方案）、Q-01（审计，可先用 fake）
【先阅读】
1. Dr.life+平台后台权限与审计规范.md 全文（矩阵、职责分离规则、关键操作附加控制、18 项审计）
2. Dr.life+API安全协议与容量及队列设计.md 3.2、3.4
3. Dr.life+数据库表结构与账务详细设计.md 第二章（admin_user、admin_role_perm、actor）；backend/migrations/00003（已有 17×6 矩阵种子，逐格与规范一致）
4. Dr.life+开发任务拆分与验收.md：A-06；backend/internal/iam/、platform/gateway/
【范围——要做】
1. 管理员账号：admin_user（argon2id，参数写进配置：m≥64MiB、t≥3、p≥2，可调；密码强度与常见弱口令检查；**不得有默认账号/默认口令**）；初始超级管理员通过命令行 `adminctl create-super`（密码从交互输入或安全文件读取，**不进 shell 历史、不进日志**）创建；TOTP（RFC 6238，30 秒、6 位、±1 窗口、**同一步长码只能用一次**防重放）密钥信封加密存储；10 个一次性恢复码（只存哈希）；强制首次登录绑定 TOTP。
2. 登录：`POST /v1/admin/auth/login`（邮箱+密码→返回需要 TOTP 的中间态令牌，5 分钟）、`POST /v1/admin/auth/totp`（验证后签发访问令牌 10 分钟 + 刷新令牌）；物流仓储 Web 同样接口前缀 `/v1/wms/auth/*`，`aud` 分别为 drlife:admin / drlife:wms；**统一错误**（不区分邮箱不存在/密码错/TOTP 错，用恒定时间路径与假哈希防用户枚举）；失败限流与锁定（账号 5 次失败锁 15 分钟，IP 另算，指数增长），写 login 审计事件；刷新令牌走 **HttpOnly+Secure+SameSite=Strict 的 host-only Cookie**（Path 限定各自 auth 路径），刷新端点要求自定义头 X-Requested-With: drlife 并校验 Origin；轮换与复用检测复用 A-03 的实现；会话：空闲 15 分钟、绝对 8 小时、同账号并发会话上限（默认 3），管理员可远程作废他人会话（需 super）。
3. **step-up 签发**（实现 S-01 的 StepUpVerifier 的签发端）：已登录管理员用 TOTP 再确认 → 签发 5 分钟、单次有效、绑定动作 scope 的 step-up 令牌；适用动作清单在代码里集中声明（出款确认/复核、解除出款冻结、账户冻结、权限变更、集成密钥变更等）。
4. RBAC：6 角色 × 17 模块 × none/view/edit，数据库为准（admin_role_perm），**`super` 固定全 edit**；中间件 `RequireAdminPerm(module, level)`（缓存 60 秒，变更主动失效）；数据范围（data_scope：地区）提供查询过滤辅助 `ScopeFilter`；敏感字段脱敏辅助（手机号等；无 `sens` 权限看到的是脱敏值），脱敏在**服务端序列化层**统一实现。
5. 管理员管理（仅 super）：创建/停用/改角色/改数据范围/重置 TOTP；**超级管理员至少保留 1 位启用**：Go 层校验 + **数据库触发器**（删除/停用/降级最后一位启用的 super 被拒，并发下也成立）；谁能改自己的角色：不能自己提权；所有变更写审计（含前后值）。
6. 职责分离自检：提供 `GET /v1/admin/rbac/sod-check`，按《权限与审计规范》「职责分离规则」检测当前矩阵与角色分配中的冲突（例如同一管理员同时具备裁决与出款），返回冲突清单；变更矩阵/分配时实时返回警告（不阻断，但写审计）。
7. IP 白名单与限流：沿用 S-01 的 /v1/admin/*、/v1/wms/* 路径级配置；登录接口使用更严限流规则集。
【验收标准】
① 矩阵与《权限与审计规范》逐格一致（用测试逐格比对，不是数行数）；`super` 不能被改成非 edit
② 越权被拒（每个角色对每个模块的 view/edit 边界各有测试）；无 sens 权限时手机号脱敏
③ 超级管理员至少保留 1 位启用（并发删除/停用两位 super 时数据库兜底）
④ 登录失败限流与锁定；用户枚举不可区分（响应与耗时）；TOTP 同码不可重放；恢复码一次性
⑤ step-up 令牌单次有效、5 分钟、绑定 scope；Cookie 刷新的 CSRF 防护（缺头/错 Origin 被拒）
⑥ go vet、-race、ci-gate 全绿；日志中无密码、TOTP 密钥、令牌
【待确认（不要自己拍板，PR 里列出）】
- TOTP 之外是否需要硬件密钥（第一版不做）
- 管理员并发会话上限默认 3
- 数据范围（地区）的取值字典（先用字符串数组，地区表在后续）
【红线】密码只存 argon2id 哈希；TOTP 密钥只存密文；不得有默认口令；脱敏只在服务端；不得返回 step-up 令牌以外的任何二次验证细节。
```

---

## A-07 Bot 池管理（后台）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】A-07 Bot 池管理（后台）
【分支】task/A-07-bot-pool-admin
【依赖】A-01、A-02、F-10、A-06（RBAC，可先 fake）、S-01
【先阅读】
1. Dr.life+第一版交付形态与技术架构.md 第二章（Bot 池、应急切换、监控）与第七章「密钥与令牌保管」
2. Dr.life+平台后台权限与审计规范.md（集成管理、仅超管可录入/更换令牌）；Dr.life+交互文档.md 第六章（平台 Web：消息通知 → Telegram Bot 池）
3. Dr.life+开发任务拆分与验收.md：A-07；backend/internal/iam/（A-02 的 Bot 令牌缓存）、platform/secrets/
【范围——要做】
1. 后台接口（/v1/admin/bots*，需 RBAC `integ` 模块 edit，**添加/更换令牌仅 super** + step-up）：列表（每个端的池：用户名、角色 primary/standby、状态 active/standby/limited/banned、token_last4、已绑定用户数、最近发送成功率）；添加 Bot（先经 Telegram 适配层 `getMe` 校验令牌有效且用户名一致，再信封加密入库，AAD=`bot_token:<id>`，只保存末 4 位用于展示；生成 webhook_secret（高熵，信封加密）并通过适配层 `setWebhook` 设置到 `/v1/webhook/telegram/<bot public_id>`）；设为主（**原子交换**：同一事务内旧主变 standby、新主变 primary，满足部分唯一索引，且不能把 banned 设为主）；标记受限/封禁/恢复；删除（**同一端至少保留一个可用 Bot，且不能删除当前主 Bot（须先换主）**）。
2. 令牌从不出现在日志、接口响应、审计 detail 中（审计只记末 4 位）；响应只给 token_last4；更换令牌写审计（前后末 4 位）；变更后**主动失效** A-02 的解密令牌缓存。
3. Telegram 适配层（`backend/internal/comms/telegram` 的 adapter 接口 + 模拟实现；与 S-04 共用接口，若 S-04 未合并则在此定义最小接口 `GetMe`/`SetWebhook`/`DeleteWebhook`）：超时、重试、熔断，调用留日志（不含令牌）。
4. Webhook 入站（/v1/webhook/telegram/{botPublicID}）：用 S-01 的固定头密钥验签（`X-Telegram-Bot-Api-Secret-Token`，每个 Bot 独立 secret，恒定时间比较）；处理 `/start`（建立/更新 account_bot：bound_at、write_access）、`my_chat_member`（屏蔽/恢复 → unreachable_at 更新）；幂等（update_id 去重，Redis 短期记录）；未知 Bot/验签失败统一拒绝。**实现 S-04 需要的 `ReachabilityRecorder` 接口**。
5. 应急切换演示：主 Bot 标记 limited/banned 后，新入口链接/启动参数指向新的主 Bot（提供 `GET /v1/public/entry/{end}` 返回当前主 Bot 的入口链接，可缓存 60 秒）；旧 Bot 令牌仍在池里时，已有用户从旧入口进来仍能通过 A-02 校验。
6. 监控数据来源：每个 Bot 的绑定用户数、登录命中数（来自 login_event 聚合，不要在请求里实时扫表，使用定时聚合或计数器）。
【验收标准】
① 同一端至少保留一个 Bot；删除主 Bot 被拒；换主是原子的，并发换主不会产生两个 primary
② 所有变更写审计，令牌从不出现在日志与响应（子串断言覆盖响应、日志、审计 detail、错误）
③ 主 Bot 受限后入口指向备用，旧入口用户仍可登录（与 A-02 联测）
④ Webhook 验签失败/缺头/未知 Bot 被拒；update_id 重放只处理一次；/start 建立 account_bot
⑤ 添加 Bot 前 getMe 校验失败则不入库；仅 super 可录入/更换令牌（越权测试）
⑥ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- Webhook 公网域名（走 adminapi 的 /v1/webhook 前缀，是否需要单独域名）
- Bot 受限/封禁的自动判定（本任务只手动标记 + 发送失败率告警，自动切换留待运营决定）
【红线】令牌只在内存中短暂存在；不得把令牌写入任何日志或审计；不得直接跨 schema 写 comms；webhook 路径未绑定验证器即拒绝。
```

---

## A-08 账号关联（买家 ⇄ 商家）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】A-08 账号关联（买家 ⇄ 商家）
【分支】task/A-08-account-link
【依赖】A-01、A-02、A-06（后台查看）
【先阅读】
1. Dr.life+消息通知、渠道关联、埋点与遗漏清单.md 中账号关联；Dr.life+交互文档.md 买家端「关联账号」；prototype/index.html 中账号关联演示
2. Dr.life+数据库表结构与账务详细设计.md 第二章 account_link；Dr.life+开发任务拆分与验收.md：A-08
【范围——要做】
1. 检测：`GET /v1/{merchant|buyer}/me/link-candidates`：**只在同一 Telegram 用户编号（同一 person）下**查找另一角色的账号，返回对方角色与账号 public_id（不返回其他个人信息）；
2. 关联：`POST /v1/merchant/me/links`（**仅商家老板**可发起，带 Idempotency-Key）把本人的买家账号与商家老板账号关联；解除 `DELETE`；同一对账号任一时刻最多一条未解除记录（数据库部分唯一索引已有，Go 层并发处理）；
3. **关联不改变任何资金、订单、权限归属**：不得合并钱包，不得共享订单可见性（测试证明：关联前后买家账号的订单/钱包可见范围不变，商家账号同理）；解除不删任何数据；
4. 关联后的唯一用途：同一人在两个入口间一键切换（返回对方入口链接）与平台端查看关联关系；
5. 后台：`GET /v1/admin/accounts/{id}/links`（权限 usr view）；解除/关联写审计与 login 以外的安全事件。
【验收标准】
① 只在同一 Telegram 用户编号下匹配（构造两个不同人的测试，互相检测不到）
② 仅商家老板可关联（员工、买家发起均 403）
③ 关联/解除不改变资金、订单、权限；解除不删数据
④ 并发重复关联只产生一条未解除记录；平台端可见关联关系
⑤ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 是否允许一位买家账号关联多个商家老板账号（默认一对一）
【红线】不得返回对方账号的姓名/手机号等个人信息；不得合并任何业务数据。
```

---

## A-09 协议同意记录

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】A-09 协议同意记录
【分支】task/A-09-consent
【依赖】A-01、A-02；N-07（协议发布）可后接
【先阅读】
1. Dr.life+用户协议与隐私政策草案(三语).md；Dr.life+交互文档.md 第一章 2（协议勾选默认勾选、取消 → 未同意页）
2. Dr.life+数据库表结构与账务详细设计.md 第八章 ops.consent / ops.legal_doc；Dr.life+开发任务拆分与验收.md：A-09、N-07
【范围——要做】
1. 迁移 `ops.consent`（account_id、kind: terms/privacy、version、decision: accepted/declined、at、ip 前缀、device_id；**只追加**，触发器拒绝 UPDATE/DELETE；同一账号同一 kind 同一 version 最多一条 accepted）；只读接口 `LegalVersionProvider.Latest(kind)`（N-07 实现；本任务提供基于配置的默认实现，种子 v1.0）。
2. 接口：`GET /v1/{end}/me/consents`（返回各 kind 的最新版本、用户已同意版本、是否需要同意）、`POST /v1/{end}/me/consents`（kind、version，必须等于当前最新版本，否则 409）；用户点取消 → 记 declined。
3. **受限中间件** `RequireConsent`：账号未同意最新版本时，除白名单路由（登录、/me、语言、同意、退出、/public）外**所有接口返回 428 + 专用错误码 CONSENT_REQUIRED**（带需同意的 kind 与版本）；已同意旧版本、发布新版本后下一次请求即返回需同意；declined 的账号返回明确状态（无法使用），**不删除数据**。状态缓存 60 秒并在发布新版本/提交同意时主动失效。
4. 与 A-02 对接：登录响应增加 `needs_consent`；新账号默认未同意。
5. 后台：N-07 将提供覆盖率统计；本任务提供聚合查询函数（版本维度已同意/未同意账号数）。
【验收标准】
① 新账号未同意时受限（只能进入同意流程）
② 发布新版本后用户下次请求返回需同意
③ 取消同意则无法使用并返回明确状态；同意记录带版本与时间，只追加
④ 同意版本必须等于最新版本（旧版本提交被拒）
⑤ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 默认勾选的合规风险已由用户决定保留（见文档）：保留「取消」按钮与独立的同意记录，不要擅自移除
- 同意记录是否需要保存 IP（默认仅保存前缀）
【红线】同意记录只追加；不得在中间件里读取 iam 以外的业务表；白名单外路由不得漏拦。
```

---

## A-10 商户入驻与核验（新增任务）

> 原任务表遗漏：谁来创建 iam.merchant 并核验？本任务补上（merchant_owner 首次登录后补资料 → 平台核验）。

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】A-10 商户入驻与核验（新增任务）
【分支】task/A-10-merchant-onboarding
【依赖】A-01、A-02、A-05（权限模板初始化）、A-06、F-14（文件）
【先阅读】
1. Dr.life+产品需求文档PRD.md 商家章节与第 9 章 P0（账户与合规、商户核验）；Dr.life+交互文档.md 第三章；Dr.life+数据库表结构与账务详细设计.md 第二章 merchant
2. Dr.life+平台后台权限与审计规范.md（商户核验、数据范围）
【范围——要做】
1. 商家端：`POST /v1/merchant/profile`（老板提交入驻资料：店铺名三语、类型、地区、服务、简介三语、电话/地址/联系人——后三者信封加密存储，证照图片走 F-14 预签名上传）→ 创建 iam.merchant（kind=pending, status=active）、把 owner_account 绑定、复制权限模板（A-05 的 InitMerchantPerms）；资料可在核验前修改，核验后关键字段（店铺名、证照）修改需要重新核验。
2. 后台：`GET /v1/admin/merchants`（筛选 kind/status/地区，分页，手机号脱敏）、`POST /v1/admin/merchants/{id}/verify`（pending→verified，**记录核验人与备注**）、`.../reject`（必填原因）、`.../suspend`（停用，联动 A-04 的账号状态与事件）；权限 `mer` 模块；核验、驳回、停用都需要 step-up 与审计。
3. 商户状态对其他模块的影响通过事件发布（`iam.merchant.status_changed`、`iam.merchant.verified`），catalog 等消费；**未核验商户**能做什么（能否上架、能否发货）由各模块按 kind 判断，本任务提供只读接口 `GetMerchantKind`。
4. 自营商户（kind=self）：由 super 通过后台创建，不走自助入驻。
【验收标准】
① 只有 merchant_owner 能提交入驻；员工不能
② 核验/驳回/停用需要 mer 权限、step-up、审计；驳回必须有原因
③ 电话、地址、联系人密文存储，日志与列表脱敏；无 sens 权限看不到明文
④ 创建商户后权限模板已复制（4 角色 × 9 模块）；发出商户状态事件
⑤ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 入驻所需证照清单（先做通用附件列表）
- 核验是否需要线下上门（流程，不影响本任务）
【红线】不得把电话/地址/联系人明文写入日志、事件、审计；核验状态变更必须有记录。
```

---

## Q-01 审计日志（哈希链）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】Q-01 审计日志（哈希链）
【分支】task/Q-01-audit-chain
【依赖】F-03、F-10；S-03（outbox 消费，可后接）
【先阅读】
1. Dr.life+数据库表结构与账务详细设计.md 第七章 3（audit.log 哈希链）；Dr.life+平台后台权限与审计规范.md「审计日志」与第四章（18 项审计）
2. backend/migrations/00002_roles.sql（audit_writer 角色、INSERT/SELECT 权限）；backend/internal/platform/（outbox 约定）
【范围——要做】
1. 迁移 `audit.log`（按设计文档：id、ts、actor_id、actor_role、action、target、detail jsonb、ip、request_id、prev_hash、hash）与 `audit.chain_head`（单行：last_id、last_hash，用于串行化追加）、`audit.check_result`（day、check_id、severity、detail，供 Q-02）；`audit_writer` 仅 INSERT/SELECT；触发器拒绝 UPDATE/DELETE/TRUNCATE（SQLSTATE 55000）；`report_ro` 可读。
2. **哈希规范**：`hash = SHA-256(len-prefixed 编码( prev_hash || ts(UTC 纳秒) || actor_id || actor_role || action || target || canonical_json(detail) || ip || request_id ))`；各字段用**长度前缀**编码避免拼接歧义；detail 用确定性规范化 JSON（键排序、无空白、数字规范）；创世块固定 prev_hash。实现并单测向量。
3. `audit.Writer`：`Append(ctx, Entry)`：在事务内锁 chain_head 行（SELECT … FOR UPDATE）→ 计算 hash → 插入 → 更新 head；**1000 并发追加后链连续无分叉**；Entry 字段校验（action 为枚举字符串、detail 大小上限 16KB、**detail 脱敏过滤器**：键名含 password/token/secret/key/phone 的值替换为 [REDACTED]，只记前后值的非敏感字段）。接口放在 `platform/audit`，其他模块依赖接口（fake 供测试）。
4. **事件到审计**：各模块的后台写操作通过各自 outbox 事件（带 event_id、actor、action、target、before/after）到达；提供 `audit.Consumer`（S-03 的 Idempotent 包装，以 event_id 为幂等键，唯一约束 `audit.event_seen`）把事件追加进链；同步场景（登录、step-up、权限变更）可直接调用 Writer。
5. **校验**：`audit.Verify(ctx, fromID, toID)` 重新计算整条链，发现断链/篡改返回第一处错误位置；命令行 `auditctl verify`；每日**锚定**：把当天最后一个 hash 与 id 写入「锚点接收器」接口 `AnchorSink`（提供本地文件实现 + 对象存储实现的接口与 fake；异地写入的真实实现在 F-09/运维任务），并记录在 `audit.anchor` 表（只追加）；`auditctl anchor`。
6. 查询接口（/v1/admin/audit，权限 `aud` view）：按时间、操作人、动作、目标过滤，keyset 分页；详情 detail 对无权限字段脱敏；导出走异步（后续 L-09 同类机制，本任务先不做导出）。
7. 指标与告警：追加速率、链校验失败、锚定缺失（超过 24 小时未锚定告警）。
【验收标准】
① 只追加：UPDATE/DELETE/TRUNCATE 被拒（管理员与 audit_writer 两种身份各有测试）
② 哈希链连续：1000 并发追加后 Verify 通过；人为篡改任一行（用超级用户临时禁用触发器）Verify 能指出位置
③ 事件消费幂等：同一 event_id 重复投递只追加一条
④ detail 脱敏生效（塞入密码/令牌/手机号字段，库里已打码）
⑤ 每日锚定可运行；缺失告警；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 锚点异地位置（对象存储/不可变存储）等运维信息由用户提供；本任务只实现接口与本地实现
- 审计保留期限（默认永久）
【红线】审计表不得被应用角色修改；detail 不得含密钥/令牌/完整手机号；不得为性能放弃链连续性（可用 head 行锁串行化）。
```

---

## F-14 文件存储与预签名上传（新增任务）

> 原任务表遗漏：商品图、付款凭证、签收/验收/入库照片、证照、导入 CSV 都要文件服务，T-04/T-06/W-02/W-06/A-10 依赖它。

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】F-14 文件存储与预签名上传（新增任务）
【分支】task/F-14-file-storage
【依赖】F-02、F-10、S-01
【先阅读】
1. Dr.life+第一版交付形态与技术架构.md 第六章「文件存储」；Dr.life+技术方案与系统边界.md 第十章（安全）
2. Dr.life+用户协议与隐私政策草案(三语).md（照片、定位数据的保留）
【范围——要做】
1. `backend/internal/platform/storage/`：S3 兼容适配接口（PresignPut、PresignGet、Head、Delete、Copy）+ MinIO/本地文件系统测试实现（testcontainers 用 MinIO）+ 真实 S3 兼容实现（endpoint、桶、区域走配置，密钥走 F-10）；超时、重试、熔断。
2. 文件元数据表 `ops.file`（id、public_id、owner_account、purpose 枚举：product_image/payment_proof/delivery_proof/inbound_photo/licence/import_csv/…、object_key、content_type、size、sha256、status: pending/ready/rejected、created_at、retention_until）；对象键不含个人信息，随机化；**上传流程**：`POST /v1/{end}/files`（声明 purpose、content_type、size，服务端按 purpose 校验白名单类型与大小上限，返回预签名 PUT 链接，有效期 10 分钟，带 Content-Type 与 Content-Length 限制）→ 客户端直传 → `POST /v1/{end}/files/{id}/complete`（服务端 Head 校验大小/类型/校验和，状态 ready）；**不经 API 转发文件内容**。
3. 下载：业务模块通过接口 `storage.URLFor(ctx, fileID, viewer)` 取短期签名链接（默认 5 分钟，**访问控制由业务模块校验所有权后调用**，本包不自己决定谁能看）；私有桶，禁止公开读；商品图可走 CDN 公共前缀桶（purpose=product_image 且 status=ready 的单独只读策略，文档写明）。
4. 图片处理：上传后异步（batch 队列，S-03 未合并则同步最小实现）生成缩略图并剥离 EXIF（含 GPS）；限制像素与大小防止解压炸弹；恶意类型（SVG 脚本、可执行文件）拒绝；**病毒扫描留接口**（ScanHook，默认关闭，README 说明）。
5. 清理：status=pending 超过 24 小时的对象与元数据清理任务；retention_until 到期删除；删除要写审计事件。
6. 配额与限流：按账号每日上传次数与总容量上限（配置），超限 429；上传类路由归 S-02 的 heavy 类。
【验收标准】
① 预签名上传直传成功（MinIO 集成测试）；类型/大小不符被拒；complete 时校验和不符被拒
② 未完成的上传按期清理；EXIF/GPS 被剥离（测试用带 GPS 的样图）
③ 下载链接短期有效，过期不可用；业务侧未授权不能拿到链接（接口契约测试）
④ 对象键与日志不含个人信息；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 对象存储厂商/自建 MinIO（默认以 S3 兼容接口为准）
- 照片与证据的保留期限（默认 2 年，可配置）
【红线】不得让 API 进程转发大文件；不得公开读私有桶；不得把预签名链接写进日志。
```

---

## 评审要点（给我自己用）

| 任务 | 重点 |
|---|---|
| A-03 | 复用检测与宽限窗口是否会误杀；并发刷新无分叉；冻结后立即失效的测试是否真实；fail-closed 的路由分类；Redis 重建一致性 |
| A-04 | 事件与会话作废是否同事务/可恢复；step-up 是否真的挂在路由上；状态历史只追加；停用商户的级联 |
| A-05 | 邀请绑定的并发与一次性；phone_hash 是否带密钥；上限并发；权限缓存失效；越权矩阵测试；员工停用后的会话作废 |
| A-06 | 用户枚举（响应/耗时）；TOTP 重放；最后一位 super 的数据库兜底；Cookie 刷新 CSRF；step-up 单次性；矩阵逐格比对；脱敏位置 |
| A-07 | 换主原子性与唯一索引；令牌泄露面；webhook 验签与重放；至少保留一个 Bot 的并发；缓存失效 |
| A-08 | 是否真的只在同 person 内匹配；是否有任何数据归属变化；并发唯一性 |
| A-09 | 白名单外是否有漏拦；缓存失效；declined 状态；只追加触发器 |
| Q-01 | 长度前缀编码与规范化 JSON 是否真的无歧义；并发追加链连续；事件幂等；脱敏过滤器；篡改检测测试是否真的绕过触发器做的 |
