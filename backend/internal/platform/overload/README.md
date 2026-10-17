# 路由分舱（Bulkhead）

`critical`、`auth`、`write`、`read`、`heavy` 各有并发上限；`health`（`/healthz`、`/readyz`）不受限。全局上限默认 512，非 critical 请求另受 75% 总量限制，因此 critical 在其余类别打满时仍可使用预留的 25%，并可在必要时使用全部总量。

默认截止时间为 critical/write 10 秒、auth/read 5 秒、heavy 30 秒；许可最多等待 100ms，失败返回统一 503 与 1–3 秒 `Retry-After`。路径类别先按 `BULKHEAD_ROUTE_CLASSES=/prefix=class` 覆盖，再使用内置规则；`/v1/public/ping` 为 read。

默认并发为 critical 512、auth 64、write 128、read 256、heavy 32。这些是待 S-05 压测校准的初值，不是生产容量承诺。关闭时先标记 draining，使新业务请求快速返回 503；health 继续可访问而 ready 变为不就绪。

## Adaptive brownout (S-02d)

`NewBrownout` samples three signals on a fixed interval:

- rolling P95 request latency (fed by the bulkhead middleware, 30 second window);
- database connection-pool acquire wait;
- in-flight requests as a fraction of the global bulkhead capacity.

When any signal crosses its breach threshold for `EscalateAfter`, the level rises by one (maximum 3)
and the `heavy` concurrency limit is halved per level, never below 1. `critical` capacity is never
changed. Recovery requires every signal to stay below its recovery threshold for `RecoverAfter`;
levels then drop one at a time. Signals in the hysteresis band reset both timers to prevent flapping.

`overload.Level(ctx)` exposes the current level (`0` is normal) to handlers, and every transition is
logged at INFO and exported as `drlife_brownout_level`, `drlife_brownout_heavy_limit`, and
`drlife_brownout_transitions_total{from,to}`. Configure it with the `BROWNOUT_*` variables in
`backend/.env.example`; the clock and signal source are injectable for tests.