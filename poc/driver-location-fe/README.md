# 司机端定位与弱网 PoC（P0-9 前端部分）

> 仅用于 PoC 验证：只连接本机 mock 后端，不连接任何真实服务，也不是生产代码。
> 本目录不修改 `backend/**`、`packages/ui/**`，不新增 npm 依赖。

结论见 [`RESULT.md`](./RESULT.md)，验收证据见 [`evidence/`](./evidence/)。

## 目录

| 路径 | 作用 |
| --- | --- |
| `src/main.ts` | 页面装配：采样、队列、补传、三语切换 |
| `src/location-sampler.ts` | 固定间隔调用定位并把点写入队列 |
| `src/location-queue.ts` | localStorage 持久化队列，串行补传，失败保留 |
| `src/http-uploader.ts` | POST 上报，`Idempotency-Key` = 定位点 id |
| `src/mock-backend.ts` | 按 id 去重的内存后端（测试与 mock server 共用） |
| `server/mock-server.ts` | 仅测试用的本地 HTTP mock（默认 `127.0.0.1:8787`） |
| `public/sw.js` | 离线 shell：构建时注入版本号与预缓存清单 |
| `tests/location-queue.test.ts` | AC-2/3/4 及并发、失败保留测试 |

## 运行

前置：仓库 pnpm 工作区依赖已安装（F-06）；Node 需支持内置 TypeScript type stripping（本机 Node 25.9.0 实测可用）。不新增依赖、不改 `package.json`。

在仓库根目录开两个终端：

```powershell
# 1) 本地 mock 后端
node poc/driver-location-fe/server/mock-server.ts

# 2) 构建并预览（Service Worker 只在生产构建注册）
pnpm exec vite build --config poc/driver-location-fe/vite.config.ts
pnpm exec vite preview --config poc/driver-location-fe/vite.config.ts
```

打开 `http://localhost:5180/?fakeLocation=1&intervalMs=1000`。

开发模式（`pnpm exec vite --config poc/driver-location-fe/vite.config.ts`，端口 5177）只用于调试界面；离线刷新验证必须用上面的 preview 构建。

端口：dev `5177`、preview `5180`、mock `8787`（`/v1` 与 `/__mock` 由 Vite 代理到 mock）。

## URL 参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `fakeLocation=1` | 关闭 | 使用递增的模拟坐标，不请求真实定位权限 |
| `intervalMs` | `5000` | 采样间隔，限制在 `1000`–`60000` 毫秒 |
| `endpoint` | `/v1/driver/locations` | 上报地址，只能指向本地 mock |

## Mock 接口

| 方法与路径 | 作用 |
| --- | --- |
| `GET /__mock/health` | 健康检查 |
| `GET /__mock/stats` | 返回 `received`、`uniqueKeys`、`duplicateAttempts` 与已收 id |
| `POST /__mock/reset` | 清空 mock 数据 |
| `POST /v1/driver/locations` | 接收定位点；要求 `Idempotency-Key` 且等于 body 的 `id`，重复键返回 200 + `duplicate` |

统计口径：`received` = 去重后保存的条数，`uniqueKeys` = 去重键数量，`duplicateAttempts` = 重复投递次数。

## 页面行为

- 每 `intervalMs` 调用一次 `navigator.geolocation.getCurrentPosition`（高精度、10 秒超时），生成 `id`、`lat`、`lng`、`accuracyM`、`capturedAt` 后写入队列。
- 每次采样后串行补传；某个点失败就停止本轮，已成功的点出队，失败点及后续点保留。
- 队列写入 localStorage（`drlife.driver.location.queue.v1`），刷新不丢；上限 1000 条，超出丢弃最旧的点。
- 触发补传：每个采样周期、`online` 事件、页面重新可见、点击“立即补传”。
- 上报请求头 `Idempotency-Key: <point.id>`，请求体 `id` 同值；mock 按 id 去重。
- 三语（中文 / English / ខ្មែរ）词条在 `src/copy.ts`，语言选择存 localStorage。
- 生产构建注册 Service Worker，预缓存 `index.html` 与当次构建的 JS/CSS；离线导航回退到缓存 shell。`/v1`、`/__mock` 请求不经过 Service Worker。

## 本地校验

```powershell
pnpm exec vitest run --config poc/driver-location-fe/vitest.config.ts
pnpm exec tsc -p poc/driver-location-fe/tsconfig.json --noEmit
pnpm exec vite build --config poc/driver-location-fe/vite.config.ts
```

## 离线复现步骤

1. 在线打开页面，等待 Service Worker 激活并完成一次上报（页面底部显示 build id）。
2. 停止 mock 与 preview 进程（比 DevTools 的 offline 更严格；本机实测 `navigator.onLine` 可能仍为 `true`）。
3. 用 Ctrl+F5 / `Page.reload({ ignoreCache: true })` 强刷：页面应由 Service Worker 返回，JS/CSS 的 `transferSize` 为 0，队列中的 id 全部保留。
4. 重启 mock 与 preview，点击“立即补传”：mock 收到离线期间的全部 id，且 `duplicateAttempts` 不增加。

## 边界

本 PoC 不验证真机定位精度、后台/锁屏定位、耗电、真实后端鉴权与 Telegram initData；详见 `RESULT.md`。
