# Issue #142 buyer cart acceptance

时间：2026-11-06 14:03 (UTC+7)

范围：买家端购物车页（US-01，模拟数据）。本实现不新增后端接口、不提交真实订单；商户分组使用产品 origin 映射的 mock merchant，仅用于拆单预览。

## 验收

- PASS — 从商品详情加入采购单，默认使用最低起订量（MOQ）。
- PASS — 阶梯价在 1、100、500 边界选择正确，行金额与总金额使用 e4 bigint 固定点计算，无浮点金额计算。
- PASS — 数量可通过输入框和 +/- 调整；数量为 0 时移除该行。
- PASS — 低于 MOQ、超过 mock 可用库存（默认 100）时拒绝更新并保留原数量。
- PASS — 按中国直供/本地来源分组展示 mock 商户小计，并展示拆单预览总计。
- PASS — 中文、英文、Khmer 文案均接入 `packages/i18n`；Khmer 新文案按仓库规则保留 `TODO-KM-REVIEW` 标记。
- PASS — 空购物车提供返回商品浏览入口；真实提交按钮明确禁用并标注 mock 范围。
- PASS — 移动端数量控件、长文案和分组布局增加换行及窄屏适配。

## 本地门禁

- PASS — `pnpm typecheck`
- PASS — `pnpm lint`
- PASS — `pnpm i18n:check`（仅既有/规则允许的 Khmer 审校提示）
- PASS — `pnpm build`
- PASS — `pnpm test`（全仓：buyer 72 tests；website 26；api-client 47；i18n 8+3；ui 16；merchant 48）
- PASS — `git diff --check`

## 未执行事项

- NOT_RUN — 真实 API 购物车持久化、库存实时校验、订单提交与支付：Issue 明确要求模拟数据且无后端依赖。
- NOT_RUN — Telegram Mini App 真机验收：本卡范围未提供可用后端/测试环境，也未获部署授权。
- NOT_RUN — 合并、部署、生产写入：按工作流禁止自行执行，等待负责人审阅与授权。
