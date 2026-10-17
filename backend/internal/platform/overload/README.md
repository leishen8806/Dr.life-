# 路由分舱（Bulkhead）

`critical`、`auth`、`write`、`read`、`heavy` 各有并发上限；`health`（`/healthz`、`/readyz`）不受限。全局上限默认 512，非 critical 请求另受 75% 总量限制，因此 critical 在其余类别打满时仍可使用预留的 25%，并可在必要时使用全部总量。

默认截止时间为 critical/write 10 秒、auth/read 5 秒、heavy 30 秒；许可最多等待 100ms，失败返回统一 503 与 1–3 秒 `Retry-After`。路径类别先按 `BULKHEAD_ROUTE_CLASSES=/prefix=class` 覆盖，再使用内置规则；`/v1/public/ping` 为 read。

默认并发为 critical 512、auth 64、write 128、read 256、heavy 32。这些是待 S-05 压测校准的初值，不是生产容量承诺。关闭时先标记 draining，使新业务请求快速返回 503；health 继续可访问而 ready 变为不就绪。
