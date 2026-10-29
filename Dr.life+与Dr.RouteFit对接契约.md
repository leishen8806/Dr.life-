# Dr.life+ 与 Dr.RouteFit 对接契约（R-01）

> 状态：**v0.2，项目负责人已确认第 10 节全部事项**（2026-10-29）。RouteFit 侧按第 11 节派卡实现；接口细节如需调整，回到本文修订。卡片 CMP-638。
> 依据：《试点上线最小范围清单》第 1 节分工与 3.3 节 R-01…R-06；RouteFit 仓库 `E:\Dr.RouteFit` 的 `contracts/`（核对日期 2026-10-29，RouteFit 全部接口状态为 `LOCAL_IMPLEMENTED_NOT_DEPLOYED`）。
> 本文只定**接口形状与双方责任**，不写实现。凡标 **【RouteFit 需新增/修改】** 的项，由项目负责人在 RouteFit 侧派卡。在 RouteFit 确认之前，Dr.life+ 侧只用本地替身（mock）开发，不能以替身结果通过 G-11。

## 1. 分工与原则

| | Dr.life+ | Dr.RouteFit |
|---|---|---|
| 商品与价格 | 商品展示、售价、下单、买家付款（KESS） | 不涉及售价 |
| 实物与库存 | 只读同步可售数量，下单时预占 | 入仓、库位、库存为准（D-3） |
| 配送 | 订单付款后提交**仓库出库配送单** | 拣货、打包、派单、承运方、签收复核 |
| 资金 | 货款托管与结算；向商户收仓配费；付 RouteFit 应付款 | 计算仓配费用并报告给 Dr.life+；不向 Dr.life+ 的买家或商户直接收款 |

原则：

1. **RouteFit 是实物与履约的唯一事实来源**，Dr.life+ 是订单与资金的唯一事实来源。双方都不改对方的状态，只通过本契约的接口和事件通知对方。
2. **至少一次投递、按事件编号去重、按版本号丢弃旧事件**。任何一方都不能假设消息只到一次或按顺序到达。
3. **钱只在"已核实"事实上移动**：货款只在 RouteFit 报告"签收已复核"后结算给商户；仓配费只按 RouteFit 报告的费用行扣款，并按日对账。
4. 金额一律用整数最小单位，不做隐式换汇（见第 7 节）。

## 2. 现状差距（RouteFit 现有接口 vs 商城需要）

| # | 商城需要 | RouteFit 现状（出处） | 处理 |
|---|---|---|---|
| G1 | 一个平台代多个商户（货主）下单 | 一个 ServiceToken 只绑定一个 `customer_id`（`051_service_intake.sql` 主键 company+principal）；请求体不接受自报货主 | **【RouteFit 需新增】** 平台通道（4.1） |
| G2 | 仓库内现货出库并配送 | 外部导入是"点到点货物"模型（pickup/delivery 地址 + CargoItem），会丢弃 `skuId` | **【RouteFit 需新增】** 平台出库单（4.2） |
| G3 | 付款后自动进入履约 | 导入后只是 `pending_review` 草稿；报价和派单要运营侧 quoteId 与同意证据，外部无法生成 | **【RouteFit 需新增】** 预付平台单自动受理（4.2） |
| G4 | 取货前取消 | 外部接口没有取消 | **【RouteFit 需新增】**（4.3） |
| G5 | 进度、签收、费用主动回传 | 无外部 webhook；`/external-orders` 结果回传仅列为候选（`api-design.md:581`） | **【RouteFit 需新增】** 出站事件（第 5 节） |
| G6 | 签收证据可供售后与争议查看 | ServiceToken 无证据读取接口（`evidence-api.md:9`） | **【RouteFit 需新增】** 证据摘要与短期下载链接（5.3） |
| G7 | 可售数量 | ServiceToken 无库存查询；只有货主门户与 WMS 入站 `stock-events` | **【RouteFit 需新增】** 库存查询与变更事件（第 6 节） |
| G8 | 按商户报告仓配费用并对账 | 有 FeeItem、Quote 和 19 个费目，但无对外费用报告，费用设置页尚未接 API（`fee-settings-v0.1.md:3`） | **【RouteFit 需新增】** 费用行事件与日结单（第 7 节） |
| G9 | 欠费暂停商户出库 | 无 | **【RouteFit 需新增】**（4.4） |
| G10 | 买家下单时的实时运费报价 | 只有运营侧报价接口，且只支持固定费，按距离计价返回 `CAPABILITY_DISABLED` | **【RouteFit 需新增】** 平台报价（4.5） |

可以直接沿用的 RouteFit 约定：Bearer 服务令牌、`Idempotency-Key`（16–128 字符，同键异体返回 409）、强 ETag + `If-Match`、`ApiError` 错误体（code、retryable、requestId、currentVersion）、`Money{amountMinor,currency}`、`Location`（E.164 电话、经纬度可空）、内部 outbox 事件信封（`api-design.md:450-488`）。

## 3. 身份与安全

- **服务令牌**：RouteFit 给 Dr.life+ 签发一个平台通道令牌（通道 `PLATFORM`，`source_system=drlife`），scope 至少包括 `platform:owners:read`、`platform:fulfillments:create|read|cancel`、`platform:inventory:read`、`platform:owners:hold`、`platform:statements:read`、`platform:evidence:read`。令牌存放在 Dr.life+ 的集成配置（N-10），走密钥信封加密，不进日志。轮换期间新旧两枚同时有效。
- **货主授权**：每个商户在 RouteFit 侧是一个货主（`party.customer`）。平台令牌能代哪些货主操作，由 RouteFit 侧的绑定表决定（4.1），Dr.life+ 在请求里带 `ownerRef`，RouteFit 校验它在绑定范围内，不在范围内返回 404。
- **出站事件签名**（RouteFit → Dr.life+）：见 5.1。签名密钥与服务令牌分开，独立轮换。
- **网络**：RouteFit 测试环境与生产环境地址由 RouteFit 提供（E-1）；Dr.life+ 回调地址为 `https://adminapi.settlecore.org/v1/integrations/routefit/events`（统一 API 网关，路径级限制）。

## 4. Dr.life+ → RouteFit 接口

### 4.1 货主与商品对应 【RouteFit 需新增】

- `integration.platform_owner_binding`：(company_id, principal_id, customer_id, external_owner_ref, state)。`external_owner_ref` = Dr.life+ 商户 `public_id`。一个平台令牌可绑定多个货主。
- `GET /api/platform/v1/owners/{ownerRef}`：返回 RouteFit 货主编号、状态、可用仓库列表、出库是否暂停（4.4）。
- **SKU 对应**：商户在 RouteFit 侧入仓时登记货品，RouteFit 给出 `skuId`。商户在 Dr.life+ 上架商品时，从 `GET /api/platform/v1/owners/{ownerRef}/skus?cursor=` 返回的列表中选择对应货品（字段：skuId、merchantSkuCode、名称、单位、长宽高重、状态）。
- Dr.life+ 侧建 `logistics.sku_map(product_id, routefit_sku_id, warehouse_id, unit_ratio, state)`。一个商品对应一个 SKU，一个 SKU 只能被同一商户的一个在售商品使用；SKU 被 RouteFit 停用时，商品自动下架。

### 4.2 平台出库配送单 【RouteFit 需新增】

`POST /api/platform/v1/fulfillments`，请求头 `Idempotency-Key`，请求体（所有字段必填，未声明字段拒绝）：

| 字段 | 说明 |
|---|---|
| ownerRef | 商户 public_id |
| externalOrderId | `DRL-<子订单 public_id>`。Dr.life+ 按商户拆单，一张子订单对应一张出库配送单 |
| externalRevision | 从 1 起，单调递增。只允许在 RouteFit 开始拣货前修订 |
| warehouseId | 发货仓 |
| lines[] | `{lineRef, skuId, quantity}`，1–200 行 |
| delivery | `Location`：收货地址、联系人、E.164 电话、经纬度（可空）、地标 |
| requestedDeliveryWindow | `TimeWindow`，可空，表示尽快 |
| serviceLevel | S1…S4，由 Dr.life+ 按买家选择映射 |
| paymentTerms | 固定 `PLATFORM_PREPAID`：Dr.life+ 已向买家收款，RouteFit **不得**向收货人收款，无 COD |
| declaredValue | `Money`，用于保险与赔付上限，不是收费依据 |
| sourcePlatform | 固定 `drlife` |

响应 `201`：`{fulfillmentId, version, externalOrderId, state, duplicate}`。

自动受理规则：

- `PLATFORM_PREPAID` 订单不走"人工报价、同意证据、申请派单"流程。RouteFit 校验通过后直接预占库存，进入拣货队列。
- 库存不足或 SKU 不可用时，返回 `409 INSUFFICIENT_STOCK` / `SKU_UNAVAILABLE`（带 lines 明细），不创建单据。
- 同一 externalOrderId、同一 revision、同一内容，返回原结果并带 `duplicate: true`。revision 未递增或内容冲突，返回 409 `EXTERNAL_REVISION_CONFLICT`。已开始拣货后修订，返回 409 `EXTERNAL_REVISION_LOCKED`。这三条沿用现有语义。

`GET /api/platform/v1/fulfillments/{fulfillmentId}` 和 `GET …?externalOrderId=` 返回当前状态、版本、包裹、配送方式与跟踪号、最近事件编号。这两个接口用于补查。

### 4.3 取消 【RouteFit 需新增】

`POST /api/platform/v1/fulfillments/{id}/actions/cancel`，请求头 `If-Match`、`Idempotency-Key`，请求体 `{reasonCode, note}`。

- 拣货前：直接取消，释放库存。
- 拣货后、出库前：进入 `cancel_requested`，由 RouteFit 回库后发出 `fulfillment.cancelled`。
- 已出库：返回 `409 INVALID_TRANSITION`，改走退货（售后，T-09）。

### 4.4 欠费暂停出库 【RouteFit 需新增】

`PUT /api/platform/v1/owners/{ownerRef}/outbound-hold`，请求头 `If-Match`，请求体 `{state: held|released, reasonCode: ARREARS|MANUAL, note}`。

- `held` 期间，新提交的出库单返回 `423 OWNER_OUTBOUND_HELD`。已受理的单据照常履约。
- 入仓、存储不受影响。
- 每次切换都发出 `owner.outbound_hold_changed` 事件。

### 4.5 实时运费报价 【RouteFit 需新增】

`POST /api/platform/v1/quotes`，请求体：`{ownerRef, warehouseId, lines[{skuId, quantity}], delivery: Location, serviceLevel}`。

- 响应：`{quoteId, version, fees[FeeItem], total: Money, expiresAt}`。报价有效期由 RouteFit 定，建议不少于 30 分钟，覆盖买家付款时间。
- 出库单（4.2）增加字段 `quoteId`、`quoteVersion`。RouteFit 按报价受理并按报价金额报告配送费用行（第 7 节），**事后不再按实际重算**。
- 报价过期，或订单内容与报价不符，返回 `409 QUOTE_VERSION_CONFLICT`。此时 Dr.life+ 重新报价；金额变化时需买家确认，不能静默改价。

**运费规则（项目负责人 2026-10-29 确认，D-10）：**

- 运费是正常费用。商户按商品选择"包邮"或"买家付运费"。两种情况下，运费都结算给 RouteFit。
- **买家展示运费 = RouteFit 报价 × (1 + 平台加价率)**。加价率是一个统一比例，后台可改；试点期默认 0%，与"佣金 0、平台费 0"一致。
- **买家付运费**：买家付款中的运费部分随货款一起进入托管。签收复核后，RouteFit 报价部分转入应付 RouteFit，加价部分转入平台收入，商户钱包不动。
- **商户包邮**：买家只付货款。RouteFit 报告配送费用行后，按同一公式从商户钱包扣款。
- **售后退款**：已发生的运费不退给买家，除非责任在商户或配送方，由售后判定（T-09）。判定由商户负责的，运费从商户钱包承担；判定由配送负责的，按 RouteFit 赔付规则处理。

## 5. RouteFit → Dr.life+ 事件 【RouteFit 需新增】

### 5.1 投递协议

- `POST https://adminapi.settlecore.org/v1/integrations/routefit/events`，一次一个事件。正文沿用 RouteFit 内部信封：`eventId`、`eventType`、`schemaVersion`、`occurredAt`、`aggregate{type,id,version}`、`correlationId`、`payload`。
- 请求头：
  - `X-RouteFit-Event-Id`
  - `X-RouteFit-Timestamp`（Unix 秒）
  - `X-RouteFit-Signature: v1=<hex>`，其中 hex = HMAC-SHA256(签名密钥, `<timestamp>.<原始正文字节>`)。轮换期间可以同时带多个 `v1=`，用逗号分隔。
- Dr.life+ 的校验和处理：
  1. 用常数时间比较校验签名；时间戳与本机相差超过 300 秒的请求拒绝。
  2. 用 `ops.processed_event(consumer='routefit', event_id)` 去重。
  3. 对同一 `aggregate.id`，`version` 不大于已处理版本的事件只记录、不处理。
  4. 在同一事务内写业务状态和去重记录，提交后才返回 `2xx`。
  5. 签名错误返回 401，结构错误返回 400，内部错误返回 503。
- RouteFit 侧重试：收到非 `2xx` 或超时时按指数退避重试，至少持续 24 小时；仍失败的进入死信，并在 RouteFit 运营端告警。
- **补查兜底（R-03）**：Dr.life+ 每 10 分钟扫描非终态且 30 分钟没有事件的配送单，用 4.2 的 GET 补查。补查结果按版本号合并，和事件走同一套处理逻辑。

### 5.2 事件类型

| eventType | aggregate | 关键 payload | Dr.life+ 动作 |
|---|---|---|---|
| `fulfillment.accepted` | fulfillment | fulfillmentId、externalOrderId、预占结果 | 子订单 → 已受理 |
| `fulfillment.rejected` | fulfillment | reasonCode（库存不足、地址无法配送等） | 子订单 → 履约失败，触发全额退款（T-10 `RefundHold`） |
| `fulfillment.picked` / `fulfillment.packed` | fulfillment | 包裹列表（packageId、重量、尺寸） | 更新进度 |
| `fulfillment.shipped` | fulfillment | 承运方（SELF_DRIVER / ZTO / LOCAL_CARRIER + partnerCode）、跟踪号、预计送达 | 子订单 → 配送中，通知买家（N-01） |
| `fulfillment.delivery_reported` | fulfillment | observedAt、已交付与未交付包裹 | 仅更新进度，**不结算** |
| `fulfillment.delivered_verified` | fulfillment | 复核时间、全部包裹已交付、证据摘要（5.3） | 子订单 → 已签收；开始售后观察期，期满后结算（T-10） |
| `fulfillment.delivery_failed` | fulfillment | reasonCode、下一步（重派、退回） | 通知商户与买家 |
| `fulfillment.cancelled` | fulfillment | 取消原因、库存是否已释放 | 子订单 → 已取消，退款 |
| `fulfillment.returned` | fulfillment | 回仓时间、验收结果 | 售后（T-09） |
| `owner.outbound_hold_changed` | owner | state、reasonCode | 后台显示 |
| `inventory.changed` | sku | 见第 6 节 | 更新可售数量 |
| `fee.posted` / `fee.adjusted` | fee_line | 见第 7 节 | 扣商户钱包，计提应付 |

部分交付（还有包裹没交付）不能发 `delivered_verified`，只能走 `delivery_failed` 或退回，然后由售后人工处理。这与 RouteFit 现有规则一致：部分收货不能把整单置为已交付（`fields.md:521`）。

### 5.3 签收证据 【RouteFit 需新增】

- `delivered_verified` 事件带证据摘要：`[{evidenceId, kind(PHOTO|SIGNATURE|RECIPIENT_CODE), sha256, capturedAt}]`。不带文件内容，也不带个人信息。
- `GET /api/platform/v1/fulfillments/{id}/evidence/{evidenceId}/download-url` 返回 5 分钟有效的下载链接。
- **可见范围（项目负责人 2026-10-29 确认）**：
  - **买家**可以看自己订单的签收证据。
  - **平台售后**可以看所有订单的签收证据。
  - **商户看不到**证据原件，只能看到"已签收、时间、证据件数"。
  - Dr.life+ 每次取下载链接都写审计日志；给买家的链接只在其本人会话内生成，不转发、不缓存。

## 6. 库存 【RouteFit 需新增】

- `GET /api/platform/v1/owners/{ownerRef}/inventory?skuIds=` 返回每个 SKU 每个仓的 `requestableQty` 和 `sourceVersion`。字段语义沿用 WmsStockEvent：requestable = book − reserved − quarantine。
- `inventory.changed` 事件：`{ownerRef, skuId, warehouseId, requestableQty, sourceVersion}`。Dr.life+ 按 `sourceVersion` 只升不降地合并。
- Dr.life+ 规则（R-05）：
  - 可售数量 = `requestableQty` − 本地已付款但尚未被 RouteFit 受理的数量。
  - 下单时在本地预占，付款超时或取消时释放。
  - 以 RouteFit 的 `fulfillment.accepted` 或 `rejected` 为最终结论。
  - 每日全量比对一次，差异写告警。

## 7. 费用与对账 【RouteFit 需新增】

### 7.1 金额

- RouteFit 用 `Money{amountMinor,currency}`：USD 以美分计，KHR 以瑞尔计。Dr.life+ 账务以 0.0001 USD（e4）计。
- 换算规则：e4 = 美分 × 100，精确无舍入。Dr.life+ 发给 RouteFit 的金额必须是整美分。
- 试点期只结算 USD；收到 KHR 费用行时拒绝，并告警人工处理。

### 7.2 费用行事件 `fee.posted`

payload：`{feeLineId, version, ownerRef, feeCode, direction(CHARGE|CREDIT), amount:Money, serviceDate, fulfillmentId?, skuId?, quantity?, rateRef}`。

`feeCode` 取 RouteFit 费目：

| 类别 | 费目 |
|---|---|
| 仓储 | `storage`（按立方米·日）、`inbound`、`outbound`、`packing` |
| 配送 | `base`、`weight`、`distance`、`remote`、`stairs`、`waiting`、`redelivery`、`return`、`cancel` |
| 保险 | `insurance` |

Dr.life+ 收到后（R-04）：

- **仓储类**（storage、inbound、outbound、packing）：从商户钱包扣款，金额 = RouteFit 金额。现有 6.8 仓储费分录是 DR `WALLET_AVAIL(M)` / CR `PLATFORM_HELD(storage)`，即平台自营仓储的收入；仓储改由 RouteFit 承担后，贷方应改为 `PAYABLE_CARRIER`（应付承运方，明细 = RouteFit）。原 `wms.storage_fee_daily` 日计提不再使用。
- **配送类**：金额 = 报价金额（4.5），加价率是一个统一比例。按付款方分两种情况：
  - **商户包邮**：从商户钱包扣款，金额 = RouteFit 金额 × (1 + 加价率)。分录 DR `WALLET_AVAIL(M)`；贷方分两部分，RouteFit 金额记 CR `PAYABLE_CARRIER`，加价部分记 CR `PLATFORM_HELD(delivery_margin)`。
  - **买家付运费**：运费已在托管中，签收复核后从托管转出，贷方科目同上，商户钱包不动。原 6.9、6.10 的配送费托管与签收分账是为自建配送设计的，改由 RouteFit 承担后不再使用。
- **账务设计需先修订**：上面两条改动了现有分录（6.8 改贷方，6.9、6.10 停用，新增"配送费按费用行扣款"分录）。R-04 实现前，先修订《数据库表结构与账务详细设计》第六章并经 LedgerAuditor 复核；`PAYABLE_CARRIER` 是否按承运方拆分明细，也一并确定。
- **余额不足**：照常记欠费，进入宽限期，到期后暂停出库（4.4、R-06）。
- **同一 feeLineId**：只按最新 version 生效。`fee.adjusted` 生成冲正交易与新的扣款交易，不修改原分录。

### 7.3 日结单与对账

- `GET /api/platform/v1/statements/{serviceDate}` 返回当日全部费用行，以及按货主、按费目汇总的金额与行数，附 `statementVersion`。
- Dr.life+ 每日 06:00（金边时间）拉取前一日结单，与本地已入账的费用行逐行比对。三类差异写对账异常并告警：缺行、多行、金额不符。异常期间暂停向 RouteFit 付款，不冻结商户。
- **与 RouteFit 的结算（项目负责人 2026-10-29 确认）**：两个平台属于同一家公司，**只记内部往来账，不实际转账**。
  - `PAYABLE_CARRIER`（明细 = RouteFit）作为内部往来余额，每月核对一次："应付 RouteFit 余额 = 累计日结单合计 − 已核销"。双方财务确认后，在后台登记内部核销，走现有出款复核（双人）。
  - 内部核销不经 `BANK_DEDICATED`，需要一种新的交易类型或 `adjust` 的专用子类，在账务设计修订时确定，经 LedgerAuditor 复核。
  - 客户资金在银行专用账户（或 KESS 待结算）中，不因内部核销而减少。内部核销只改变平台内部归属，不得动用客户本金。

## 8. Dr.life+ 侧状态与数据（供 R-02…R-06 实现）

- 子订单配送状态：`awaiting_payment → paid → submitted → accepted → in_fulfillment → shipped → delivered_verified → settled`，旁路状态有 `rejected`、`cancelled`、`delivery_failed`、`returning`、`returned`、`exception`。所有迁移只由本契约第 5 节的事件或补查结果驱动。
- 新表放在 `logistics` 模式，读写角色为 `logistics_rw`：
  - `routefit_owner`：商户与货主的对应关系，以及出库暂停状态。
  - `sku_map`：见 4.1。
  - `shipment`：子订单、externalOrderId、revision、fulfillmentId、状态、最近 aggregate 版本、最近事件时间。
  - `shipment_event`：只追加，保存原始事件正文的哈希与关键字段，不保存个人信息。
  - `fee_line`：feeLineId、version、对应账务交易号。
  - `recon_run`、`recon_issue`。
- **出站调用（R-02）**：子订单付款与"待提交配送"在同一事务里写入 outbox，由 worker 调用 4.2。
  - `Idempotency-Key = drl-<shipment public_id>-r<revision>`。
  - 网络超时后不重新生成键，先用 GET 补查。
  - 可重试错误（429、503，或 retryable=true）按指数退避重试；不可重试错误进入人工处理队列并告警。
- **结算时点（T-10）**：`delivered_verified` 之后开始售后观察期，**默认 7 天**（项目负责人 2026-10-29 确认，后台可改）。期满且没有未结售后时，执行 `SettleHold` 把货款结算给商户。有未结售后时，等售后结案后再结算。

## 9. 测试与验收（G-11）

1. **契约测试**：双方各自对照本文的 JSON Schema 做生产方和消费方测试。Dr.life+ 侧的本地替身必须能复现所有错误码与事件乱序、重复。
2. **联调**：在 RouteFit 测试环境跑通以下全流程：建单 → 受理 → 拣货 → 出库 → 签收复核 → 结算；取消（拣货前、拣货后）；拒单退款；欠费暂停与恢复；费用行入账与日结单对账一致。每一项保留脱敏的请求与响应证据。
3. **故障演练**：
   - 回调端点宕机 30 分钟后恢复：事件不丢、不重复处理。
   - 补查与事件同时到达：状态正确。
   - 签名错误、时间戳过期：被拒。
4. **对账**：连续 3 天日结单与本地入账零差异。

## 10. 已确认事项（项目负责人 2026-10-29）

| # | 事项 | 结论 |
|---|---|---|
| Q1 | 商户身份 | RouteFit 新增平台通道：一个令牌代多个商户（4.1） |
| Q2 | 付款后是否自动发货 | 平台预付单自动受理，不走人工报价；库存不足或地址不可配送时自动拒单并退款（4.2） |
| Q3 | 售后观察期 | 7 天，后台可改（第 8 节） |
| Q4 | 平台加价 | 报价已含加价，统一比例，后台可改；试点默认 0%（4.5） |
| Q5 | 买家运费（D-10） | 运费是正常费用：商户选择包邮或买家付运费，运费都结算给 RouteFit；买家付运费时，下单向 RouteFit 实时询价（4.5） |
| Q6 | 签收证据可见范围 | 买家（仅本人订单）与平台售后可看，商户不可看（5.3） |
| Q7 | 与 RouteFit 的结算 | 同一公司，只记内部往来账，不实际转账，每月对账核销（7.3） |

## 11. 交给 RouteFit 侧的派卡清单（对应清单 E-1…E-6）

| 编号 | 内容 | 本文章节 |
|---|---|---|
| E-1 | 修复迁移（RF-PM-007），部署测试环境 | — |
| E-5 + G1 | 平台通道令牌与多货主绑定，以及货主与 SKU 查询 | 3、4.1 |
| G2/G3 | 平台出库配送单与自动受理 | 4.2 |
| G10 | 实时运费报价（含按距离计价） | 4.5 |
| G4 | 取消接口 | 4.3 |
| E-3 | 出库暂停 | 4.4 |
| E-2 | 出站事件（签名、重试、死信）与证据下载 | 5 |
| E-6 | 库存查询与变更事件 | 6 |
| G8 | 费用行事件与日结单 | 7 |
