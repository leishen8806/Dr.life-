# P0-8 数字商品支付规则核对（Telegram / AI 会员）

> 调研日期：2026-10-16。范围仅为 Telegram Mini App 内 AI 会员（数字服务）的收款规则与对既有需求、账务设计的影响；不是支付、账务或技术实现方案。

## 一页结论

AI 会员在 Telegram Mini App 内出售时，必须使用 Telegram Stars（`XTR`）；不能以银行专户、平台钱包余额或第三方支付商替代。Telegram 明确规定：数字商品/服务交易“must be executed exclusively through the exchange of Telegram Stars”，并称使用替代支付系统可能导致 TPA 在商店版本 Telegram 中不可访问或被终止。[Telegram Bot Platform Developer Terms §6.2](https://telegram.org/tos/bot-developers#6-payments)

因此，现有“从用户/商户可用资金扣 AI 会员费”的 PRD 表述和 6.15 的 `WALLET_AVAIL → PLATFORM_HELD`，不能作为 Telegram 内 AI 会员购买的上线验收口径。推荐董事会冻结为：**Telegram 内 AI 会员仅可由 Stars 成功支付回调开通；不把平台银行钱包扣款当作该购买的替代路径。** Stars 收益与银行专户/总账如何确认、何时确认，需架构师和账务审计员根据下述官方限制另行定案；本任务不改分录或实现。

## 依据（官方来源与关键原文）

| 结论 | 官方关键原文 | 来源 |
|---|---|---|
| Telegram 内数字服务只可用 Stars | “all transactions pertaining to digital goods and services must be executed exclusively through the exchange of Telegram Stars” | [Bot Developer Terms §6.2](https://telegram.org/tos/bot-developers#6-payments) |
| 不能用第三方/其他币种规避 | “must exclusively use Telegram Stars … regardless of any other web portals, apps, services or payment providers” | [Telegram Stars Payments FAQ](https://core.telegram.org/bots/payments-stars) |
| 成功付款后才能交付 | “always check that you received a `successful_payment` update before delivering” | [Telegram Stars Payments：Checkout](https://core.telegram.org/bots/payments-stars) |
| 退款与支持责任在商家 | “all TPA must be able to respond to the command `/paysupport`”；Bot API 可 `refundStarPayment` | [Bot Developer Terms §6.2.1](https://telegram.org/tos/bot-developers#6-payments)；[Bot API：refundStarPayment](https://core.telegram.org/bots/api#refundstarpayment) |
| Stars 不等同平台持有的银行资金 | “Stars displayed in your balance do not constitute your property”；奖励可在收到后最多 21 天不可用 | [Bot Developer Terms §6.2.2–6.2.4](https://telegram.org/tos/bot-developers#6-payments) |
| 奖励与地区可用性有限制 | 每 Star 的奖励等值为 0.013 USD；Fragment “may be unable to issue rewards … in certain countries” | [Bot Developer Terms §6.2.4](https://telegram.org/tos/bot-developers#6-payments) |

## 可验收需求

### P0-8-1：Telegram 内会员购买（P0，必须）

**用户故事：** 作为在 Telegram Mini App 中购买 AI 会员的用户，我只能看到 Stars 价格和 Telegram 支付入口；只有 Telegram 确认支付成功后才获得会员权益。

**验收标准：**

- 数字服务订单使用 `XTR`；不展示或接入平台钱包、银行专户、银行卡或第三方支付作为该订单的支付方式。
- 仅在收到 Telegram `successful_payment` 后开通会员；仅预检通过不能开通。
- 系统保存 `telegram_payment_charge_id`，以支持官方 Stars 退款接口；退款/争议入口能指向 `/paysupport` 支持渠道。
- 会员说明、价格、退款和支持文案提供中文、English、ខ្មែរ三语；条款在付款前可访问。

**不做什么：** 不承诺 Stars 与美元固定兑换比；不把存入平台钱包或银行专户的金额折抵为 Telegram 内会员费；不以外部网页、深链或第三方支付作为规避 Stars 的购买通道。

### P0-8-2：权益与真实付款状态（P0，必须）

**用户故事：** 作为购买者，我看到的会员状态只反映已收到的 Stars 成功付款或有效退款结果，不会因预检、创建订单或装饰性活动而显示已开通。

**验收标准：**

- 未收到成功付款回调时，会员保持未开通；不能因预检通过、生成发票或手工前端状态而开通。
- 退款后权益处理遵循董事会批准的退款规则；若规则尚未批准，不能把退款后状态标为“已完成”。
- 付款状态、权益状态与账务/审计记录均由实际 Telegram 付款或退款结果产生。

**不做什么：** 不增设角色、模型、状态或模拟付款活动；官方既定角色与状态保持不变（Role ≠ Model）。

### P0-8-3：Stars 对账与会计边界（P0，阻塞 N-06 / L-03）

**用户故事：** 作为财务和审计人员，我能区分 Telegram Stars 付款记录、可用 Stars、退款和最终奖励，避免把尚不可用或不可兑现的 Stars 当作银行专户余额。

**验收标准：**

- 架构师与账务审计员书面批准 Stars 交易标识、退款关联、权益开通时点以及银行专户/总账确认时点后，才更新 L-03 的 6.15 与实现 N-06。
- 对账资料能逐笔关联 Telegram 支付标识；退款能关联原付款标识。
- 任何平台手续费/兑换差额科目均须以 Telegram、Fragment 与合作银行的实际结算单据为依据；在费率、结算账户和柬埔寨可用性证实前，不创建或计量该科目。

**不做什么：** 不把 Stars 余额直接等同 `BANK_DEDICATED`、`WALLET_AVAIL` 或已实现平台现金收入；不在本任务臆定会计分录、费率或结算周期。

## 三种收款方案对比

| 方案 | Telegram 内 AI 会员的合规性 | 费率/结算周期/退款 | 柬埔寨可用性 | 对 6.15 / N-06 的影响 |
|---|---|---|---|---|
| 纯 Telegram Stars | **符合官方规则**：数字服务在 Telegram 内须 `XTR`。 | Telegram 条款称每 Star 奖励等值 0.013 USD，但价格和价值可变；可用性最多延后 21 天；退款用 `refundStarPayment`。不能把它断言为固定“手续费率”。 | **未证实**：官方仅说明 Fragment 在部分国家/用户可能无法发放奖励；需以实际公司主体、收款人与 Fragment 流程验证。 | 原 6.15 不能继续作为“钱包扣款购买”。需由架构/账务审计定义 Stars 付款记录、退款与奖励/银行确认之间的分录。N-06 以成功 Stars 付款开通。 |
| 外部支付（银行专户） | **不符合**作为 Telegram 内 AI 会员支付方式；官方明确禁止用第三方支付替代 Stars。 | 银行费率、清算周期、退款规则取决于合作银行，当前**未证实**。 | 合作银行接口与产品能力尚未确认，**未证实**。 | 不能用于 Telegram 内购买。若董事会以后销售非 Telegram 渠道的独立服务，其会计和合规范围另立需求，不能沿用本任务结论。 |
| 混合 | 若“混合”指 Telegram 内用 Stars、银行只处理与会员无关的钱包/实物业务，则会员部分合规；若指让银行钱包扣款或外部支付替代 Stars，**不符合**。 | Stars 一侧同上；银行一侧**未证实**。两边不可混为同一笔会员购买。 | Stars 奖励、银行能力均各自待证实。 | 仅可把 Stars 付款作为会员开通依据；不得用银行钱包补写或抵销会员购买分录。 |

## 推荐、范围与待董事会确认

**推荐：** 采用“Telegram Mini App 内 AI 会员 = 纯 Stars”的产品规则，先完成官方 Stars 流程与真实付款/退款审计链；把银行专户收款从本功能移除。理由是这是唯一有明确 Telegram 官方依据的方案，且不会把不可用 Stars、平台钱包和银行资金混为一类。

**需董事会/架构师确认（未确认前不进入实现）：**

1. 公司主体和实际收款人是否能通过 Fragment 接收奖励（含柬埔寨可用性、KYC、税务与最终收款路径）。官方只说明“may be unable”，没有柬埔寨肯定名单。
2. Stars 价格（XTR 数量）、会员周期、退款后权益处置和三语条款。
3. Stars 奖励到银行专户前后的会计确认和对账方式；这是 L-03 6.15 的变更前置，须由架构师和账务审计员决定。
4. 现有 PRD 第 6 节“会员费从可用资金扣除”的文字应替换为本结论；本任务卡仅授权新增本调研文档，未改 PRD/账务文档。

## 维护范围（防止范围蔓延）

- 本结论只覆盖 Telegram Mini App 内的 AI 会员，不改变仓储、配送、实物商品、银行专户或 0 佣金等既有业务承诺。
- 不决定具体 Bot/API、数据表、分录、退款天数、税务、Fragment 或银行开户/接入方案；这些需架构、账务审计和法务/合作方确认。
- 不改变官方既定的 8 角色与 8 状态；不把角色当作模型，不创造装饰性付款或会员活动；经典主题不引入 3D。
