# Issue #136 协议同意流程验收

## 范围

- 基线：最新 `main`（2026-11-06 UTC+7）
- 分支：`fix/136-consent-update-modal`
- 覆盖买家端与商家端共享 `StartupGate`，接口路径由 Telegram session 的角色类型决定

## 验收矩阵

| 验收项 | 结果 | 证据 |
| --- | --- | --- |
| 2008/未同意状态进入协议页 | PASS | session 读取 `/v1/{buyer|merchant}/me/consents`，`needs_reaccept` 映射到 consent phase |
| 隐私政策与用户协议两项 | PASS | 共享三语 `ConsentView`，展示当前版本、正文和必选状态 |
| 同意后重试原业务流程 | PASS | 接受接口成功后重新读取 consent state，StartupGate 才进入 ready/业务页 |
| 拒绝/未勾选不能进入业务页 | PASS | 接受按钮在所有必选项勾选前保持禁用；未同意时业务 children 不渲染 |
| 协议更新重新确认 | PASS | `needs_reaccept` 对版本变化重新建立必选勾选 |
| 三语词条 | PASS | zh/en/km 新增 consent 文案，Khmer 保留 `[TODO-KM-REVIEW]` 标记 |
| 真实 Telegram Mini App / 已发布法律文档 | NOT_RUN | 当前无真实 initData、线上服务和设备验收权限 |

## 门禁

- `pnpm typecheck`：PASS
- `pnpm lint`：PASS
- `pnpm i18n:check`：PASS
- `pnpm test`：PASS（网站 26、UI 16、买家 69、商家 48，另含 api-client 测试）
- `pnpm build`：PASS

未执行合并、部署、自审或生产数据库操作。
