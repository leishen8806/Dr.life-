# @drlife/api-client

该目录保存由 `backend/api/openapi.yaml` 生成的共享 TypeScript 类型。本任务只提供最小包文件，不创建 pnpm workspace。

```ts
import type { components, operations, paths } from "@drlife/api-client";
```

接口变更先修改 OpenAPI 规范，再从 `backend` 运行 `make generate-all`。不要手工修改 `src/schema.d.ts`。
