# B12 · WMS（物流仓储 Web）接口契约设计

> 2026-11-21，依据 B12 派单。本文和 `backend/api/openapi.yaml` 只定义我方契约，不实现处理器、不加迁移、不切换 `apps/wms` 的 mock adapter、不引入外部 WMS 字段猜测。所有数量字段为整数；本设计不定义金额。

## 1. 现状盘点：页面 → mock 字段 → 目标接口

`apps/wms/src/api/mock.ts` 是当前开发态数据源，`apps/wms/src/api/index.ts` 在 DEV 环境继续返回 `mockApi`，生产态仍由 `realApi` 占位。`apps/wms/src/view-model.ts` 已表达前端可见的状态转换，但不代表服务端状态机已经实现。

| 页面 / 区域 | 当前 mock / view-model 字段 | B12 目标接口 | 契约要点 |
| --- | --- | --- | --- |
| 在途进货列表 | `Receiving.reference`, `scheduled`, `status`, `recommendedLocation` | `GET /v1/wms/inbound/shipments` | 分页、排序、仓库范围；联系人电话与地址返回脱敏值。 |
| 在途进货详情 | `Receiving.id`, `reference`, `scheduled`, `status` | `GET /v1/wms/inbound/shipments/{public_id}` | 只返回当前 WMS scope 可见的运输/到货信息。 |
| 入仓预约 / 收货列表 | `Receiving.id`, `reference`, `scheduled`, `measured`, `volume` | `GET /v1/wms/inbound/receipts` | `scheduled_quantity`、`measured_quantity`、`measured_volume_cm3` 均为整数；差异状态由服务端决定。 |
| 实测收货 | `receive(id, measured, volume, threshold)`, `receiveRecord` | `POST /v1/wms/inbound/receipts/{public_id}/receive` | `Idempotency-Key`、`x-audit: wms.receiving.receive`；扫码文件只引用已有 file public id。 |
| 库存列表 | `Inventory.product`, `zone`, `location`, `quantity`, `available`, `locationTotal`, `locations`, `movement` | `GET /v1/wms/inventory` | 支持 product/zone/location 筛选；数量与库位合计均为整数，前端可据此标红不一致。 |
| 库存详情 / 库位 | `Inventory.locations`, `location`, `locationTotal` | `GET /v1/wms/inventory/{public_id}` | 返回 `locations[]` 明细和 `location_total_quantity`；不下放跨仓库数据。 |
| 盘点调整 | `canAdjust`, `canSubmitAdjustment`, `adjust(id, quantity, reason)` | `POST /v1/wms/inventory/{public_id}/adjustments` | 整数目标数量、必填原因、证据 file 引用；`Idempotency-Key` 与 `wms.inventory.adjust` 审计。 |
| 库存流水 | `Inventory.movement` | `GET /v1/wms/inventory/{public_id}/movements` | 只读、只追加流水；`quantity_delta` 为整数，不定义删除/覆盖。 |
| 出库 / 拣货列表 | `PickTask.id`, `reference`, `items`, `deliveryMethod`, `driver`, `phone`, `status` | `GET /v1/wms/outbound/picks` | 联系电话脱敏；按仓库权限分页。 |
| 拣货详情 | `PickTask` 全字段 | `GET /v1/wms/outbound/picks/{public_id}` | 返回当前任务和脱敏收件信息。 |
| 打包 | `pack(id)`, `packTask`（`ready → packed`） | `POST /v1/wms/outbound/picks/{public_id}/pack` | 幂等写入、`wms.outbound.pack` 审计；状态冲突返回 409。 |
| 出库 / 交接 | `assign(id, driver, deliveryMethod)`, `assignTask`（`packed → dispatched`） | `POST /v1/wms/outbound/picks/{public_id}/handoff` | 选择 `zto`/`inhouse`，可引用司机和运单；不在本卡定义配送费托管或配送轨迹。 |
| 异常列表 | 当前 mock 未建模；view-model 只有差异状态 | `GET /v1/wms/exceptions` | 覆盖数量差异、破损、缺货、库位冲突、适配器错误、交接失败。 |
| 异常详情 | 当前 mock 未建模 | `GET /v1/wms/exceptions/{public_id}` | 限 WMS scope，描述不得包含完整手机号或凭据。 |
| 异常处理 | 当前 mock 未建模 | `POST /v1/wms/exceptions/{public_id}/resolve` | 需要幂等键和 `wms.exception.resolve` 审计；证据仍只引用 file public id。 |
| 接口状态 | 当前 mock 未建模；`realApi` 为生产占位 | `GET /v1/wms/integrations/status` | 仅返回适配器状态/时间/三语提示，不返回密钥或外部原始 payload。 |

## 2. 统一契约规则

- 所有新路径使用 `x-roles: [wms]`。非 WMS 角色或无仓库数据范围必须由服务端返回 403；前端隐藏菜单不构成权限控制。
- 列表统一使用 one-based `page` 与 `page_size`（最大 100），响应包含 `items`、`total`、`page`、`page_size`；接口状态列表因其小规模固定返回 `items`。
- 所有写接口带必填 `Idempotency-Key`，并声明 `x-audit`。重复相同请求可重放已保存响应；同一 key 对不同请求体返回冲突/处理中错误。
- 错误码：未登录 401、角色或数据范围不符 403、输入错误 400、资源不存在 404、状态/幂等冲突 409、限流 429、服务/适配器故障 500/503。
- 数量使用整数 `int64`；收货体积使用整数 `measured_volume_cm3`，避免浮点误差。金额不出现在本 B12 契约中；若后续发现仓储费或配送费依赖，本卡只记录发现，不自行定义字段。
- 收货地址、联系人电话、司机电话均只返回脱敏值；文件只引用已有 `public_id`，不扩大文件下载权限。
- 库存流水是追加式事实记录；盘点调整写入原因和证据，不能通过覆盖库存快照绕过流水。

## 3. 外部 WMS/WCS/中通适配器形状

本卡只定义我方边界，外部系统字段仍为“待规范”。实现时建议适配器暴露以下能力（名称为设计占位，不是已实现 Go 接口）：

```text
WmsAdapter
  ListInbound(ctx, scope, cursor) -> inbound snapshots
  Receive(ctx, scope, inbound_id, measured quantities) -> receipt result
  ListInventory(ctx, scope, filter) -> inventory snapshots
  CreateAdjustment(ctx, scope, inventory_id, integer target, reason) -> movement result
  ListOutbound(ctx, scope, filter) -> pick snapshots
  Handoff(ctx, scope, pick_id, delivery method) -> handoff result
  Health(ctx, scope) -> status only
```

适配器调用必须有固定超时、有限重试（只对明确可重试的网络/5xx 错误）、指数退避与熔断；每次调用记录 request id、适配器名、耗时、结果类别和重试次数，不记录凭据、原始手机号或完整外部 payload。熔断期间 API 返回 503/“稍后更新”，不得伪造库存或收货状态。

外部 WMS/WCS/中通的 SKU、库位、批次、回调签名、分页游标和状态枚举均待提供正式规范后再映射；本 PR 不猜这些字段，也不连接测试/生产服务器。

## 4. Owner 亲审与后续实现边界

1. 库存主从关系：自建库存与采购 WMS/WCS 谁是事实源、冲突如何裁决。
2. 盘点差异阈值、收货确认、自动上架与计费开始的状态及权限。
3. 出库交接与 RouteFit 配送状态联动；配送费托管不在本卡设计。
4. 仓库、区域、班组和员工权限的数据范围；员工是否可盘点、收货、处理异常。
5. 外部 WMS/WCS/中通正式接口、回调安全、超时/重试上限和降级策略。
6. 数量单位、包装换算、批次/效期、破损与缺货的审计规则。

## 5. 本 PR 明确不做

- 不写 Go handler、数据库 schema/migration、前端 real adapter、页面或 mock 切换。
- 不设计仓储费、配送费、钱包、KESS、融资或任何金额字段。
- 不实现外部适配器、不猜外部字段、不连接服务器、不做真实 WMS/中通联调。

