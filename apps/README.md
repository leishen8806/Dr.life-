# 前端工作区

## 环境与安装

- Node.js 22（见根目录 `.nvmrc`）
- pnpm 10.28.2，由 Corepack 按根目录 `packageManager` 固定

```bash
corepack enable
pnpm install --frozen-lockfile
```

## 应用

| 应用          | 启动命令                             | 本地地址                |
| ------------- | ------------------------------------ | ----------------------- |
| 买家 Mini App | `pnpm --filter @drlife/buyer dev`    | `http://localhost:5173` |
| 商家 Mini App | `pnpm --filter @drlife/merchant dev` | `http://localhost:5174` |
| 司机 Mini App | `pnpm --filter @drlife/driver dev`   | `http://localhost:5175` |
| 平台后台      | `pnpm --filter @drlife/admin dev`    | `http://localhost:5176` |
| 物流仓储后台  | `pnpm --filter @drlife/wms dev`      | `http://localhost:5177` |

本地后端的 `CORS_ALLOWED_ORIGINS` 需要包含上述五个来源。Mini App 在普通浏览器里会明确显示“开发模式”，并使用不含真实身份或令牌的 mock 数据。

五个应用共用 `@drlife/ui` 的语言切换控件。F-07 阶段语言偏好仅保存在浏览器；A-04 接入后再登记到账户。构建前会自动校验三语词条与用户可见字符串是否仍被硬编码。

## 配置

开发环境未设置 `VITE_API_BASE_URL` 时，接口客户端连接 `http://localhost:8080`。生产部署必须设置 `VITE_API_BASE_URL`，当前约定值为 `https://adminapi.settlecore.org`；生产地址不写入客户端源代码。

## 新增应用或包

应用放在 `apps/<name>`，共享包放在 `packages/<name>`，并提供需要的 `typecheck`、`build`、`lint`、`test` 或 `i18n:check` 脚本。工作区通配符会自动发现目录，内部依赖使用 `workspace:*`。
