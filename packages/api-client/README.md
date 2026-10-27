# @drlife/api-client

该目录保存由 `backend/api/openapi.yaml` 生成的共享 TypeScript 类型，并提供基于 `openapi-fetch` 的客户端封装。

```ts
import { createApiClient } from "@drlife/api-client";

const api = createApiClient({
  auth: { store },
  getLanguage: () => "en"
});
```

`store` 来自下面的「令牌与 step-up」。`getToken` 是旧的手工模式，和 `auth` 互斥，同时传会在创建时抛错。**不要**把令牌放进 `localStorage`、`sessionStorage` 或 Cookie。

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

## 令牌与 step-up

访问令牌与刷新令牌**只存在内存里**（`createTokenStore` 的闭包变量），不写 `localStorage`、`sessionStorage`、`document.cookie`、IndexedDB，也不挂到 `globalThis`。登录实现由各应用注入。

```ts
import { createApiClient, createTokenStore, type AuthProvider } from "@drlife/api-client";

const provider: AuthProvider = {
  login: async () => fetchTokensWithInitData(), // Mini App 用最新 initData；Web 回登录页
  refresh: async (refreshToken) => fetchRefreshedTokens(refreshToken),
  logout: async () => revokeSession()
};

const store = createTokenStore(provider);
store.onReloginRequired(() => showLoginAgain()); // 会话作废，需要重新登录

const api = createApiClient({
  auth: {
    store,
    stepUp: async ({ requestId }) => askUserForVerification(requestId) // 返回 null 表示用户取消
  }
});

await store.login(); // 或 store.setTokens(pair)
```

| 行为 | 规则 |
| --- | --- |
| 触发刷新 | 只有 `401` + 错误码 `2001`。`2003` / `2004` 不触发刷新 |
| 单飞刷新 | 同一时刻只有一个 `refresh` 在途，其余请求共享结果后各自重放 |
| 重放次数 | 每个请求最多重放一次（刷新或 step-up 各一次），不成环 |
| 幂等键 | 重放沿用首次的 `Idempotency-Key`；`X-Request-ID` 重新生成 |
| 刷新失败 | 清空令牌 + 触发 `relogin-required` 监听器（同一批并发只触发一次），并把原始 `ApiError` 抛给调用方 |
| 没有刷新令牌 | 不调用 `refresh`，直接走刷新失败路径 |
| step-up | 收到 `2003` 调用注入的 handler，拿到令牌后带 `X-Step-Up-Token` 重放一次；`2004` 不再重试 |

`2003` 只重放一次是硬约束：网关对 step-up 令牌的 JTI 做一次性消费（`gateway/stepup.go`，TTL 5 分钟），同一个令牌重试必然失败。二次验证的界面由各应用实现，本包只提供 `StepUpHandler` 接口。

`auth` 层位于 `createResilientFetch` 内侧：写请求的幂等键在外层生成，内层重放时原样带上，所以同一次用户动作不会产生第二个幂等键。

错误请使用 `ApiError.kind` 和 `ApiError.messageKey` 显示 `packages/i18n` 的词条，不能直接显示后端 `messages`。`redactSensitive()` 可在客户端上报前移除令牌、initData 和电话号码。`loginStormDelay(true)` 只返回 0–2 秒随机延迟，调用方自行决定是否等待。

接口变更先修改 OpenAPI 规范，再从 `backend` 运行 `make generate-all`。不要手工修改 `src/schema.d.ts`。

开发环境未设置 `VITE_API_BASE_URL` 时使用 `http://localhost:8080`。生产环境必须显式设置，例如部署配置中的 `https://adminapi.settlecore.org`；客户端代码不内置生产地址。
