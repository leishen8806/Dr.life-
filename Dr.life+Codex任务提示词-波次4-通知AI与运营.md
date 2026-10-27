# 波次 4：通知、AI 与运营（N-01 至 N-10）

**用法：** 整块复制代码块内容粘贴给 Codex，每个任务一个独立 worktree 与分支。
**外部阻塞：** N-03 依赖 P0-3；N-06 **必须等 P0-8 结论**；N-01 的短信兜底只留接口。N-10（集成配置）被多个任务引用，建议早开。

---

## N-01 通知中心（事件 → 规则 → 偏好 → 渠道，站内信，SSE）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】N-01 通知中心（事件 → 规则 → 偏好 → 渠道，站内信，SSE）
【分支】task/N-01-notification-center
【依赖】S-03（队列与 Relay）、S-04（Telegram 发送）、A-07（Bot 绑定与可达性）、A-04（账户语言）
【先阅读】
1. Dr.life+消息通知、渠道关联、埋点与遗漏清单.md 第一、二章（统一通知中心、优先级 P0/P1/P2、各端渠道）与第六章已确认决定
2. Dr.life+数据库表结构与账务详细设计.md 第八章 comms（message、delivery_log、template）；Dr.life+API安全协议与容量及队列设计.md 6.7、6.2、5.3（长连接与并发上限）
3. Dr.life+开发任务拆分与验收.md：N-01、N-02；backend/internal/platform/queue/、comms/telegram/
【范围——要做】
1. 迁移（`comms` schema）：message（public_id、to_account、event、priority P0/P1/P2、params jsonb、dedupe_key 唯一、read_at、created_at）、delivery_log（message_id、channel inapp/telegram/sms/push、bot_id、ok、why 枚举、attempt、at）、notification_pref（account_id、channel 开关、marketing_consent、quiet_hours）、template 由 N-02 管。**站内信始终保留**（message 即站内信，永不因渠道失败而丢）。
2. 管线：业务事件（来自各模块 outbox，经 S-03 Relay 路由到 `comms:event:v1` 消费者，**幂等**：dedupe_key = 事件号+接收人+事件类型）→ 规则表（事件类型 → 接收人解析（用接口 `RecipientResolver`，不跨 schema 读）、模板、优先级）→ 偏好与同意过滤（**P2 营销默认关闭，需主动同意；P0 不可关闭**）→ 渠道选择：P0 = 站内信 + 至少一个外部渠道（Telegram 优先；全不可达 → **短信兜底接口 `SmsSender`（只留接口 + fake，不实现真实短信）**）；P1 = 站内信 + 用户开启的外部渠道；免打扰 22:00–07:00（时区配置）只发 P0，其余延迟到早上（`ProcessIn`）。
3. Telegram 渠道：调用 S-04 的发送任务，按用户**已绑定且可用**的 Bot 发送，主不可用换备用（S-04 实现故障转移，这里只传递用户与事件）；用户语言取账户登记语言（A-04 的 GetAccountLanguage）。
4. 站内信接口：/v1/{buyer|merchant|driver}/messages（列表分页、未读数、标记已读、全部已读）、/v1/admin/notifications（发送记录：每条消息各渠道结果与原因，`ops` 权限 view，脱敏）；偏好接口 /v1/{end}/me/notification-prefs。
5. **实时通道 SSE**：`GET /v1/{end}/events`（Server-Sent Events，Bearer 鉴权；心跳 15 秒；单账号并发连接上限 3；服务端 5 分钟强制重连以刷新令牌；Last-Event-ID 断线续传最近 N 条）。**注意：SSE 是长连接，必须从 S-02 的 Bulkhead 请求截止时间中豁免，并单独设并发上限与内存预算（每连接缓冲上限，慢客户端被断开）**；推送内容只含事件类型与标识，客户端再拉取详情。后台 Web 同样用 SSE 接收待办与告警（/v1/admin/events、/v1/wms/events）。
6. 司机 App 的华为/FCM 推送：只定义 `PushSender` 接口、渠道枚举 push 与 fake，**不实现**（未来 App 规划文档）。
7. 发送记录查询、重试与去重：失败自动重试由 S-04/S-03 负责；本模块记录每次结果；同一事件重复投递不重复发送。
【验收标准】
① 事件→规则→偏好→渠道全链路；P2 默认关闭、P0 不可关闭；免打扰时段只发 P0
② 站内信始终保留；Telegram 不可达时 P0 走短信兜底接口（fake 断言被调用）
③ 同一事件重复投递只产生一条消息、一次外部发送（幂等）
④ SSE：鉴权、心跳、并发上限、断线续传；慢客户端不拖垮服务（测试）；不受 Bulkhead 超时误杀
⑤ 消息按账户语言渲染；日志不含消息正文；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 短信渠道服务商（只留接口）
- 免打扰时区（默认 Asia/Phnom_Penh）
- SSE 与 Cloudflare Tunnel 的长连接超时（需运维验证）
【红线】不得在日志/指标里放消息正文或收件人手机号；营销消息不得绕过同意；不得跨 schema 读取收件人资料。
```

---

## N-02 通知模板与深链

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】N-02 通知模板与深链
【分支】task/N-02-templates-deeplinks
【依赖】N-01、A-06、N-07 的三语发布规则可参照
【先阅读】
1. Dr.life+消息通知、渠道关联、埋点与遗漏清单.md 第二章（Telegram：startapp 深链）；Dr.life+Bot资料与高棉语欢迎语.md；Dr.life+交互文档.md 第一章 6（通知与深链）
2. Dr.life+数据库表结构与账务详细设计.md comms.template；Dr.life+开发任务拆分与验收.md：N-02
【范围——要做】
1. 模板表 `comms.template`（event、channel、text jsonb 三语、version、status draft/published、published_by/at；已发布版本只增不改）：**三语齐全才能发布**（数据库 CHECK + Go），高棉语草案带 TODO-KM-REVIEW 标记字段；变量白名单（如 {order_no}、{amount}），渲染时**转义**（防止注入链接/Markdown），缺变量用安全默认；金额格式化按语言（USD + 可选 KHR）。
2. 后台模板管理（/v1/admin/templates，`ops` 权限 edit，写审计）：草稿、预览（用示例变量渲染三语）、发布、历史；P0 资金类模板修改需要 step-up 与二人复核（提交人≠发布人）。
3. **深链**：`https://t.me/<bot>?startapp=<payload>`；payload 为 `{role}_{target}_{id}` 的短码（长度与字符集受 Telegram 限制），**服务端生成并签名（HMAC，防篡改）**；打开的 Mini App 必须与**接收人角色匹配**（买家通知打开买家 Mini App、商家通知打开商家 Mini App；用对应端的主可用 Bot 链接）；前端收到 start_param 后调用 `GET /v1/{end}/deeplink/resolve` 校验并返回目标页面路由，**越权或过期的深链回落到首页**，不泄露目标资源是否存在。
4. 提供 N-01 使用的 `Render(event, lang, params)` 与 `BuildDeepLink(event, recipient)`；模板与深链的单元测试覆盖三语与全部事件类型（10 类事件模板：下单、付款凭证、发货、到货、签收、提现到账、冻结、审核结果、入库完成、新配送任务）。
【验收标准】
① 模板三语齐全才能发布；已发布版本不可改，只能发新版本
② 买家通知打开买家 Mini App，商家通知打开商家 Mini App（角色匹配测试）；篡改/过期/越权深链回落首页
③ 变量转义生效（注入测试）；P0 模板修改需要 step-up 与二人复核
④ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 10 类事件模板的高棉语文案需母语审校（本任务只放草案与标记）
【红线】不得把可点击的未校验外链放进模板变量；深链不得携带个人信息。
```

---

## N-03 AI 网关（多提供商、优先级与回退、预算、脱敏）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】N-03 AI 网关（多提供商、优先级与回退、预算、脱敏）
【分支】task/N-03-ai-gateway
【依赖】P0-3 结论、F-10、S-02、S-03、N-10（提供商配置）
【先阅读】
1. Dr.life+第一版交付形态与技术架构.md AI 网关章节（统一 OpenAI 兼容协议；DeepSeek、ChatGPT、Ollama 局域网；按优先级回退，最终回退本地规则）；Dr.life+API安全协议与容量及队列设计.md 6.8；AGENTS.md 规则 17
2. Dr.life+数据库表结构与账务详细设计.md ai.provider / call_log；poc/ 下 P0-3 结论；Dr.life+开发任务拆分与验收.md：N-03、P0-3
【范围——要做】
1. `backend/internal/ai/gateway/`：统一接口 `Chat(ctx, Request) (Response, error)`，适配器 = OpenAI 兼容 HTTP（DeepSeek、ChatGPT、Ollama 同一实现，差异在 endpoint/model/headers 配置）；优先级链按 ai.provider.priority；每个提供商：**超时、重试（仅幂等失败）、并发上限（信号量）、熔断（连续失败暂停再半开）**；同步对话总截止 8 秒，超时/失败自动换下一个；**全部失败回退本地规则引擎 `LocalRules`**（关键词→模板化回复，三语，不调用任何外部服务）。
2. 迁移：`ai.provider`（name、priority、status、endpoint、model、key_enc、monthly_budget_e4、enabled）、`ai.call_log`（account 匿名化引用、provider、ok、ms、tokens_in/out、failed_providers[]、created_at；**按月分区，保留 90 天**）、`ai.usage_budget`（provider、month、spent_e4）；预算按 token 计价估算（价目表配置），**达到月预算自动跳过该提供商**并告警。
3. **发送前脱敏**（`Redactor`）：手机号、邮箱、银行账号/卡号、证件号、详细地址模式、Telegram 用户名替换为占位符；结构化字段（姓名等）不进入提示；有单元测试与『黑名单样本』测试；**发出的请求体在测试里被捕获并断言不含原始敏感串**。
4. 不给金融/法律建议：系统提示词模板固定声明范围，回复追加『仅供参考』标注（三语）；提示词模板与版本放配置，不拼接用户可控的系统指令（防提示注入：用户内容放 user 角色，长度上限，控制字符清理）。
5. 管理接口（/v1/admin/ai/providers，**仅 super**，step-up，审计）：增删改提供商、调优先级、启停、设预算；密钥信封加密、只显示末 4 位；『测试调用』接口（固定无害提示，限频）；统计接口（各提供商成功率、延迟、回退次数、花费，供 Grafana 与后台）。
6. 指标：回退率（按层级）、各提供商延迟与失败、熔断状态、预算使用；告警（回退率高、全部提供商失败）。
7. Ollama 在局域网：访问地址走配置，超时短（默认 3 秒连接）；不可达时静默回退下一个。
【验收标准】
① 三个接口用同一适配器调用；拔掉 Ollama（模拟不可达）→ 超时后自动换下一个；全部失败回退本地规则（三语回复）
② 发送前脱敏有效（被捕获的请求体不含手机号/卡号/邮箱等样本）；提示注入样本不改变系统指令
③ 预算用尽自动跳过并告警；熔断与恢复有测试；日志不含用户消息正文与密钥
④ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- ChatGPT/DeepSeek 的具体模型与价目（运营配置）
- Ollama 的高棉语效果评测（P0-3 的 30 句评测由人工执行并填表）
【红线】不得把未脱敏内容发给外部 AI；不得记录完整提示与回复；不得让 AI 回复直接触发资金动作。
```

---

## N-04 AI 聊天与额度、会员状态

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】N-04 AI 聊天与额度、会员状态
【分支】task/N-04-ai-chat-quota
【依赖】N-03、A-01、T-01/T-02（推荐商品）、F-10（消息加密）
【先阅读】
1. Dr.life+产品需求文档PRD.md AI 顾问与业务规则（AI 聊天：只推荐在售商品、不给金融法律建议、标注仅供参考、每条消息 1 次额度、按自然月重置）；Dr.life+交互文档.md 买家端 AI 聊天（『记住的你』默认收起）；prototype/index.html AI 聊天
2. Dr.life+数据库表结构与账务详细设计.md ai.conversation/message/usage/membership；Dr.life+开发任务拆分与验收.md：N-04、N-05、N-06
【范围——要做】
1. 迁移：`ai.conversation`、`ai.message`（**正文字段信封加密**，AAD=`ai_msg:<id>`，密文 bytea；摘要/标签不含原文）、`ai.usage`（account、month、used，**唯一 (account, month)**）、`ai.membership`（account、until）。
2. 额度：每条用户消息消耗 1 次，按自然月（Asia/Phnom_Penh）重置；免费额度与会员额度走配置（默认 20/30、500 为占位，待确认）；**原子扣减**（`UPDATE … SET used = used + 1 WHERE used < limit RETURNING`），并发不超额；额度用尽返回 **开通会员所需信息**（价格、每月次数、有效期，不含余额——余额由钱包接口另取）与专用错误码，不调用 AI。
3. 聊天：`POST /v1/{buyer|merchant}/ai/chat`（SSE 或普通响应，route 类 heavy；Idempotency-Key 防重复扣额度）、会话列表/详情/删除（用户可删除自己的会话，物理删除密文）；上下文窗口截断；回复标注『仅供参考』；系统提示限定不给金融/法律建议。
4. **推荐商品**：只推荐平台在售（status=on 且商户可见）、**标准分非 C 级**（`IsRecommendable`）、**不含广告**的商品，通过 catalog 的只读接口；推荐结果以结构化卡片返回（商品 public_id、名称、价格），不让模型自由生成商品信息；模型输出里的商品引用必须在白名单内校验，不在的丢弃（防幻觉）。
5. 保留期：消息原文按隐私政策期限（配置，默认 90 天）自动清理任务（S-03 batch），清理后只留元数据；画像（N-05）不含原文。
6. 隐私：开始使用 AI 前需要用户已同意（A-09）；日志不含正文；管理员无法查看用户聊天原文（后台只有统计）。
7. 后台只读：额度使用统计、会员数、回退率（来自 N-03）。
【验收标准】
① 额度用尽返回开通会员所需信息且不调用 AI；并发 100 次请求不超额；Idempotency-Key 重复不重复扣
② 不给金融/法律建议（系统提示与测试样本）；回复含『仅供参考』
③ 只推荐在售且非 C 级、不含广告的商品；模型虚构的商品被丢弃（测试）
④ 消息正文加密存储（库里查不到明文）；到期清理有效；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 免费额度数值（20/30）与会员额度/价格（占位，待用户确认）
- 原文保留天数
【红线】不得明文存储聊天内容；不得让管理员读取聊天原文；推荐不得包含广告商品。
```

---

## N-05 基础信息与标签（用户画像）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】N-05 基础信息与标签（用户画像）
【分支】task/N-05-profile-tags
【依赖】N-04、T-03（搜索/浏览事件，可后接）
【先阅读】
1. Dr.life+产品需求文档PRD.md「标准与画像」与 AI 顾问（基础信息与喜欢标签；用户可确认/修改/删除；AI 推断不覆盖已确认值，只给出『是否改为…』建议）；Dr.life+消息通知、渠道关联、埋点与遗漏清单.md
2. Dr.life+数据库表结构与账务详细设计.md ai.basic_info / ai.tag；Dr.life+用户协议与隐私政策草案(三语).md（画像、可删除）；Dr.life+开发任务拆分与验收.md：N-05
【范围——要做】
1. 迁移 `ai.basic_info`（account、key（称呼/城市/房屋类型/面积/卧室数/家庭人数/预算/计划装修时间）、value、status ai/ok、src account/ai/user、suggested（AI 建议值，用户未确认）、updated_at）、`ai.tag`（account、key、weight、src jsonb、at、expires_at）；**不含聊天原文**。
2. AI 记录：聊天后异步（`ai` 队列）提取基础信息与标签（N-03 网关 + 结构化输出校验）；**已被用户确认（status=ok）的值不会被 AI 覆盖**，只产生 `suggested` 并提示『是否改为…？』；缺失项由聊天自然追问（提供 `MissingBasicInfo(account)` 给 N-04 的系统提示）。
3. 标签体系：兴趣标签（品类/风格/颜色/材质/产地/价位）权重与有效期（90/180 天，到期衰减/清理任务）、规则标签（等级、交易额段、会员、活跃度——由其他模块事件计算）；来源：AI 聊天、搜索、浏览、下单。
4. 用户权利接口：`GET /v1/{end}/me/profile`（查看全部基础信息与标签及来源）、`PUT /me/profile/basic/{key}`（确认/修改）、`DELETE` 单项与『全部清除』、导出我的数据；删除后异步清理派生数据并写审计事件；**『AI 记住的你』默认收起**由前端处理，接口返回摘要行。
5. 后台只读画像（`usr` 权限 view）：全部账户一览（城市、关键信息、主要喜欢、信息完整度、更新时间）、按账户查看来源标注；**无 `sens` 权限时脱敏**；匿名编号用于分析，不暴露聊天原文。
6. 广告定向使用画像必须另取得用户同意（A-09 增加 kind=ads_profiling 的占位，默认拒绝），本任务只保证接口 `CanUseForAds(account)` 恒返回 false 直到同意存在。
【验收标准】
① AI 推断不会覆盖已确认值，只给出建议；用户可查看/确认/修改/删除，删除生效且派生数据被清理
② 标签有权重与有效期，到期清理；画像不含聊天原文
③ 后台画像按权限脱敏；CanUseForAds 默认 false
④ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 标签有效期取 90 还是 180 天（PRD 两个值，默认兴趣 90 天、经营关注 180 天）
【红线】不得保存聊天原文到画像；不得在无同意时用于广告；删除请求必须可验证完成。
```

---

## N-06 AI 会员费入账

> ⛔ P0-8 出结论前不要开工。结论可能是：A) 允许钱包扣款；B) 必须用 Telegram Stars；C) 需外部支付入金 + 记账两步。下面按 A 写，B/C 需要改本任务。

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】N-06 AI 会员费入账
【分支】task/N-06-ai-membership-fee
【依赖】P0-8 结论（Telegram 数字商品支付规则）、L-03（MemberFee）、N-04
【先阅读】
1. P0-8 的结论文档（附官方规则链接与日期）；Dr.life+数据库表结构与账务详细设计.md 第六章 6.15、第十二章待确认 3
2. backend/internal/ledger/doc.go（MemberFee 前置条件与幂等键 `member_fee:member:<id>:<period>`）；Dr.life+交互文档.md 买家端『开通会员』弹窗；Dr.life+开发任务拆分与验收.md：N-06
【范围——要做】
1. （方案 A）`POST /v1/{buyer|merchant}/ai/membership`：Idempotency-Key + step-up（扣款类动作）；检查可用余额 ≥ 会员价；调用 `ledger.MemberFee`（幂等键含周期）→ 成功后在**同一业务事务/可恢复流程**内写 `ai.membership`（until 延长）与额度提升；余额不足返回可操作错误（含『去充值』所需信息），**不能开通**；会员到期不自动续费（第一版）。
2. 崩溃恢复：ledger 成功而 membership 未写 → 恢复任务按 `member_fee` 交易补写会员状态（不重复扣款）；必须有崩溃注入测试。
3. 弹窗所需信息接口：价格、每月次数、有效期、当前可用余额；
4. 入账后计入平台收入（category=member，ledger 已处理）；会员费记录出现在用户资金流水。
5. 标注：代码与 PR 说明中写明『受 P0-8 结论约束』，结论变化时的改动点（方案 B/C）列表。
【验收标准】
① 余额不足不能开通；开通后额度提升且入账（流水可查）
② 重复购买同一周期幂等；崩溃后恢复一致
③ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- P0-8 结论（必须先有）
- 会员价格与周期（占位 4.99 美元/30 天）
【红线】未取得 P0-8 结论不得合并；不得绕过 ledger.MemberFee。
```

---

## N-07 协议与政策管理（三语发布、版本、同意覆盖率）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】N-07 协议与政策管理（三语发布、版本、同意覆盖率）
【分支】task/N-07-legal-docs
【依赖】A-09、A-06、F-02
【先阅读】
1. Dr.life+用户协议与隐私政策草案(三语).md；Dr.life+产品需求文档PRD.md（协议与政策管理）；Dr.life+第一版交付形态与技术架构.md 官网（内容来自后台已发布协议，发布时重新生成）
2. Dr.life+数据库表结构与账务详细设计.md ops.legal_doc / consent；Dr.life+开发任务拆分与验收.md：N-07、A-09、U-10
【范围——要做】
1. 迁移 `ops.legal_doc`（kind terms/privacy/…、version、effective_at、zh/en/km 正文（markdown）、note（变更说明）、status draft/published、published_by/at；**已发布版本只增不改**，触发器拒绝 UPDATE/DELETE；同一 kind 同一时刻只有一个『当前生效』版本；生效日期不得早于发布时间 + 配置的提前通知期？列待确认）。
2. 后台（/v1/admin/legal，`cfg`/`ops` 权限，发布需 step-up + 审计）：草稿创建/编辑、**三语预览**、发布（**三语齐全 + 变更说明 + 生效日期**，缺一不可发布；Markdown 白名单渲染与 XSS 清洗）、历史版本查看、与上一版的差异对比；法律文本为草案，发布前系统提示『需律师审核』的确认勾选并记录。
3. 与 A-09 对接：实现 `LegalVersionProvider`（最新生效版本），发布/生效时主动失效同意状态缓存，用户下次请求返回需重新同意；同意覆盖率统计接口（各版本已同意/未同意/拒绝账号数，按端与角色）。
4. 公开读取：`GET /v1/public/legal/{kind}`（三语，仅已发布且已生效版本，可缓存，带 ETag），供官网（U-10）与 Mini App 展示；`/v1/public/legal/{kind}/versions` 历史列表。
5. 官网重建钩子：发布事件通过 outbox 触发 webhook/队列任务通知官网构建（目标地址配置，N-10），失败重试告警。
【验收标准】
① 三语不齐/缺变更说明/缺生效日期不能发布；已发布版本不可改，只能发新版本
② 发布新版本后用户下次打开被要求重新同意（与 A-09 联测）；覆盖率统计正确
③ 公开接口只返回已发布且已生效版本；Markdown 清洗防 XSS（测试）
④ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 生效日期与发布时间的最小间隔（提前通知期）
- 法律文本是否必须律师审核后才能发布（建议是，代码里做确认勾选）
【红线】不得修改已发布版本；不得发布不齐三语的文本。
```

---

## N-08 广告管理

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】N-08 广告管理
【分支】task/N-08-ads
【依赖】T-01、L-03（AdCharge/AdSpend/AdRefund）、A-06、S-03
【先阅读】
1. Dr.life+消息通知、渠道关联、埋点与遗漏清单.md 第六章决定 5；Dr.life+产品需求文档PRD.md 运营中心『广告管理』（固定价位广告位 + 人工审核，不做竞价）与搜索广告位标注
2. Dr.life+数据库表结构与账务详细设计.md ops.ad_slot/ad_campaign 与第六章 6.12–6.14；backend/internal/ledger/doc.go
3. Dr.life+开发任务拆分与验收.md：N-08、T-03
【范围——要做】
1. 迁移：`ops.ad_slot`（place、on、model、price_e4 默认价）、`ops.ad_campaign`（merchant_id、place、kind、ref（商品/店铺）、model（按天/按曝光/按点击，固定价）、price_e4、budget_e4、spent_e4、imp、clk、status draft/pending/approved/running/paused/rejected/ended、reviewed_by、…）；**spent_e4 ≤ budget_e4（CHECK）**。
2. 商家端（`ads` 权限）：创建广告（选择广告位与对象）→ 提交审核；创建时**预算划扣**：`ledger.AdCharge`（余额不足不能创建，免费演示广告不入账）；暂停/恢复；终止或被拒时 `ledger.AdRefund` 退回剩余预算；统计只看自己的。
3. 后台（`ops` 权限 + 审计）：广告位开关与默认价格、投放审核（通过/驳回需原因）、暂停/恢复/终止；**广告位关闭 → 不展示且不扣费**（展示服务读取开关，关闭的位置返回空）。
4. **消耗入账**：展示/点击事件异步聚合（S-03 队列），**按广告、按小时（或按天）合并成一条 `ledger.AdSpend`**，合并键 = `ad_spend:ad:<id>:<bucket>`，幂等；预算耗尽自动停投；避免每次点击一条流水。
5. 展示接口：为搜索（T-03 的 `AdProvider`）与首页提供广告候选：**一律带 is_ad=true 与『广告』标注**，**不影响自然排序**；黄页无付费排名（承诺）：不得出现付费提升自然结果的路径（测试扫描）。
6. 统计：曝光、点击、花费按商户与广告位汇总（后台与商家各自权限范围）。
【验收标准】
① 关闭的广告位不展示且不扣费；预算划扣与退回通过 ledger，流水可查
② 消耗按小时合并一条 AdSpend，合并键保证幂等；预算不超（并发点击测试）
③ 广告标注『广告』且不影响自然排序（T-03 联测）
④ 余额不足不能创建广告；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 广告位清单与默认价格
- 计费模型（按天/按曝光/按点击）第一版支持哪些
【红线】不得绕过 ledger 的广告业务方法；不得出现竞价；不得让广告改变自然排序。
```

---

## N-09 渠道、上下游关系与埋点

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】N-09 渠道、上下游关系与埋点
【分支】task/N-09-channels-relations-events
【依赖】A-02（start_param）、A-06、S-03、F-02
【先阅读】
1. Dr.life+消息通知、渠道关联、埋点与遗漏清单.md 第三、四章（渠道来源、上下游关联、关系拓扑、自建埋点设计与治理）；Dr.life+数据库表结构与账务详细设计.md ops.channel_acq/relation/event、第九章（event 按天分区保留约 13 个月）
2. Dr.life+开发任务拆分与验收.md：N-09；Dr.life+用户协议与隐私政策草案(三语).md（埋点同意）
【范围——要做】
1. 迁移：`ops.channel_acq`（account、channel、by（邀请人）、campaign、invite_code、first/last 来源）、`ops.relation`（up、down、type 采购/服务/配送/供货/邀请/合作、src 自动/人工、at；外部供应商可无账户）、`ops.event`（ts、ev、anon_id、role、lang、sess、page、client(tg/h5/app/pc)、app_version、channel、props jsonb；**按天分区，保留 13 个月（可配置），到期 DROP PARTITION 维护任务**）。
2. 渠道采集：登录时（A-02 解析的 start_param）记录渠道码/邀请码/地推员码；首次与最近来源；邀请关系自动生成 relation(邀请)；人工登记渠道与外部供应商（后台，`ops` 权限，审计；外部供应商是否核验为待确认）。
3. 上下游关系自动生成：订单（采购）、分批结算（服务）、签收（配送）、导入（供货）通过各模块事件（幂等消费者）写 relation；去重 (up, down, type) 并记录最近时间。
4. **埋点接收** `POST /v1/{end}/events`（匿名设备编号，登录后关联账户匿名编号；批量上报、每批上限、速率限制）：服务端**白名单校验**事件名与 props 键（埋点字典，新增事件要登记；props 中出现手机号/邮箱/地址模式直接丢弃并计数）、**不采集个人信息、金额只在服务端业务事件里记录**；尊重用户同意（未同意统计类事件不记录——开关来自 A-09/偏好）；失败不影响用户体验（快速 202）。服务端业务事件（下单、付款凭证、出库、签收、提现）由各模块通过 `events.Emit` 写入，交易类指标以服务端事件为准。
5. 后台只读（`ops` 权限）：渠道转化（账户数、有交易账户数、转化率）、关系拓扑数据（分层：外部供应商 → 商户 → 司机 → 买家，按关系类型过滤，点击节点返回其上下游）、漏斗（浏览商品 → 加购 → 下单 → 付款凭证 → 验收，按会话去重）、埋点字典、最近事件、导出 CSV（异步）。
6. 写入走队列批量（S-03），事件表写入失败不阻塞业务。
【验收标准】
① 事件不含个人信息（props 白名单与模式过滤有测试，塞入手机号被拒）
② 事件表按天分区，到期自动 DROP；渠道与邀请关系自动采集；拓扑数据与漏斗口径正确
③ 未同意不采集统计事件；批量上报与限流有效
④ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 人工登记的外部供应商是否需要核验（文档待定）
- 埋点保留期 13 个月（合规意见为准）
【红线】不得记录手机号/地址/聊天原文；不得用事件表做账务依据。
```

---

## N-10 集成配置与系统设置（含汇率）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】N-10 集成配置与系统设置（含汇率）
【分支】task/N-10-integrations-settings
【依赖】F-10、A-06、Q-01
【先阅读】
1. Dr.life+平台后台权限与审计规范.md（集成管理：仅超管可录入/更换、末 4 位、测试、每次变更写审计）；Dr.life+第一版交付形态与技术架构.md 第七章『密钥与令牌保管』；Dr.life+交互文档.md 平台『接口状态/集成』
2. Dr.life+数据库表结构与账务详细设计.md ops.integration / setting / fx_rate 与第十二章待确认 4（汇率来源）；Dr.life+开发任务拆分与验收.md：N-10
【范围——要做】
1. 迁移 `ops.integration`（id、group 地图/中通/银行/WMS/AI/Telegram/短信/存储/官网构建…、provider、env test/staging/prod、endpoint、key_enc（信封加密，AAD=`integration:<id>`）、key_last4、status、enabled、last_test_at/result）、`ops.setting`（key、value jsonb、updated_by、updated_at；**键白名单与类型校验**，变更写审计含前后值）、`ops.fx_rate`（base、quote、rate numeric 或整数 ppm、effective_from、created_by；**只增不改**）。
2. 后台（/v1/admin/integrations*，`integ` 模块，**仅 super 可录入/更换密钥**，step-up，审计）：列表（只显示『已设置 + 末 4 位』）、新增/更换密钥（旧值不可读回）、启停、环境隔离（**test/staging/prod 的集成项互相独立，生产环境不得加载测试密钥**——启动校验 APP_ENV 与 integration.env 一致）、**测试调用**（经各适配器的 `Ping`，限频、结果只记成功/失败与耗时，不记响应内容）、最近调用状态。
3. 提供给其他模块的读取接口 `integrations.Get(ctx, group, provider) (Config, error)`：**密钥解密只在调用点内存中**，缓存带 TTL 与主动失效；日志与错误不含密钥；未启用/缺失时返回明确错误供适配器降级。
4. 设置：`setting` 的读写接口与类型化访问器（费率、阈值、额度、开关等；其他任务引用的配置键在 README 列表登记）；变更即时生效（缓存失效）。
5. 汇率：手工维护（后台新增生效汇率，**瑞尔显示汇率来源为待确认，先手工**）；`FxProvider` 接口给 W-03/T-04 使用：取某时刻有效汇率，返回汇率引用编号以便固化；USD 为准、KHR 为显示。
【验收标准】
① 密钥信封加密，页面与接口只显示『已设置 + 末 4 位』；仅 super 可录入/更换；每次变更写审计（不含密钥）
② 环境隔离：test/prod 配置互不加载；测试调用限频且不记录响应内容
③ 汇率只增不改，按时间取有效值，引用编号可固化
④ 日志与错误不含密钥（子串断言）；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- KHR 汇率来源（手工 / 央行牌价 / 银行牌价）
【红线】密钥不得读回明文；不得在非生产环境保存生产密钥；设置变更必须审计。
```

---

## 评审要点（给我自己用）

| 任务 | 重点 |
|---|---|
| N-01 | 幂等去重；P0/P2 规则；SSE 资源与豁免；语言选择；偏好与同意 |
