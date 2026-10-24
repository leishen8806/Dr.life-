# 波次 3：仓储与物流（W-01 至 W-10）

**用法：** 整块复制代码块内容粘贴给 Codex，每个任务一个独立 worktree 与分支。
**新增任务：** W-10 出库、拣货与打包（原任务表遗漏）。进货物流节点追踪（PRD P1）与分区立体库（P2）暂不安排。
**外部阻塞：** W-01 的库存主从关系依赖 P0-7；W-07 依赖 P0-5；W-08 依赖 P0-9；这些任务提示词已写成『接口 + 模拟实现先跑通』。

---

## W-01 wms 表、库位与库存不变量

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】W-01 wms 表、库位与库存不变量
【分支】task/W-01-wms-tables
【依赖】F-03、T-01（ProductSnapshot）、P0-7 结论（库存主从关系；未出结论时按『我们自己是库存之主』实现并经 WarehouseSystem 接口隔离）
【先阅读】
1. Dr.life+数据库表结构与账务详细设计.md 第三章 wms（zone/bin/stock/stock_bin/stock_move/inbound/storage_fee_daily/storage_statement）、第九章
2. Dr.life+产品需求文档PRD.md 物流仓储；Dr.life+交互文档.md 第五章（库存：数量=库位之和，不一致标红；盘点调整；库存流水）；Dr.life+平台后台权限与审计规范.md（审计 #14 库存一致性）
3. Dr.life+开发任务拆分与验收.md：W-01、P0-7；Dr.life+技术方案与系统边界.md 第二章（WMS 外购的边界）
【范围——要做】
1. 迁移（`wms` schema，`wms_rw`）：zone（code、kind shuttle/pallet/bulky/overflow、capacity_m3）、bin（zone_id、code **唯一**、capacity_m3）、stock（product_id **PK**、qty numeric(18,4) `CHECK (qty >= 0)`、vol_m3_each 快照）、stock_bin（product_id、bin_id、qty `CHECK(qty>=0)`，主键 (product_id,bin_id)）、stock_move（id、product_id、bin_id、kind in/out/adjust/transfer、qty_delta、ref_type、ref_id、actor_id、reason、created_at；**只追加**，触发器拒绝 UPDATE/DELETE/TRUNCATE，`ledger_rw` 式权限撤销）。
2. **数据库不变量（触发器）**：① `SUM(stock_bin.qty) = stock.qty`（延迟约束触发器，事务提交时核对）；② 数量不为负；③ **库位容量不得超**：库位内 Σ(qty × vol_m3_each) ≤ bin.capacity_m3（vol 快照存 stock，避免跨 schema JOIN）；④ 库存数量变化必须伴随 stock_move（提交时核对 stock.qty 的变化量 = 当次 move 的 Σ qty_delta）；⑤ stock_move 重算：提供 `RecomputeStockFromMoves` 并在测试里证明与 stock 一致。
3. 服务层 `wms.Stock`（唯一改库存的入口，其他模块只能经接口）：`Receive`（入库，W-02 调用）、`Pick/Issue`（出库，W-10 调用）、`Adjust`（盘点调整：必填原因，需 `logi` 权限 edit，**写审计**，大幅调整（占比阈值配置）需要二次复核——列待确认）、`Transfer`（库位间移库）、`Reserve/Release`（订单预占，实现 T-05 的 `StockReserver`；预占不改 stock.qty，单独 `wms.reservation` 表，可用量 = qty − 预占；预占超时自动释放，S-03 scheduled）；**并发安全**：对同商品行锁 `SELECT … FOR UPDATE`，固定加锁顺序（product_id 升序，再 bin_id 升序）；100 并发出库不超卖。
4. 分区规则：品类 → default_zone 推荐库位算法 `SuggestBin`（容量优先、同商品就近、分区匹配；纯函数，表驱动测试）；低库存提示（阈值配置，事件 `wms.stock.low`）。
5. 接口隔离：`WarehouseSystem` 接口（Receive/Issue/Adjust/Transfer/Stock/Moves…），默认实现为内置 wms；P0-7 若选定外购 WMS，则新增适配器实现而不改上层。**与外购 WMS 的主从关系列为待确认。**
6. 种子：默认分区（category 分区规则示例）与若干库位仅用于开发/测试环境（不入生产种子）。
7. 只读接口（供 W-09、T-02 StockProbe）：`GetStock(productID)`、`ListMoves`、`InStock(productID)`。
【验收标准】
① 触发器生效：数量=库位之和（故意写不一致在提交时被拒）、非负、库位容量不超、move 与库存变化一致
② stock_move 只追加，UPDATE/DELETE/TRUNCATE 被拒（管理员与应用角色）；可由流水重算库存
③ 100 并发出库不超卖、无死锁；盘点调整必填原因并写审计
④ 预占与释放正确，预占超时自动释放；SuggestBin 表驱动测试
⑤ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 库存主从关系（P0-7）
- 盘点大幅调整的复核阈值
- 数量是否允许小数（默认 numeric，按件计的商品用整数校验由品类决定）
【红线】不跨 schema JOIN；不得有绕过 stock_move 改库存的路径；库存不进缓存。
```

---

## W-02 入仓与实测量方

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】W-02 入仓与实测量方
【分支】task/W-02-inbound
【依赖】W-01、F-14、A-06/A-05、S-03
【先阅读】
1. Dr.life+技术方案与系统边界.md 第六章「入仓」状态机；Dr.life+产品需求文档PRD.md 入仓收货、第 4 章核心流程 1；Dr.life+交互文档.md 第五章（入仓）
2. Dr.life+数据库表结构与账务详细设计.md wms.inbound；Dr.life+开发任务拆分与验收.md：W-02、W-03
【范围——要做】
1. 迁移 `wms.inbound`（public_id、merchant_id、product_id、declared_qty、declared_vol_m3、actual_qty、actual_vol_m3、method、status booked/received/putaway/cancelled、diff_status none/pending_confirm/confirmed/disputed、diff_confirmed_at、photos、received_by、created_at）及状态机触发器。**按实测体积计费**：计费用字段 actual_*，商户自报仅作参考（数据库层面计费查询只读 actual）。
2. 流程：商家预约（/v1/merchant/inbounds，`store` 权限 edit，申报数量与体积、预计到货日）→ 仓库到货收货（/v1/wms/inbounds/{id}/receive：录入**实测**数量与体积、拍照（F-14 purpose=inbound_photo）、可选扫码）→ **差异超阈值**（数量或体积，阈值配置，默认 5%）→ diff_status=pending_confirm，通知商家确认（N-01），商家确认/争议前不开始计费、不上架；差异在阈值内或商家已确认 → 自动上架：`wms.Stock.Receive` + `SuggestBin`，状态→putaway，**记录计费起算时间**（供 W-03）。
3. 幂等：重复收货提交（同 Idempotency-Key 或同入仓单）不重复入库；并发收货同一单只成功一次；入库与计费起算在**同一事务**。
4. 取消：booked 状态可取消；已 received 的不可取消（走盘点调整）。
5. 权限：商家只看自己的入仓单（404）；仓库账号用 `logi`/wms 角色（A-06 的后台账号）；所有仓库侧写操作写审计。
6. 扫码：仅提供 `barcode` 字段与查找接口（按商品条码/入仓单号查），不做设备集成。
【验收标准】
① 按实测体积计费（计费查询只用 actual，有测试证明改申报值不影响）
② 差异超阈值需商户确认才上架并开始计费；阈值内自动上架
③ 重复/并发收货只入库一次；入库、上架、计费起算同事务
④ 越权 404；状态机非法迁移被拒；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 差异阈值默认 5%（数量与体积分别配置？）
- 商家一直不确认差异的超时处理
【红线】不得使用商户自报体积计费；不得绕过 wms.Stock 改库存。
```

---

## W-03 仓储费日计提与月账单

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】W-03 仓储费日计提与月账单
【分支】task/W-03-storage-fees
【依赖】W-02、L-03（StorageFee）、L-07b、S-03（scheduled 队列）、N-10（费率配置，可先 setting）
【先阅读】
1. Dr.life+数据库表结构与账务详细设计.md wms.storage_fee_daily / storage_statement、第六章 6.8、第十二章待确认 5（欠费策略）
2. Dr.life+第一批商户与定价试探方案.md（起步价 14 美元/立方米/月）；Dr.life+平台后台权限与审计规范.md（审计 #18 欠费）；Dr.life+API安全协议与容量及队列设计.md 6.6（定时任务）
3. backend/internal/ledger/doc.go（StorageFee 前置条件、幂等键）；Dr.life+开发任务拆分与验收.md：W-03
【范围——要做】
1. 迁移：`wms.storage_fee_daily`（merchant_id、product_id、day、qty、vol_m3、rate_e4、amount_e4；`UNIQUE(merchant_id,product_id,day)`；只追加）、`wms.storage_statement`（merchant_id、period yyyy-mm、accrued_e4、collected_e4、status open/collected/arrears、ledger_txn_ref、fx_rate_ref、usd_total_e4、khr_total、created_at；`UNIQUE(merchant_id, period)`）、`wms.rate`（费率版本：rate_e4 每立方米每月、effective_from、created_by，版本只增不改；种子 14 美元 = 140000 e4）。
2. **日计提任务**（S-03 scheduled，TaskID=计提日）：对每个商户每个商品，amount = qty × vol_m3 × rate_e4 ÷ 30，**用整数运算**（把 qty×vol 先换成固定小数整数，再乘 rate，再整除 30，**舍入规则写明并有测试：每日向下取整到 e4，月末账单合计 = Σ日计提，差额不追补——列待确认**）；以当日结束时的库存与『计费起算时间』为准（W-02 已上架才计）；幂等：重跑同一天不重复写；补跑缺失日；不进总账（子账）。
3. **月度出账**（每月出账日，配置，默认次月 1 日）：汇总上月日计提生成 statement；**一次性调用 ledger.StorageFee**（幂等键 `storage_fee:bill:<statement id>`）；余额不足 → `ErrInsufficientFunds` → statement.status=arrears，**不扣成负数**；充值后自动催收：消费 `ledger.txn.posted`（topup 类型）事件的幂等消费者重试扣款；欠费超阈值（默认 200 美元）→ 产生审计 #18 所需数据与事件；**欠费是否限制发货、宽限期**为待确认，先在 `wms.arrears_policy` 配置项里默认『不限制，仅提醒』。
4. 对账单双币显示：USD 为准，KHR 按 `ops.fx_rate`（FxProvider 接口，N-10 前配置 fake）折算并**把当次汇率引用固化到 statement**（fx_rate_ref），之后不重算。
5. 接口：商家 /v1/merchant/storage/statements（列表、详情含每日明细分页，`store` 或 `fin` view）、后台 /v1/admin/storage（欠费列表）、wms Web 查看；导出 CSV 走异步（W-09 机制）。
6. 费率调整：后台新建费率版本（`cfg` 权限 + 审计），**只影响生效日之后的计提**，历史不变。
【验收标准】
① 日计提公式与舍入有表驱动测试（含小数体积、1 件、极大库存）；同一天重跑不重复；缺失日可补跑
② 月度出账一次扣款；余额不足 → arrears、不扣成负数；充值后自动催收成功且幂等
③ 对账单合计 = Σ日计提；双币显示且汇率固化；费率版本不影响历史
④ 欠费超阈值产生事件；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 欠费是否限制发货与宽限期（账务设计文档第十二章第 5 条）
- 舍入与出账日
- KHR 汇率来源（手工维护 / 央行牌价，N-10）
【红线】不得浮点；不得对未上架库存计费；不得把子账日计提写进总账。
```

---

## W-04 配送费规则与计算（版本化）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】W-04 配送费规则与计算（版本化）
【分支】task/W-04-delivery-rules
【依赖】L-03（Allocate 分账）、A-06、T-05
【先阅读】
1. Dr.life+数据库表结构与账务详细设计.md logistics.delivery_rule、第六章 6.9–6.10；Dr.life+产品需求文档PRD.md 配送费与『平台不向司机收费』；Dr.life+第一批商户与定价试探方案.md（配送定价）
2. backend/internal/ledger/allocate.go（Allocate/基点）；Dr.life+开发任务拆分与验收.md：W-04、W-05、W-06
【范围——要做】
1. 迁移 `logistics.delivery_rule`（id、version、base_e4、per_km_e4、margin_bp（平台留存基点，默认 0）、zto_fee_e4（中通份，可空）、driver_bp、effective_from、created_by、note；**版本只增不改**，触发器拒绝 UPDATE/DELETE；生效时间区间不重叠）。
2. 纯函数 `deliveryFee(rule, distanceKm, mode) → {total_e4, driver_e4, platform_e4, carrier_e4}`：全部整数运算（距离先换成米整数）；各份用 ledger.Allocate 思想『最后一项倒挤』，**之和恒等于总额**；平台留存默认 0；中通配送（mode=zto）时 carrier 份来自 zto_fee_e4 或 ZTO 报价接口（W-07），自建配送时 driver 份 = 总额 − 平台留存；**公式细节（是否含重量/体积加价、起步里程）未在文档定义，列为待确认，先实现 base + per_km×距离，并把公式封装在可替换的策略接口里**。
3. 快照：计算结果与所用规则版本号在**发货时**固化到 shipment（W-05），之后规则调整不影响已托管订单（测试证明）。
4. 接口：商家/买家下单前的运费预估 `POST /v1/{buyer|merchant}/delivery/estimate`（只读，用当前生效版本）；后台 /v1/admin/delivery-rules（新建版本需 `logi`/`cfg` 权限 + step-up + 审计，**不能修改历史版本**）。
5. 距离来源：接口 `DistanceProvider`（地图适配层，N-10 集成配置；先提供直线距离的 haversine 实现与 fake，并标注误差）。
【验收标准】
① 规则调整不影响已托管订单（版本快照测试）
② 各份之和恒等于总额（属性测试：随机距离、极小/极大金额、奇数）；无浮点
③ 规则版本只增不改，区间不重叠（数据库约束/触发器）；后台改规则需权限与审计
④ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 配送费公式细节（起步价、里程阶梯、重量/体积加价）
- 平台留存是否永远 0（承诺为不向司机收费，留存仅在中通/平台模式下可能>0）
【红线】不得对司机端收取任何费用；不得修改历史规则版本；不得浮点。
```

---

## W-05 发货与配送费托管

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】W-05 发货与配送费托管
【分支】task/W-05-ship-escrow
【依赖】W-04、T-05、T-06、L-03、W-07/W-08（配送方式，可先接口）
【先阅读】
1. Dr.life+技术方案与系统边界.md 第六章（订单、配送费托管）；Dr.life+数据库表结构与账务详细设计.md logistics.shipment、第六章 6.9、6.11
2. backend/internal/ledger/doc.go 与 business.go（DeliveryHold/DeliveryRefund 前置条件、按单托管检查）；Dr.life+交互文档.md 商家端发货与 wms 出库
3. Dr.life+开发任务拆分与验收.md：W-05
【范围——要做】
1. 迁移 `logistics.shipment`（order_id **唯一**、mode zto/self、waybill_dom/xb/kh、driver_id、status created/picked/in_transit/delivered/cancelled、fee_total_e4、fee_driver_e4、fee_platform_e4、fee_carrier_e4、rule_version、dist_km、hold_txn_ref、created_at）；`shipment_node`（shipment_id、seq、label、location、at、source zto/driver/manual；只追加）。
2. 发货：`POST /v1/merchant/orders/{id}/ship`（Idempotency-Key；`orders` 权限 edit；订单须 paid_proof）：计算配送费（W-04）→ **商户可用资金 ≥ 配送费才允许** → 调用 `ledger.DeliveryHold`（ref=订单）→ 同事务写 shipment（含费用快照与规则版本）、订单→shipped、order_event、outbox。**资金不足返回可操作错误**（含差额与『去充值』所需信息，不泄露余额之外的内部信息）；ledger 调用成功而 trade/logistics 写入失败的崩溃恢复：恢复任务按订单核对 ledger 的 delivery_hold 与 shipment，缺 shipment 的补建或退回托管并告警（要有崩溃注入测试）。
3. 取消发货/订单取消：未签收前取消 → `ledger.DeliveryRefund`（按单托管检查已内置），shipment→cancelled；已签收分账后不可取消。
4. 配送方式：mode=zto 调 W-07 适配器下单取运单号（失败时订单保持 paid_proof 并回滚托管，或进入『待重试』——写明并测试）；mode=self 进入派单池（W-08）。
5. 审计一致性数据：『已发货却没有托管』『已签收但托管未分账』是审计异常（Q-02）——提供只读查询 `ListShipmentsWithoutEscrow` 等。
【验收标准】
① 资金不足不能发货（返回可操作错误，且无任何残留：无 shipment、无 ledger 交易、订单状态不变）
② 发货后托管金额 = 配送费快照；同一订单重复发货幂等（并发 50 次只托管一次）
③ 崩溃注入（ledger 成功后中断）后恢复任务修复一致；取消发货正确退回托管
④ 越权 404；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 中通下单失败时的处理策略（回滚托管 vs 待重试）
【红线】不得绕过 ledger.DeliveryHold；不得在 ledger 成功后丢失 shipment；配送费快照一旦写入不可改。
```

---

## W-06 签收凭证与配送费分账

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】W-06 签收凭证与配送费分账
【分支】task/W-06-delivery-proof-release
【依赖】W-05、L-03（DeliveryRelease）、F-14
【先阅读】
1. Dr.life+技术方案与系统边界.md 第六章（签收：有签收凭证才触发分账）；Dr.life+数据库表结构与账务详细设计.md logistics.delivery_proof、第六章 6.10
2. backend/internal/ledger/business.go（DeliveryRelease：ProofID、DriverBP/PlatformBP/CarrierBP、Remainder、按单托管检查）；Dr.life+交互文档.md 司机端『记录签收』
3. Dr.life+开发任务拆分与验收.md：W-06
【范围——要做】
1. 迁移 `logistics.delivery_proof`（shipment_id、kind photo/signature/code、file_id（F-14）、code_hash（验证码哈希，**只存哈希**）、at、actor_id、geo 可空、created_at；只追加）；**数据库触发器：shipment 进入 delivered 状态必须存在至少一条凭证**。
2. 签收提交：司机端 `POST /v1/driver/tasks/{id}/proof`（拍照/签名/验证码三选一或组合；验证码由买家收到并在签收时报出，服务端比对哈希，错误次数限制）；中通配送的签收由 W-07 的回调触发（以回传的签收凭证或签收节点为准，列待确认）；买家端『确认验收』是订单 arrived→closed，与配送签收区分。
3. **有凭证才分账**：签收成功后（同一事务写 delivery_proof、shipment→delivered、order→arrived）发事件，分账由**幂等任务**执行：调用 `ledger.DeliveryRelease`，用 shipment 快照的 fee_*：driver 份进司机钱包（`EnsureWallet`）、平台留存（默认 0）、中通应付；**各份之和 = 托管金额**（用 Allocate，余额份指定）；金额为 0 的份不生成分录；**重复签收不重复分账**（ledger 幂等键 `delivery_release:order:<id>` + 任务去重）；分账失败重试并告警，直到成功（订单不得停在『已签收未分账』，由恢复任务扫描修复，这是审计异常项）。
4. 司机收入状态：预计 → 待结算（已送达）→ 已结算（分账入账后）；提供只读聚合供 W-08 使用。
5. 防作弊：同一设备/账号短时间大量签收、凭证文件重复（sha256 相同）告警（事件，不阻断）；GPS 与收货地距离偏差仅记录。
【验收标准】
① 无凭证不分账（数据库触发器 + 服务层双重）
② 三方分账之和 = 托管金额（含奇数、极小金额）；金额为 0 的份无分录
③ 重复签收不重复分账；分账失败自动重试，恢复任务能修复『已签收未分账』
④ 司机收入入账（钱包）正确；买家验证码错误次数限制有效
⑤ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 中通配送签收的凭证来源（P0-5 结论）
- 自建配送是否允许『无买家在场』的替代签收
【红线】验证码只存哈希；不得在无凭证时触发分账；分账不得在请求线程里同步强依赖（失败不影响签收记录）。
```

---

## W-07 中通适配器

> 外部接口规格未定：先按适配器接口 + 模拟实现跑通，真实对接在拿到测试账号和文档后另起提交。

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】W-07 中通适配器
【分支】task/W-07-zto-adapter
【依赖】P0-5 结论（接口文档与测试账号）、S-01（webhook 验签）、S-03、N-10
【先阅读】
1. Dr.life+技术方案与系统边界.md 第二章（中通）、第八章（外部适配层：超时、重试、熔断、降级）；Dr.life+API安全协议与容量及队列设计.md 第一章（回调伪造）、S-01 的 WebhookVerifier
2. poc/ 下 P0-5 的结论（若有）；Dr.life+开发任务拆分与验收.md：W-07、P0-5；Dr.life+数据库表结构与账务详细设计.md logistics.shipment_node
【范围——要做】
1. `backend/internal/logistics/zto/`：接口 `CarrierGateway`（CreateOrder、Cancel、PullTracks、ParseCallback、EstimateFee）+ **模拟实现**（可注入延迟、失败、乱序、重复回调）+ 真实实现骨架（HTTP 客户端、签名、重试、熔断；凭据来自 N-10 集成配置与 F-10，**不入库不入日志**）。
2. 下单：W-05 调用 CreateOrder 取运单号（国内段/跨境段/柬埔寨段 waybill_dom/xb/kh）；失败降级见 W-05；调用与回调全部记录到 `logistics.carrier_call_log`（脱敏：不含密钥与完整手机号/地址）。
3. 轨迹：定时拉取（S-03 scheduled，间隔配置，按活跃运单分批，限速）+ 回调推送双通道，节点入库 `shipment_node`（**只追加；以 (shipment_id, source, 外部事件号) 去重幂等**，乱序到达按事件时间排序展示）；『有什么显示什么』：缺字段不报错。
4. 回调入口 `/v1/webhook/zto`：用 S-01 的 HMAC+时间戳+随机数验签（具体算法以 P0-5 为准，默认实现 HMAC-SHA256），**未验签直接拒绝**，幂等；处理异步（入队后快速返回 200）。
5. 降级：超时/失败时订单页显示『稍后更新』（返回 degraded=true 的节点列表与最后更新时间），熔断开启时不再调用外部；指标：调用成功率、延迟、熔断状态、回调验签失败。
6. 签收：中通回传签收事件 → 触发 W-06 的签收流程（凭证来源见待确认）。
【验收标准】
① 超时/失败时降级显示『稍后更新』，不影响订单主流程；熔断与恢复有测试
② 回调验签失败被拒、重复回调幂等、乱序回调正确入库
③ 密钥只走集成配置，日志与调用记录无密钥与完整个人信息（子串断言）
④ 模拟实现可跑通 下单 → 轨迹 → 签收 全流程；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 真实接口规格、验签算法与字段映射（P0-5）
- 中通费用报价接口是否存在（影响 W-04 的 carrier 份）
【红线】不得在拿到真实文档前臆测并写死字段；不得把客户电话/地址明文写日志。
```

---

## W-08 司机与任务（派单、接单、位置、收入）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】W-08 司机与任务（派单、接单、位置、收入）
【分支】task/W-08-driver-tasks
【依赖】W-05、W-06、A-02、L-05（提现入口）、P0-9 结论（弱网与定位）
【先阅读】
1. Dr.life+产品需求文档PRD.md 司机章节；Dr.life+交互文档.md 第四章（司机端：任务、接单、送达、收入、提现、弱网）；Dr.life+数据库表结构与账务详细设计.md logistics.driver
2. poc/ 下 P0-9 结论；Dr.life+API安全协议与容量及队列设计.md（写接口幂等、heavy/critical 类）；Dr.life+开发任务拆分与验收.md：W-08
【范围——要做】
1. 迁移 `logistics.driver`（account_id 唯一、plate、vehicle、status pending/approved/suspended、online boolean、last_seen_at）、`logistics.driver_location`（driver_id、lat/lng numeric、accuracy、at、client_event_id；**按天分区，保留 30 天**）、`logistics.task`（shipment_id、driver_id 可空、status new/accepted/picked/delivered/cancelled、offered_at、accepted_at、…）。
2. **司机入驻**：司机端首次登录后提交资料（车牌等，证件照 F-14）→ 物流/平台后台审核（`logi` 权限，审计）通过后才能接单；未通过只能看到入驻状态。
3. 派单：自建配送的 shipment 进入任务池；**手动派单**（wms 后台指定司机，`logi` edit）与**抢单/推送**两种模式（配置，默认手动派单 + 就近推荐，推荐仅作排序）；接单 `POST /v1/driver/tasks/{id}/accept`（**并发抢单只有一个成功**，行锁/条件更新）；取货、上报位置、送达（W-06）；**司机只能看到派给自己的任务（别人的 404）**；新任务页签只显示可抢的池内任务（不含他人已接）。
4. 位置上报：`POST /v1/driver/location`（批量数组、每批上限、每秒速率受 S-02 限流；`client_event_id` 去重保证弱网补传**不重复**；时间戳校验与时钟漂移容忍；过旧数据拒绝）；写入走队列批量落库，失败不影响接口响应；地图所需数据接口（任务点与当前位置，不返回他人位置）。
5. 弱网与离线：接口全部幂等（任务状态更新带客户端序号，服务端按序号去重与乱序容忍），配合前端离线队列（U-07）；服务端提供 `GET /v1/driver/sync?since=` 增量同步。
6. 收入与提现：`GET /v1/driver/earnings`（总收入、待结算、今日、完成数、平均每单、明细分页；明细展开基础费/里程费；**只显示司机自己的金额，不暴露平台与中通份**）；提现入口调用 ledger 的 RequestWithdrawal（L-05；二次验证与账号状态按网关/guard 流程）；司机提现无手续费。
7. 平台不向司机收取任何费用：代码与配置里**不得**出现对司机端的扣费项（测试扫描：司机相关 ledger 业务方法只有 DeliveryRelease 入账与 RequestWithdrawal/Payout）。
【验收标准】
① 司机只能看到派给自己的任务；未审核司机不能接单
② 并发抢单只有一个成功；重复提交状态更新幂等
③ 断网 5 分钟后补传的位置与状态不重复、不丢失（client_event_id 去重测试）
④ 收入视图只含司机自己的金额；提现走 ledger 并受账号状态约束
⑤ go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 派单模式（手动/抢单）默认值
- 位置保留期限与隐私披露（协议需要覆盖）
- 是否需要提前启动司机 App（P0-9 结论）
【红线】司机位置不对买家以外无关方暴露；不得对司机收费；位置表必须分区并有保留期。
```

---

## W-09 物流仓储 Web 接口（表格化与导出）

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】W-09 物流仓储 Web 接口（表格化与导出）
【分支】task/W-09-wms-web-api
【依赖】W-01..W-06、A-06（wms 账号）、L-09（导出机制）、S-02（report 池）
【先阅读】
1. Dr.life+交互文档.md 第五章（wms：库存、入仓、出库、配送、接口状态；通用表格、右侧抽屉、导出 CSV）；Dr.life+产品需求文档PRD.md 物流仓储
2. Dr.life+API安全协议与容量及队列设计.md 5.4（report 池、导出异步）；Dr.life+开发任务拆分与验收.md：W-09
【范围——要做】
1. OpenAPI 先行的 /v1/wms/* 读接口：库存（按商品/分区/库位，库存详情含库位明细、库存流水、**数量=库位之和不一致标红的字段**）、在途进货、入仓单、出库单（W-10）、配送单（运单、节点、司机、状态）、接口状态（中通/地图等适配器健康与最近调用）；统一的列表协议：筛选（白名单字段）、搜索、排序（白名单）、分页（keyset 优先，page_size ≤ 100）。
2. 走 `report` 连接池（只读/从库）：不得占用 oltp 池；超时 30 秒；重查询必须有索引并在 PR 里附 EXPLAIN 摘要。
3. 导出 CSV：**异步**（创建导出任务 → S-03 batch 队列生成 → F-14 存储 → 短期签名链接），行数上限与每日导出次数上限；CSV 公式注入转义；导出权限与列表一致，**敏感字段按权限脱敏**；导出动作写审计（谁、导出了什么、过滤条件，不记录数据本身）。
4. 权限：仅 wms/admin 后台账号（A-06），`logi` 模块 view/edit 边界；范围（data_scope 地区/仓库）过滤；越权 403/404。
5. 实时：待办与告警通过 SSE 通道（N-01 提供）推送；本任务只提供数据接口。
【验收标准】
① 表格化列表与详情接口参照《交互文档》物流仓储；库存详情含库位明细与流水
② 导出异步、与列表一致、脱敏与审计正确；重导出幂等
③ 走 report 池，慢查询有索引（EXPLAIN 附上）
④ 越权被拒；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 进货物流节点（P1）是否在此一并提供（默认只留接口位）
【红线】不得在请求线程里同步生成大导出；筛选/排序字段必须白名单。
```

---

## W-10 出库、拣货与打包（新增任务）

> 原任务表只有『发货』（W-05），没有仓库侧拣货打包与出库扣库存。PRD 与交互文档都要求：拣货单 → 打包 → 配送方式 → 交接。

```text
你在 Dr.life+ 仓库工作（私有仓库 leishen8806/Dr.life-，默认分支 main）。请基于 origin/main 最新提交新建独立 worktree，不要在用户的 E:\Dr.life+ 主工作目录里改；只提交到本任务分支并发起 PR，不要推 main，不要合并。
【通用规则】先读 AGENTS.md（第 4 节规则全部适用）和《开发任务拆分与验收》第十二章清单；文档标“待确认/⚠️”的地方不要自己拍板，按保守默认实现并在 PR 里列出；已合并迁移一律不改，只能新增（取序号见 backend/migrations/README.md）；改表同步《数据库表结构与账务详细设计》，改接口先改 backend/api/openapi.yaml 再生成；权限、范围、脱敏全在服务端，资源范围条件来自令牌，别人的资源一律返回 404；后台写操作写审计（Q-01 未合并前调用 audit.Writer 接口，用 fake 实现）；三语文案齐全，新增高棉语草案带 TODO-KM-REVIEW；日志与错误不含密钥、令牌、完整手机号、聊天原文；测试用真实 PostgreSQL（testcontainers）与 -race，权限与状态机必须有专项测试；金额一律 bigint（0.0001 美元），不得用 float；外部系统一律经适配层并提供模拟实现；跨模块只通过接口或事件，不直接读写别人的 schema；完成标准：go vet、-race、集成测试、OpenAPI 无差异、ci-gate 全绿；PR 描述写明做了什么、没做什么、验收逐条结果、待确认事项、对文档的影响。

【任务】W-10 出库、拣货与打包（新增任务）
【分支】task/W-10-outbound-picking
【依赖】W-01、W-05、F-14、A-06
【先阅读】
1. Dr.life+产品需求文档PRD.md 物流仓储『出库』；Dr.life+交互文档.md 第五章『出库与配送』；Dr.life+技术方案与系统边界.md 第六章（订单、入仓）
2. backend/internal/wms/（W-01 的 Stock.Issue、Reserve）；Dr.life+开发任务拆分与验收.md：W-05
【范围——要做】
1. 迁移 `wms.pick_list`（order_id、status created/picking/picked/packed/handed_over/cancelled、picker_id、…）、`wms.pick_line`（product_id、bin_id、qty、picked_qty）；状态机触发器。
2. 流程：订单进入 paid_proof 且商家发货后（W-05）自动生成拣货单（按库位排序的路径）→ 仓库账号领取并拣货（扫码/手动，**实际拣货数量 ≤ 预占数量**）→ 打包（箱数、重量、体积、照片 F-14）→ 交接给配送（中通取件或司机取货）；**出库扣库存在『拣货确认』时**通过 `wms.Stock.Issue`（扣预占转实际扣减，stock_move kind=out），数量与库位一致性由 W-01 触发器保证。
3. 短拣/缺货：拣货数量不足 → 标记差异、通知商家与买家（N-01）、订单标记 problem 并走 T-09，不得静默少发；
4. 接口：/v1/wms/pick-lists*（`logi` 权限；所有写操作写审计）；与 W-09 的出库列表衔接。
5. 幂等：同一拣货行重复提交不重复扣库存；并发两位拣货员领取同一拣货单只有一个成功。
【验收标准】
① 出库扣库存只发生一次（重复提交幂等），库存一致性触发器保持成立
② 短拣有记录、有通知、订单进入 problem
③ 并发领取只有一个成功；状态机非法迁移被拒；go vet、-race、ci-gate 全绿
【待确认（不要自己拍板，PR 里列出）】
- 是否需要波次/合并拣货（第一版不做）
【红线】不得绕过 wms.Stock 扣库存；拣货数量不得超过预占。
```

---

## 评审要点（给我自己用）

| 任务 | 重点 |
|---|---|
| W-01 | 延迟约束触发器是否真兜底；并发加锁顺序；预占与可用量；move 与库存一致性；应用角色权限 |
| W-05 | ledger 与 shipment 的一致性恢复；幂等并发；资金不足无残留；取消后的托管退回 |
| W-06 | 触发器兜底；分账幂等与恢复；验证码暴力破解限制；分账金额与快照一致 |
