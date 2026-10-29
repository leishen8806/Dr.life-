# RouteFit 本地替身

`routefitmock` 是仅用于 Go 集成测试的内存 HTTP 服务，实现《Dr.life+与Dr.RouteFit对接契约》第 4–7 节。它不读取真实密钥、不连外部 RouteFit，也不包含账务或鉴权逻辑。

```go
m := routefitmock.New(routefitmock.Config{CallbackSecret: "test-only"})
m.AddOwner(routefitmock.Owner{OwnerRef: "merchant-1", WarehouseIDs: []string{"wh-1"}})
m.AddSKU("merchant-1", routefitmock.SKU{ID: "sku-1", State: "active"}, "wh-1", 10)
server := httptest.NewServer(m.Handler())
defer server.Close()
```

测试可用 `AdvanceFulfillment` 生成履约事件，再用 `Deliver` 或 `DeliverMany` 投递到回调地址。`DeliveryOptions` 可注入重复投递、倒序、延迟、错误签名、过期时间戳、回调超时和非 2xx 后重试；`SetFault` 可让下一个 HTTP 请求返回指定 5xx 或延迟。

| 契约项 | 路由 / API | 覆盖错误码或字段 |
| --- | --- | --- |
| 4.1 | `GET owners/{ownerRef}`、`GET skus` | `OWNER_NOT_FOUND`；货主、仓、SKU 字段 |
| 4.2 | `POST/GET fulfillments` | `Idempotency-Key`、revision、`INSUFFICIENT_STOCK`、`SKU_UNAVAILABLE`、`EXTERNAL_REVISION_*` |
| 4.3 | `POST fulfillments/{id}/actions/cancel` | `INVALID_TRANSITION`、库存释放 |
| 4.4 | `PUT owners/{ownerRef}/outbound-hold` | `OWNER_OUTBOUND_HELD`（423） |
| 4.5 | `POST quotes` | `quoteId`、version、`expiresAt`、`QUOTE_VERSION_CONFLICT` |
| 5.1–5.2 | `Deliver`、`DeliverMany` | `X-RouteFit-*` 与 HMAC-SHA256 `v1=` 信封 |
| 5.3 | `GET evidence/{evidenceId}/download-url` | 5 分钟 `expiresAt`、`EVIDENCE_NOT_FOUND` |
| 6 | `GET inventory` | `requestableQty`、`sourceVersion` |
| 7.3 | `GET statements/{serviceDate}` | `feeLines`、按货主/费目摘要、`statementVersion` |

所有错误响应为 `{"error":{"code":"...","message":"...","messageKey":"...","retryable":false,"requestId":"routefitmock","fields":[]}}`；版本冲突另带 `currentVersion`。金额字段只使用 `int64 AmountMinor`，不使用浮点数。

也可独立启动：`go run ./cmd/routefitmock -listen 127.0.0.1:18080 -scenario scenario.json`。独立模式初始为空；`-scenario` accepts `owners`, `skus` (`ownerRef`, `sku`, `warehouseId`, `quantity`) and `feeLines`.

Standalone mode alone enables these scenario-control endpoints: `POST /__mock/fulfillments/{id}/advance` (`{"state":"picked"}`), `POST /__mock/deliver` (`callback`, `event`, `options`), and `POST /__mock/fault` (`status`, `count`, `delay`). They are unavailable from the embedded test handler. It only runs locally or in test environments.
