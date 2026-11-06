# Issue #137 buyer order pages acceptance

时间：2026-11-06 14:12 (UTC+7)

范围：买家端订单列表与订单详情页，先交付接口桩 + 模拟数据；不接真实接口、不实现下单或支付。

## 验收

- PASS — 模拟订单覆盖待付款、已付款、已提交、已接单、履约中、已发货、已签收核验、已结算，以及取消/拒绝旁路，共 10 个状态。
- PASS — 列表展示订单号、商家、商品摘要、金额与状态，点击进入详情。
- PASS — 详情展示商品行、数量、单价、总额、商家、创建日期和配送进度。
- PASS — 金额使用 e4 bigint 固定点格式化，不做浮点运算。
- PASS — 列表加载态、空态、错误态均有可见反馈与重试入口。
- PASS — 中/英/高棉三语文案接入 `packages/i18n`；Khmer 新文案保留 `TODO-KM-REVIEW` 审校标记。
- PASS — 页面返回按钮与 Telegram Back Button 复用现有路由栈。

## 本地门禁

- PASS — `pnpm typecheck`
- PASS — `pnpm lint`
- PASS — `pnpm i18n:check`（175 keys；仅 Khmer 审校提示）
- PASS — `pnpm build`
- PASS — `pnpm test`（buyer 72 tests；website 26；api-client 47；i18n 8+3；ui 16；merchant 48）
- PASS — `git diff --check`

## 未执行事项

- NOT_RUN — 真实订单 API、OpenAPI 生成客户端、协议 2008 跳转和真实状态刷新：#139 尚未合并；真实接口对接属于 #140。
- NOT_RUN — Telegram Mini App 真机/线上验收与截图：当前无部署授权和可用真实接口环境。
- NOT_RUN — 合并、部署、生产写入：按工作流等待负责人审阅与授权。
