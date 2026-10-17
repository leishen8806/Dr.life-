# @drlife/api-client

该目录保存由 `backend/api/openapi.yaml` 生成的共享 TypeScript 类型，并提供基于 `openapi-fetch` 的客户端封装。

```ts
import { createApiClient } from "@drlife/api-client";

const api = createApiClient({
  getToken: () => sessionStorage.getItem("token"),
  getLanguage: () => "en"
});
```

客户端为每个请求加入 `X-Request-ID`，写请求自动加入 `Idempotency-Key`。重试会复用同一请求的幂等键；UI 应用可为一个按钮动作保留同一键：

```ts
const action = createIdempotentAction();
await api.POST("/v1/example", action.request({ /* JSON body */ }));
// 用户开始新的动作后：action.reset()
```

| 策略 | 默认值 | 覆盖方式 |
| --- | --- | --- |
| 超时 | 读 8s、写 12s、`X-Request-Class: heavy` 30s | `resilience.timeout` |
| 重试 | 最多 3 次、总时长 ≤15s、全抖动 | `resilience.retry` |
| 重试状态 | 网络错误、408、429、503、带 `Retry-After` 的 409 | 仅 GET 或带幂等键的写请求 |
| 熔断 | 同一端点类别连续 5 次 5xx/网络错误，10s | `resilience.breaker`（上限 60s） |
| GET 去重 | 同 URL + 语言的在途请求 | 内建，写请求不去重 |

错误请使用 `ApiError.kind` 和 `ApiError.messageKey` 显示 `packages/i18n` 的词条，不能直接显示后端 `messages`。`redactSensitive()` 可在客户端上报前移除令牌、initData 和电话号码。`loginStormDelay(true)` 只返回 0–2 秒随机延迟，调用方自行决定是否等待。

接口变更先修改 OpenAPI 规范，再从 `backend` 运行 `make generate-all`。不要手工修改 `src/schema.d.ts`。

开发环境未设置 `VITE_API_BASE_URL` 时使用 `http://localhost:8080`。生产环境必须显式设置，例如部署配置中的 `https://adminapi.settlecore.org`；客户端代码不内置生产地址。
