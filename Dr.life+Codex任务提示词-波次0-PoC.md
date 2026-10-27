# 波次 0：PoC 任务（P0-3、P0-4、P0-9）与需要人工/外部材料的 PoC

**用法：** 整块复制代码块内容粘贴给 Codex。PoC 的产出是**一份结论 + 可运行的最小代码**，放 `poc/`，**不进入生产目录**，结论回写《技术方案与系统边界》PoC 表。
**已完成：** P0-1（initData + Bot 池，代码已合并，**真机验收待你完成**）、P0-2（账务并发与幂等，已被 L-01/L-02 取代）。

### 需要你（人）提供材料的 PoC（不适合直接交给 Codex 写代码）

| PoC | 你要做/提供什么 | 做完后 |
|---|---|---|
| **P0-5 中通接口** | 向中通申请**测试账号与接口文档**（下单、轨迹、回调、费用、签收凭证字段、验签算法、回调来源 IP）；确认可用的节点与位置字段 | 把文档放到 `docs/vendor/zto/`（**不要提交密钥**），然后让我写 W-07 的真实对接提示词；PoC 的适配器接口与模拟实现已含在 W-07 里 |
| **P0-6 合作银行接口** | 向合作银行索取**接口文档与 3 份真实对账文件样例（脱敏）**，确认入金/冻结/出款/对账文件的触发方式与格式、回单格式 | 样例放 `docs/vendor/bank/`（脱敏）；我据此写 L-08 的专用解析器提示词；并更新《数据库》第十二章待确认 1、2 |
| **P0-7 WMS 选型** | 候选 WMS 清单与试用环境；明确**库存主从关系**（谁是库存之主） | 结论写进《技术方案》；W-01 的 `WarehouseSystem` 适配器按结论实现 |
| **P0-8 数字商品支付规则** | 查 Telegram 对 Mini App 内数字商品（AI 会员）的**官方支付规则**（附链接与日期），确认钱包扣款是否允许、是否必须用 Telegram Stars | 一页结论；N-06 在结论出来后才能开工 |
| **P0-1 真机验收** | 六个测试 Bot、真机 Mini App、`/revoke` 主 Bot，把证据回填 `poc/telegram-initdata/RESULT.md` | A-02 才能合并 |

---

## P0-3 Ollama 内网调用、AI 回退与高棉语评测

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】P0-3 Ollama 内网调用、AI 回退与高棉语评测
【分支】task/P0-3-ai-fallback-poc
【依赖】机柜服务器就绪（Ollama 所在的局域网服务器）；可先用本地 Ollama 容器做功能验证
【先阅读】
1. Dr.life+技术方案与系统边界.md 第十三（前置）章 PoC 表第 3 项；Dr.life+第一版交付形态与技术架构.md AI 网关章节；Dr.life+开发任务拆分与验收.md：P0-3、N-03
2. poc/README.md、poc/telegram-initdata/（PoC 目录风格）
【范围——要做】
1. `poc/ai-fallback/`（独立 Go module，不进入生产目录，**不得被 backend 引用**）：最小 OpenAI 兼容客户端，用同一份代码调用 DeepSeek、ChatGPT（OpenAI）、Ollama；配置（endpoint、model、密钥）全部来自环境变量或被 .gitignore 的 .env.local，**密钥不进代码、日志、结论文档**；超时（连接 3 秒、总 8 秒）、有限重试、按优先级回退链、最终回退本地规则（关键词模板，三语）。
2. 回退演示：①三个接口都可用；②断开 Ollama 网络（用 docker 网络断开或错误地址模拟）→ 超时后自动换下一个；③全部失败 → 本地规则；每个场景输出耗时分布（P50/P95）。
3. **高棉语评测**：构造 30 句真实高棉语装修/建材/仓储/配送问题（`testdata/khmer_questions.jsonl`，字段：id、问题、类别、期望要点；**样本须由高棉语母语者审核，缺审的整体标 TODO-KM-REVIEW**），对三个接口各跑一遍，保存回复到 `results/`（不含密钥与个人信息）；评分表（人工评分模板 csv：相关性、准确性、语言自然度、是否越权给金融/法律建议）与汇总脚本；**评分由人工完成，脚本只做汇总，不要用模型自评冒充人工评分**。
4. 测量 Ollama 经局域网的延迟与稳定性（并发 1/4/8 下的 P95、错误率、冷启动首 token 时间），GPU 型号与模型尺寸记录模板。
5. 结论 `poc/ai-fallback/RESULT.md`：回答 PoC 表『要回答的问题』——云 → 局域网延迟、稳定性、断线回退、高棉语效果；对 AI 优先级与部署位置的建议；待人工评分的部分明确标『待用户完成』。
【验收标准】
① 同一客户端调用三个接口；拔掉 Ollama 后超时自动回退；全部失败回退本地规则（三语）
② 30 句高棉语评测集、结果文件、评分模板与汇总脚本齐全（评分由人工填）
③ 密钥未进仓库/日志/结论；go vet、go test -race 通过；CI 全绿
【待确认（不要自己拍板，PR 里列出）】
- 真实 DeepSeek/ChatGPT 密钥与 Ollama 服务器由用户提供并本地运行（**没有密钥时只标『待用户验证』，不要伪造结果**）
【红线】不得提交密钥与真实用户数据；不得伪造评测或评分；PoC 代码不进 backend/。
```

---

## P0-4 高棉语搜索 PoC（PostgreSQL 全文 + pg_trgm + 同义词）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】P0-4 高棉语搜索 PoC（PostgreSQL 全文 + pg_trgm + 同义词）
【分支】task/P0-4-khmer-search-poc
【依赖】无（T-03 依赖其结论）
【先阅读】
1. Dr.life+技术方案与系统边界.md PoC 表第 4 项与第九章搜索；Dr.life+产品需求文档PRD.md 搜索（三语互搜：地砖 / tile / ក្បឿង）；Dr.life+开发任务拆分与验收.md：P0-4、T-03
2. Dr.life+数据库表结构与账务详细设计.md catalog.product / search_synonym 与第九章索引
【范围——要做】
1. `poc/khmer-search/`（独立目录，docker compose 起 PostgreSQL 16 + pg_trgm，**不依赖 backend 代码**）：造数脚本生成 **100 个商品**（三语名称，贴近真实建材：瓷砖、水泥、电线、涂料、卫浴、门窗等；高棉语名称须由母语者审核，缺审标 TODO-KM-REVIEW）与 **50 个查询**（中/英/高棉语、同义词、错别字、拼写变体、高棉语无空格长词、混合语言），并给出每个查询的**人工标注期望结果集**（`testdata/queries.jsonl`）。
2. 实现并比较至少三种方案：A) tsvector（simple）+ GIN；B) pg_trgm 相似度（trigram，含高棉文字符 n-gram）；C) A+B+同义词扩展（`search_synonym` 词组）+ 综合打分；可选 D) 简单的高棉语音节切分（如基于 Unicode 簇规则或开源切分库，**评估是否值得**）。说明高棉语没有词间空格对各方案的影响。
3. 评测脚本：对每个方案计算 **召回率（Recall@10）、首位命中率（Top-1）、MRR**，按查询类别（同语言、跨语言、错别字、长词）拆分；输出表格与 EXPLAIN（ANALYZE）耗时（1 万商品规模再测一次，造数脚本支持放大）；索引大小。
4. 结论 `poc/khmer-search/RESULT.md`：回答『第一版方案够不够、是否上 Meilisearch/OpenSearch』：给出明确推荐、数据支撑、阈值建议（供 T-03 的评测门禁使用，如召回 ≥ 90%、首位命中 ≥ 70%）、已知失败样本分析；**如果推荐需要专用搜索引擎，写清迁移触发条件**。
【验收标准】
① 100 商品 + 50 查询 + 人工标注期望集齐全；至少三种方案对比
② 给出召回率与首位命中率（分类别），以及 1 万商品规模的耗时
③ 结论明确：第一版 PostgreSQL 方案是否足够，并给出 T-03 门禁阈值
【待确认（不要自己拍板，PR 里列出）】
- 高棉语样本需母语者审核；期望集标注质量决定结论可信度
【红线】PoC 代码不进 backend/；不得用同一批数据调参后再宣称评测通过（保留一份盲测查询集，PR 里说明）。
```

---

## P0-9 司机端弱网与定位 PoC（Mini App）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成，并在 backend 目录运行 npx --yes openapi-typescript@7.13.0 api/openapi.yaml --output ../packages/api-client/src/schema.d.ts 重新生成前端类型一并提交（否则 CI openapi-check 失败）；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；推送前必须在本地跑完 go vet、全量测试与集成测试（本机 Docker）并把结果写进 PR 描述；不要靠 CI 反复试，一个分支未本地通过前不要连续推送（私有仓库 Actions 月额度有限，已有被拦记录）；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】P0-9 司机端弱网与定位 PoC（Mini App）
【分支】task/P0-9-driver-offline-poc
【依赖】无（W-08、U-07 依赖其结论）；真机与 Telegram 测试 Bot 由用户提供
【先阅读】
1. Dr.life+技术方案与系统边界.md PoC 表第 9 项；Dr.life+交互文档.md 第四章（司机端弱网：状态更新先本地暂存、恢复后补传；地图瓦片不可用降级列表）；Dr.life+开发任务拆分与验收.md：P0-9、W-08、U-07
2. poc/telegram-initdata/README.md（如何用测试 Bot + HTTPS 隧道在真机上打开 Mini App）
【范围——要做】
1. `poc/driver-offline/`：最小 Mini App 页面（React/Vite 或原生，单页）+ 最小 Go 后端：①前台持续定位上报（Telegram Mini App 内 `navigator.geolocation` 与 Telegram 的 `LocationManager`（如可用）两种方式，各自的权限提示与失败表现）；②**离线暂存**：IndexedDB 队列，每条带 `client_event_id`（UUID）与单调序号，网络恢复后按序补传；③服务端按 `client_event_id` 去重（`/poc/location/batch`）、统计丢失/重复/乱序；④任务状态更新（接单/取货/送达）同样走队列；⑤『断网 5 分钟』场景脚本（Chrome DevTools offline + 真机飞行模式两种）与自动化统计页。
2. 测量：前台持续定位的真实表现（采样间隔、精度、耗电粗估、页面切到后台后的行为——**Mini App 在后台通常暂停，记录事实**）、iOS/Android 差异、权限被拒的处理；补传成功率、重复数、乱序数、服务端限流下的补传速度；地图瓦片不可用时的降级（列表）演示。
3. 结论 `poc/driver-offline/RESULT.md`：回答『前台持续定位的真实表现；断网 5 分钟后操作恢复是否全部补传且不重复；是否需要提前启动司机原生 App』，给出明确建议与依据；真机数据标注设备型号与系统版本；**拿不到真机数据的项标『待用户验证』并写步骤，不要伪造**。
【验收标准】
① 断网 5 分钟内的位置与状态更新，恢复后全部补传、无重复（自动化统计页证明）
② 前台持续定位表现与后台行为有真机记录（或明确标『待用户验证』）
③ 结论包含是否需要提前启动司机 App 的建议
【待确认（不要自己拍板，PR 里列出）】
- 真机、Telegram 测试 Bot、HTTPS 隧道由用户提供
【红线】PoC 不进生产目录；位置数据只用测试账号；不得伪造真机结果。
```

---
