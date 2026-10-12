# @drlife/api-client

该目录保存由 `backend/api/openapi.yaml` 生成的共享 TypeScript 类型，并提供基于 `openapi-fetch` 的薄客户端封装。

```ts
import { createApiClient } from "@drlife/api-client";

const api = createApiClient({
  getToken: () => sessionStorage.getItem("token"),
  getLanguage: () => "en"
});
```

接口变更先修改 OpenAPI 规范，再从 `backend` 运行 `make generate-all`。不要手工修改 `src/schema.d.ts`。

开发环境未设置 `VITE_API_BASE_URL` 时使用 `http://localhost:8080`。生产环境必须显式设置，例如部署配置中的 `https://adminapi.settlecore.org`；客户端代码不内置生产地址。
