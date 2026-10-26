# 波次 2：商品与交易（T-01 至 T-09）

**用法：** 整块复制代码块内容粘贴给 Codex，每个任务一个独立 worktree 与分支。
**关键依赖：** T-01 先行；T-03 依赖 P0-4 结论；T-05 依赖 A-04；T-08 依赖 L-04（已合并）；T-09 的赔付资金流向有一个**待你拍板的账务缺口**（见 T-09 说明）。
**跨模块规则：** catalog/trade 不跨 schema 读 iam/wms/ledger，使用事件投影与只读接口。

---

## T-01 catalog 表与接口（分类、商品、阶梯价）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】T-01 catalog 表与接口（分类、商品、阶梯价）
【分支】task/T-01-catalog
【依赖】F-03、F-04、A-02、A-04（商户状态事件）、A-10（商户核验状态）、F-14（商品图）、S-01
【先阅读】
1. Dr.life+数据库表结构与账务详细设计.md 第三章 catalog、第九章（索引与分区）；Dr.life+产品需求文档PRD.md 商家章节（商品管理）与买家章节（商城）
2. Dr.life+技术方案与系统边界.md 第三、四章（模块边界：catalog 不改库存数量）、第六章；Dr.life+交互文档.md 商家端「商品」、买家端「商城」
3. Dr.life+开发任务拆分与验收.md：T-01、T-02、T-03、T-04；backend/internal/iam/ 的 RequireMerchantPerm（A-05）
【范围——要做】
1. 迁移（`catalog` schema，角色 `catalog_rw`）：category（code 唯一、name jsonb 三语、enabled、default_zone、sort、is_custom）、product（public_id、merchant_id、category_id、name jsonb、spec jsonb、unit、origin cn/lc、lead_days、vol_m3 numeric(12,4)、status draft/review/on/off/rejected、source manual/amazon/shopee/a1688/csv、source_ref、std_score smallint 0..100、std_missing jsonb、reject_reason、search_text tsvector 生成列留给 T-03 建索引、created/updated）、product_tier（product_id、min_qty、unit_price_e4 bigint；`UNIQUE(product_id,min_qty)`；min_qty>0、price>0）、product_image（product_id、file_id、sort）。
2. **三语完整校验**：name、spec 的 zh/en/km 三个键都必须非空才能 status=on（数据库 CHECK/触发器 + Go 层）；高棉语文案缺失给出明确错误码；草稿可不齐。**阶梯价至少 2 档**（提交审核/上架时校验，触发器兜底）。
3. **不跨 schema 读 iam**：catalog 维护投影表 `catalog.merchant_ref`（merchant_id、kind、status、updated_at），由 iam 事件（`iam.merchant.status_changed` 等，经 S-03 Relay 的幂等消费者）更新；**商户被停用/未核验 → 其商品对买家隐藏**（查询时过滤，不是改商品状态）。事件乱序/重复要幂等（按事件序号或 updated_at 比较）。
4. 状态机：draft→review（商家提交）→on（后台审核通过）/rejected（必填原因）；on⇄off（商家上下架，需 `prod` 权限）；rejected→draft（修改后再提交）；非法迁移被拒；每次迁移写 catalog 事件（outbox）供 T-02 重算与搜索索引更新。
5. 接口（OpenAPI 先行）：商家 /v1/merchant/products（列表、详情、创建、修改、提交审核、上下架；**只能改自己的商品**，别人的返回 404；RequireMerchantPerm('prod', view/edit)，修改价格等敏感操作调用 RecordOp）；买家 /v1/buyer/products（列表分页 + 品类/产地/价格区间筛选、详情，只返回 on 且商户可见的商品，**不返回 std_score 以外的内部字段**）；公开 /v1/public/categories（可缓存，S-02 的 CacheControl）；后台 /v1/admin/products（待审列表、通过、驳回，`prod` 模块 edit，写审计）。
6. 图片：product_image 引用 F-14 的 file_id（purpose=product_image），上架前至少 1 张（计入标准分，不作硬性要求时按 T-02 规则）；返回图片 URL 用 storage.URLFor（商品图走公共前缀）。
7. 不改库存数量：库存在 wms（W-01）；本模块只提供 `ProductSnapshot`（供订单与 wms 引用）。
【验收标准】
① 三语不齐不能上架（Go 与数据库双层，含只有 km 缺失的用例）
② 阶梯价至少 2 档、起订量唯一；价格为 e4 整数
③ 商家只能改自己的商品（别人的商品 404）；无 prod 权限的员工 403
④ 停用/未核验商户的商品对买家隐藏；事件重复/乱序不出错
⑤ 非法状态迁移被拒；驳回必须有原因；审核写审计
⑥ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 未核验（pending）商户的商品能否上架（默认：能提交，但上架需商户 verified 或后台特批——请在 PR 里说明实现）
- 分类种子清单（先放 6 个大类，名称三语草案）
【红线】不跨 schema JOIN iam；价格不得用 float；买家接口不得泄露商户内部字段与审核备注。
```

---

## T-02 商品标准分与商户标准分

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】T-02 商品标准分与商户标准分
【分支】task/T-02-standards
【依赖】T-01、S-03（重算任务）
【先阅读】
1. Dr.life+产品需求文档PRD.md「标准与画像」（商品标准 9 项 100 分 A≥85 / B 70–84 / C<70；商户标准：核验 30、资料完整 15、商品平均分 20、履约率 20、使用平台仓储 15、问题订单每单 −5，分 A/B/C/D）
2. Dr.life+开发任务拆分与验收.md：T-02；backend/internal/catalog/
【范围——要做】
1. 商品标准分纯函数 `ScoreProduct(product) (score int, missing []Item, grade)`：九项（三语名称、品类、规格、阶梯价、体积、交期、产地、实拍图、仓内有货）合计 100；**权重表放配置并有默认值**（默认：三语名称 15、品类 5、规格 10、阶梯价 15、体积 10、交期 10、产地 5、实拍图 20、仓内有货 10，合计 100——待确认），每一项判定规则写清（如阶梯价≥2 档满分，1 档半分；实拍图 ≥3 张满分）；等级 A≥85、B 70–84、C<70。
2. 「仓内有货」通过接口 `StockProbe`（wms 实现，W-01 前用 fake：恒为未知=不计分且不扣分，并在缺失项里标注『待仓库数据』）。
3. 商户标准分：核验 30（kind=verified 满分）、资料完整 15（按字段完整度）、商品平均分 20（该商户 on 商品的平均标准分 ÷ 100 × 20）、履约率 20（接口 `FulfilmentProbe`，trade 前用 fake）、使用平台仓储 15（接口 `StorageUsageProbe`）、问题订单每单 −5（下限 0）；等级 A/B/C/D 阈值写配置（默认 A≥85、B 70–84、C 50–69、D<50——待确认）。商户资料来源于 `catalog.merchant_ref` 扩展字段（由 iam 事件带入完整度摘要，不读 iam 表）。
4. 重算：商品变更事件（创建/修改/图片/阶梯价/状态）与每日全量对账任务（S-03 batch/scheduled 队列）触发；**幂等、可重跑**；只在分数或缺失项变化时写库与发事件（避免写放大）；商品分变化触发该商户的标准分重算（合并窗口去抖，默认 60 秒）。
5. 后台只读接口：/v1/admin/products/{id}/standard（总分、各项得分、缺失项、等级）、商户同理；商家端 /v1/merchant/products/{id}/standard（自己的缺失项提示补齐）。
6. 消费约束：C 级商品 **不进 AI 推荐**（提供 `IsRecommendable(product)` 给 N-04）、**搜索降权**（std_score 暴露给 T-03，权重在 T-03 配置）。
【验收标准】
① 规则与 PRD 一致（逐项用例，含边界 70/84/85）
② C 级不可被推荐（IsRecommendable 测试）；搜索可读到 std_score
③ 结果可在后台查看缺失项；商家端看到补齐提示
④ 重算幂等、事件重复不重复写；变化才写库；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 九项权重与商户 A/B/C/D 阈值（PRD 只给了商品等级阈值）
- 仓内有货的判定口径（有库存即满分还是按库存量分档）
【红线】不得读其他模块的表；权重写进配置而不是散落常量。
```

---

## T-03 搜索（联想、结果、排序、热词、历史、三语互搜）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】T-03 搜索（联想、结果、排序、热词、历史、三语互搜）
【分支】task/T-03-search
【依赖】P0-4 结论、T-01、T-02、S-02（降级等级、heavy 类）
【先阅读】
1. Dr.life+产品需求文档PRD.md「搜索（淘宝模式）」；Dr.life+技术方案与系统边界.md 第九章「搜索」；Dr.life+交互文档.md 买家端搜索
2. poc/ 下 P0-4 的结论与评测集（若尚未合并，按本任务自己建评测集，见验收）；Dr.life+API安全协议与容量及队列设计.md 5.3（heavy 类与降级）
3. Dr.life+开发任务拆分与验收.md：T-03、P0-4
【范围——要做】
1. 迁移：`catalog.search_synonym`（terms text[]，三语同义词组，GIN）、`catalog.search_log`（account_id 可空、q、result_n、created_at，**按天分区**，保留 90 天，到期 DROP PARTITION 的维护任务）、product.search_text 的 tsvector 生成与 GIN 索引、`pg_trgm` 索引（name 各语言）。
2. **三语互搜**：查询先做同义词扩展（地砖/tile/ក្បឿង 互通），再走全文检索 + 三字符 trigram 相似度；**高棉语没有词间空格**：对 km 文本使用字符 n-gram（trigram）匹配而不是依赖分词，结论以 P0-4 评测为准；中文用 trigram/bigram；英文用 simple/english 配置；错别字用相似度阈值容错。
3. 接口（OpenAPI 先行，/v1/buyer/search*，路由类 heavy）：联想 `GET /search/suggest?q=`（商品名、品类『在某分类中搜索』、店铺、同义词，最多 10 条，**缓存热门前缀**）；结果 `GET /search/products`、`/search/shops`（两个页签）；排序：综合、销量、价格升/降；筛选：品类、产地、价格区间、仓内现货；分页（keyset 或 page≤50）；热词 `GET /search/hot`（搜索统计 + 人工置顶配置，缓存）；历史 `GET/DELETE /search/history`（本人，可清除）；无结果时返回推荐商品（热销/同品类）。
4. **综合排序** = 相关度 + 商品标准分 + 商户标准分 + 销量（销量来自 trade 的投影表 `catalog.sales_ref`，由订单完成事件累加，幂等）；各项权重配置化；C 级商品降权（系数配置）；**广告位占位**：返回结构里预留 `sponsored` 标记位与 `AdProvider` 接口（N-08 实现），广告项必须带 `is_ad=true` 且**不改变自然结果的排序与分页**（测试证明开关广告前后自然结果顺序不变）。
5. 过载降级：读取 S-02 的降级等级——等级≥1 时关闭联想的店铺/同义词扩展与相关度重排，仅做简单前缀匹配；等级≥2 返回缓存热词与最近热门；**必须有测试**（注入降级等级）。
6. 搜索日志：异步写（队列，失败不影响响应），不记录个人可识别信息（account_id 只用于历史与画像，N-05 消费）；`search_log` 写入采样配置。
7. **评测门禁**：在 testdata 里放 100 个商品、50 个查询（中/英/高棉语/错别字，高棉语样本须由母语者审核，缺审的标 TODO-KM-REVIEW）及期望结果，测试计算召回率与首位命中率，**阈值写进测试**（初始：召回 ≥ 90%，首位命中 ≥ 70%，以 P0-4 结论为准）；低于阈值 CI 失败。
【验收标准】
① 三语互搜（地砖 / tile / ក្បឿង 搜到同一批瓷砖商品）
② 排序综合 = 相关度 + 标准分 + 商户分 + 销量，C 级降权；广告不影响自然排序
③ 无结果给推荐；历史可清除；热词可人工置顶
④ 降级等级生效（测试）；评测门禁达标并进 CI
⑤ 常规查询 P95 < 500 ms（本地 1 万商品规模基准，数据写进 PR）；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 是否需要上 Meilisearch/OpenSearch（由 P0-4 结论与基准决定，本任务只用 PostgreSQL）
- 各排序权重的初始值
【红线】不得在请求线程里做全表扫描；高棉语不得依赖空格分词；广告标注缺失视为缺陷。
```

---

## T-04 商品导入（1688 / 亚马逊 / 虾皮 / CSV）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】T-04 商品导入（1688 / 亚马逊 / 虾皮 / CSV）
【分支】task/T-04-product-import
【依赖】T-01、F-14、S-03、N-10（集成配置，可先 fake）
【先阅读】
1. Dr.life+产品需求文档PRD.md 商家「商品导入」；Dr.life+技术方案与系统边界.md 第九章（采集）、外部适配层；Dr.life+用户协议与隐私政策草案(三语).md（使用权声明）
2. Dr.life+数据库表结构与账务详细设计.md catalog.product_import；Dr.life+开发任务拆分与验收.md：T-04
【范围——要做】
1. 迁移 `catalog.product_import`（id、merchant_id、source、status: queued/running/done/failed/cancelled、items jsonb（大小上限，超出走文件）、rights_ack boolean not null、counts、error_summary、created_at）；`rights_ack` 必须为 true 才能创建导入（数据库 CHECK + Go 校验）。
2. 适配层接口 `SourceAdapter`（1688、Amazon、Shopee：按商品链接/ID 拉取**只读商品数据**——标题、规格、图片、价格、重量/尺寸；**不读取任何订单、买家、店铺后台数据**），每个适配器：超时、重试、熔断、限速、调用日志（不含密钥）；真实调用依赖 N-10 的集成配置与外部合规，**本任务只交付接口、模拟实现与解析器，真实对接标为待确认**。
3. CSV 导入：先经 F-14 上传 CSV（purpose=import_csv），后台任务（S-03 batch 队列）流式解析（行数上限、字段白名单、编码 UTF-8 BOM 容忍、公式注入防护：以 =,+,-,@ 开头的单元格转义）；逐行校验，坏行收集到错误报告（可下载），好行落为草稿商品。
4. 价格换算：来源币种（CNY/USD 等）按 `ops.fx_rate`（接口 `FxProvider`，N-10 前用配置 fake）换算成 USD e4 整数，**舍入规则写明并测试**（向上取整到 0.0001？以保守定价为准，列为待确认）；体积估算：有尺寸则按长宽高算，没有则按品类默认值并标注『估算』（计入标准分提示）。
5. 重复识别：同商户内按 (source, source_ref) 精确去重 + 标题 trigram 相似度提示（阈值配置），重复项标记而不是静默丢弃。
6. **导入后商品一律 status=review（待审核），不得直接上架**（数据库触发器兜底：source≠manual 的商品创建后状态只能是 draft/review）。
7. 接口：POST /v1/merchant/imports（带 rights_ack 与 Idempotency-Key）、GET 进度与结果、取消；权限 `prod` edit；限流归 heavy；每日导入量与单次条数上限（配置）。
【验收标准】
① 未勾选使用权声明无法导入（含绕过 Go 直接写库被拒）
② 导入的商品待审核才能上架（触发器兜底）
③ 只读商品数据，适配器代码与测试里不存在订单/买家数据的字段与调用
④ CSV 公式注入被转义；坏行有错误报告；外部适配走适配层，超时/熔断有测试（模拟故障）
⑤ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 1688/Amazon/Shopee 的真实接入方式与条款（官方 API / 合作商 / 授权）——由用户确认后另起对接任务
- 舍入规则
【红线】不得抓取或存储买家/订单数据；密钥不进日志；导入商品不得绕过审核。
```

---

## T-05 订单与采购单

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】T-05 订单与采购单
【分支】task/T-05-orders
【依赖】T-01、A-04、A-05、S-03、F-14；W-01（库存预占，可先用 StockReserver fake）
【先阅读】
1. Dr.life+技术方案与系统边界.md 第六章「订单」状态机与第七章（订单与资金的边界：货款线下，不进分录）；Dr.life+产品需求文档PRD.md 买家/商家订单、第 4 章核心流程
2. Dr.life+数据库表结构与账务详细设计.md 第三章 trade（order、order_item）；Dr.life+交互文档.md 买家端订单、商家端订单需求；prototype/index.html 订单流程
3. Dr.life+开发任务拆分与验收.md：T-05、T-06、W-05
【范围——要做】
1. 迁移（`trade` schema，`trade_rw`）：order（order_no 唯一、public_id、buyer_account_id、merchant_id、status unpaid/paid_proof/shipped/arrived/closed/problem/cancelled、total_e4、delivery_mode zto/self、delivery_fee_hold_txn_id、delivery_fee_release_txn_id、problem_note、closed_at、created_at）、order_item（order_id、product_id、product_name 三语快照 jsonb、qty、unit_price_e4 **下单时快照（含阶梯匹配结果）**、vol_m3 快照）、order_event（只追加的状态变更日志：旧/新状态、actor、原因）。**金额 total_e4 = Σ(qty×unit_price_e4)，触发器/Go 双重校验**；状态迁移由触发器限制合法路径。
2. 采购单：买家一次选多个商家的商品 → **按商家拆单**（一个采购单拆成多个 order，同一 `batch_no`）；阶梯价匹配：按 qty 取 min_qty ≤ qty 的最高档，**快照写入 order_item**，之后改价不影响已下单；商品需 on 且商户可见（调用 catalog 接口 `ProductSnapshot`，不读表）。
3. 下单：`POST /v1/buyer/orders`（Idempotency-Key 必需，服务端幂等；账号冻结/停用禁止下单——接口 `AccountGuard`，与账务的同名接口风格一致）；库存预占通过 `StockReserver`（W-01 前 fake，真实实现按可用库存预占，预占失败返回缺货明细）；order_no 生成规则（日期 + 随机，不可预测、不含个人信息）。
4. 状态机（见技术方案第六章）：unpaid →（商家记录付款凭证，T-06）paid_proof →（商家发货，W-05 触发）shipped →（签收，W-06 触发）arrived →（买家验收或超时自动，配置默认 7 天）closed；任一阶段可进 problem（售后 T-09）；取消：unpaid 买家可取消，paid_proof 前后规则写明（已记凭证后取消需商家同意，列待确认）。每次迁移写 order_event 与 outbox 事件，供通知（N-01）、搜索销量（T-03）、wms 使用。
5. 接口（OpenAPI 先行）：买家 /v1/buyer/orders（创建、列表、详情含状态步骤条数据、取消、确认验收）；商家 /v1/merchant/orders（列表按状态页签与筛选、详情；动作接口由 T-06/W-05 提供；`orders` 模块权限 view/edit）；后台 /v1/admin/orders（`ord` 模块 view，手机号脱敏）；**范围条件全部来自令牌**。
6. **订单不动资金**：本模块不调用 ledger（货款线下）；配送费托管由 W-05 触发。
【验收标准】
① 状态流转符合技术方案第六章，非法迁移被拒（表驱动测试 + 触发器）
② 下单快照价格与阶梯；改价后旧订单金额不变；total 与明细一致
③ 账号冻结/停用禁止下单；重复提交（同 Idempotency-Key）只创建一次，并发 50 次只产生一组订单
④ 买家只能看自己的订单、商家只能看自己店的订单（越权 404）；员工权限按 orders 模块
⑤ go vet、-race、ci-gate 全绿；参照原型订单流程
【待确认（不要自己拍板，PR 里列出）】
- 已记付款凭证后的取消规则
- 买家验收超时自动完成的天数（默认 7）
- 购物车是否服务端保存（默认前端本地，采购单一次提交）
【红线】不得在订单里存买家手机号等个人信息（需要时通过 iam 接口脱敏读取）；不得调用 ledger；订单金额不得用 float。
```

---

## T-06 付款凭证（线下付款，仅留痕）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】T-06 付款凭证（线下付款，仅留痕）
【分支】task/T-06-payment-proof
【依赖】T-05、F-14、A-05
【先阅读】
1. Dr.life+数据库表结构与账务详细设计.md trade.payment_proof 与第六章末「货款凭证」；AGENTS.md 账务规则 6（货款凭证不进分录）
2. Dr.life+开发任务拆分与验收.md：T-06、L-09；Dr.life+交互文档.md 商家端「记付款凭证」
【范围——要做】
1. 迁移 `trade.payment_proof`（id、order_id 或 plan_batch_id（二选一 CHECK）、amount_e4、ref（银行转账参考号/收据号）、proof_file_id、recorded_by（actor）、created_at；`UNIQUE(order_id, ref)`；**只追加**，触发器拒绝 UPDATE/DELETE，更正用『作废凭证 + 重新记录』的新记录（status voided，原因必填））。
2. 接口：`POST /v1/merchant/orders/{id}/payment-proofs`（Idempotency-Key；`orders` 或 `fin` 权限 edit；金额、参考号、凭证文件（F-14，purpose=payment_proof，上传者必须是该商户账号）），成功后订单 unpaid→paid_proof（同事务写 order_event）；买家可查看自己订单的凭证（只读）；作废接口（原因必填，需老板或 finance 角色）。
3. **不写任何 ledger 分录**：测试里断言记凭证前后 ledger 表无变化（transactions/postings 计数不变）。
4. 并列显示：提供只读接口 `ListProofs(ctx, filter)` 供 L-09 在流水页并列展示（标注『凭证（仅留痕）』），不要把凭证混进 ledger 的数据结构；返回字段脱敏（参考号后 4 位以外打码，无 fin 权限时）。
5. 校验：金额 > 0 且 ≤ 订单总额的合理上限（可超出一定比例？默认不允许超过订单总额，列待确认）；文件必须 ready 且属于本人。
【验收标准】
① 只写 payment_proof，ledger 无任何变化（断言）
② 凭证只追加，更正走作废 + 新记录；同订单同参考号唯一
③ 文件走预签名上传（F-14），他人上传的文件不能被引用
④ 记凭证后订单状态正确流转；员工权限与 403/404 正确
⑤ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 凭证金额是否可小于订单总额（分次付款）——默认允许多次凭证，累计 ≥ 总额视为已付清（请确认）
【红线】不得调用 ledger；不得允许修改/删除凭证；文件所有权必须校验。
```

---

## T-07 询价与报价（RFQ）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】T-07 询价与报价（RFQ）
【分支】task/T-07-rfq-quotes
【依赖】T-01、A-05
【先阅读】
1. Dr.life+产品需求文档PRD.md 买家「找装修/发布需求」与商家「订单需求」；Dr.life+交互文档.md 商家端订单需求页签与归类；prototype/index.html 需求与报价流程
2. Dr.life+数据库表结构与账务详细设计.md trade.rfq / quote；Dr.life+开发任务拆分与验收.md：T-07
【范围——要做】
1. 迁移 `trade.rfq`（public_id、buyer_account_id、name、area/loc、category_id、qty_text、budget_e4、due_date、target_merchant_id（可空：定向询价）、status open/selected/closed/expired、created_at）、`trade.quote`（rfq_id、merchant_id、price_e4、lead_days、valid_until、note、selected boolean、status active/withdrawn；**`UNIQUE(rfq_id, merchant_id) WHERE status='active'`**，同一商户对同一需求只能一份有效报价，可撤回后重报；**每个 rfq 至多一个 selected（部分唯一索引）**）。
2. 接口：买家发布/关闭需求、查看报价列表、**选择一份报价**（事务内：selected=true，其他报价标记 lost，rfq→selected，发事件通知各商家）；商家按页签列出需求：**待我报价 / 已报价 / 已选中我 / 他人已选**（归类逻辑与《交互文档》一致，用 SQL 视图/查询实现并测试每种状态）；商家提交/修改/撤回报价（`orders` 权限 edit；员工操作记录）；定向询价只对 target 商户可见。
3. 可见性：公开询价对所有已核验商户可见（按品类/地区匹配的推荐排序可选，非必须）；买家姓名/电话不暴露给商家，选中后通过 Mini App 聊天/Telegram 联系的方式列为待确认。
4. 过期：due_date 过期自动 expired（定时任务，S-03 scheduled，幂等）；valid_until 过期的报价不可被选。
5. 与 T-05 的衔接：选中报价后买家可『一键转订单』（调用 T-05 的下单接口，价格取报价快照，作为一条订单项）。
【验收标准】
① 同一商户对同一需求只能一份有效报价（并发重复提交只成功一份）
② 选择报价的并发：两个请求同时选不同报价，只有一个成功
③ 商家端四种归类与《交互文档》一致（每种有测试）；定向询价对其他商户不可见（404）
④ 过期任务幂等；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 选中后买卖双方的联系方式交换方式
- 报价是否允许多次修改的次数上限（默认 3）
【红线】不得向商家暴露买家个人信息；选择动作必须是单事务。
```

---

## T-08 装修项目与分批资金计划

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】T-08 装修项目与分批资金计划
【分支】task/T-08-projects-plans
【依赖】L-04（hold 状态机）、T-07、A-04、S-03
【先阅读】
1. Dr.life+技术方案与系统边界.md 第六章「分批结算（装修资金）」；Dr.life+产品需求文档PRD.md「装修公司/设计师/施工队/监理」与第 4 章 4、5；Dr.life+数据库表结构与账务详细设计.md pay_plan / pay_plan_batch 与第六章 6.2–6.4
2. backend/internal/ledger/hold.go、doc.go（FreezeHold/SettleHold/RefundHold/DisputeHold/ResolveDispute 的前置条件与错误）；Dr.life+交互文档.md 项目与资金计划页面
3. Dr.life+开发任务拆分与验收.md：T-08、L-04
【范围——要做】
1. 迁移：`trade.project`（buyer_account_id、company_merchant_id、name、area、budget_e4、status）、`trade.pay_plan`（project_id、total_e4、status draft/active/closed）、`trade.pay_plan_batch`（plan_id、seq、payee_merchant_id、amount_e4、condition 文本、status planned/frozen/released/disputed/refunded、hold_id（引用 ledger.hold 的 public 标识，不跨 schema 外键）、freeze_txn_ref、release_txn_ref）；**`SUM(batch.amount_e4) = plan.total_e4`**：在计划激活时由 Go 校验，并由**延迟约束触发器**在 plan.status→active 与批次改动时兜底。
2. 流程：公司/业主创建项目与计划（批次列表，收款方为商户）→ 买家**确认并激活计划** → 逐批冻结（调用 `ledger.FreezeHold`，ref_type='plan_batch'，ref_id=批次 id；买家可用资金不足返回可操作错误，提示充值）→ **节点确认**（谁确认见待确认；确认动作触发 `ledger.SettleHold`，收款方钱包由 iam/ledger 的 `EnsureWallet` 取得）→ 批次 released。**所有资金动作只通过 ledger 的业务方法**，本模块不写账务表，保存的 txn 引用仅用于展示与对账。
3. 争议：任一方对批次发起争议（原因必填）→ `DisputeHold`（冻结不动，不动账）→ 后台裁决（`disp` 模块 edit，运营可裁决、财务只能查看；**裁决人 ≠ 出款人（SoD）**：调用 `ResolveDispute` 时 ResolvedBy 与 PayerActorID 不得相同）→ 释放给收款方或退回买家；全程写审计与事件。
4. 幂等与恢复：每个资金动作先在 trade 侧记『意图』（batch 状态机的中间态），再调用 ledger（ledger 自带幂等）；崩溃重启后用 `RecoverBatches` 任务（S-03 scheduled）核对 ledger.hold 状态与批次状态，不一致的修复并告警；这一步必须有崩溃注入测试（在 ledger 调用成功后、trade 更新前强制中断）。
5. 接口：买家/公司/商户按角色的项目与计划读写（范围来自令牌）、后台 /v1/admin/projects、争议裁决接口（step-up + 审计）。**装修公司/设计师/施工队/监理的角色与账号归属**（PRD 提到但账号模型只有 buyer/merchant/driver）按『装修公司是 merchant 的一种类型』处理，设计师/施工队/监理只做节点确认的**被邀请参与人**，不建新账号角色；该处理列为待确认。
【验收标准】
① 批次合计 = 计划总额（Go 与数据库双层，含修改批次时的并发场景）
② 节点确认触发 SettleHold；冻结余额与未结清 hold 一致（调用 ledger.VerifyHoldsMatchFrozen）
③ 争议冻结不动、裁决人 ≠ 出款人、裁决后释放/退回正确
④ 崩溃注入后恢复任务修复不一致；重复确认/重复冻结幂等
⑤ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 节点确认由谁做（买家确认？监理记录 + 业主确认？）
- 设计师/施工队/监理是否需要独立账号角色
- 是否支持部分结算（账务第一版只支持整笔，已定）
【红线】不得直接写 ledger 表；不得绕过 hold 状态机；SoD 参数不得由客户端传入（来自服务端身份）。
```

---

## T-09 售后最小流程（退款 / 退货 / 破损赔付）

> 货款是线下付款（不进账务）。能走账务流水的只有：配送费托管退回、平台侧调整、以及『商家钱包 → 买家钱包』的赔付（账务目前没有这类业务方法）。这条在『待确认』里要用户拍板，否则 T-09 只能做状态机与已有账务能力。

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】T-09 售后最小流程（退款 / 退货 / 破损赔付）
【分支】task/T-09-after-sales
【依赖】T-05、L-03、W-05、A-06（后台权限）
【先阅读】
1. Dr.life+技术方案与系统边界.md 第六章「订单 → 售后」与第七章；Dr.life+产品需求文档PRD.md 第 9 章 P0「最小售后」
2. Dr.life+数据库表结构与账务详细设计.md trade.after_sale 与第六章 6.9–6.11；backend/internal/ledger/doc.go（对外业务方法清单）
【范围——要做】
1. 迁移 `trade.after_sale`（order_id、kind refund/return/damage、status requested/reviewing/approved/rejected/executing/done/cancelled、initiator_actor、decided_by、amount_e4、ledger_txn_ref、reason、evidence_file_ids、库存回流标记）；状态机触发器；**发起人 ≠ 审核人**（数据库 CHECK 兜底，谁发起谁审核有权限控制：买家发起、商家/后台审核，按订单状态规定可发起窗口）。
2. 资金流向只通过 ledger 业务方法：**已发货未签收取消 → `DeliveryRefund`（配送费托管退回商户）**；其余货款类退款是线下事项，系统只记录『线下已退款』凭证（同 T-06 风格，不进分录）；平台承担的赔付用 `Adjust`（两人复核，仅平台自有账户）或后续新增的业务方法；**不要自行发明新的账务入口**。
3. 库存回流：退货入库经 W-02 的入仓/回流接口（`StockReturn`，W-01 前 fake），破损不回流并记损耗；
4. 接口：买家发起、商家处理、后台仲裁（`disp`/`ord` 权限）；证据照片走 F-14；所有动作写 order_event（订单进入 problem 状态与恢复规则写明）与审计（后台）。
【验收标准】
① 全部资金流向走账务流水（可通过 ledger 查询到引用），货款线下部分只记凭证
② 谁发起谁审核有权限控制（数据库 CHECK + 服务端校验），状态机完整、非法迁移被拒
③ 退回配送费幂等；订单与售后状态一致
④ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- **赔付资金是否需要新增 ledger 业务方法（商家钱包 → 买家钱包）**：若需要，请先出一个『L-13 对等赔付入账』任务；本任务不得自行扩展 ledger
- 售后发起窗口（签收后 N 天）
【红线】不得绕过 ledger 的业务方法；不得修改订单金额快照；证据文件所有权要校验。
```

---

## 评审要点（给我自己用）

| 任务 | 重点 |
|---|---|
| T-01 | 三语校验是否真有数据库兜底；商户可见性投影与事件幂等；越权 404；状态机；买家响应是否泄露内部字段 |
| T-03 | 高棉语匹配是否真有效（看评测样本）；广告对自然排序的隔离；降级路径；基准与索引使用（EXPLAIN） |
| T-05 | 拆单与幂等并发；状态机触发器；价格快照；越权 404；是否有跨 schema 读 |
| T-08 | 与 ledger 的一致性恢复；SUM 约束的数据库兜底；SoD 来源；争议流程权限（disp 模块） |
