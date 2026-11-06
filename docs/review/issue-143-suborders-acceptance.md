# Issue #143 merchant sub-order acceptance

时间：2026-11-06 14:19 (UTC+7)

范围：商家端子订单列表与详情页，只读接口桩/模拟数据；不接真实订单接口，不承担服务端权限安全。

## 验收

- PASS — 子订单列表按待付款、已付款、已发货、已送达状态筛选。
- PASS — 列表展示子订单号、买家、商品摘要、状态和金额。
- PASS — 详情展示商品行、数量、单价、金额和配送状态。
- PASS — 金额用 bigint e4 固定点格式化，无浮点运算。
- PASS — 商家端三语文案接入 `packages/i18n`；Khmer 新文案保留 `TODO-KM-REVIEW`。
- PASS — 只读展示明确使用接口桩数据；前端筛选不替代服务端权限校验。

## 本地门禁

- PASS — `pnpm typecheck`
- PASS — `pnpm lint`
- PASS — `pnpm i18n:check`（164 keys；仅 Khmer 审校提示）
- PASS — `pnpm build`
- PASS — `pnpm test`（merchant 50 tests；buyer 69；website 26；api-client 47；i18n 8+3；ui 16）
- PASS — `git diff --check`

## 未执行事项

- NOT_RUN — 真实子订单 API、服务端商户隔离、真实配送状态和订单接口联调：本卡明确为模拟数据，后端接口未提供。
- NOT_RUN — Telegram Mini App 真机/线上验收与截图：无部署授权和真实接口环境。
- NOT_RUN — 合并、部署、生产写入：按工作流等待负责人审阅与授权。
