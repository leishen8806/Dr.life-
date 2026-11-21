# V14 API 缺口盘点

> 只读盘点，2026-11-21。来源：Issue #307 的原型缺口清单、`backend/api/openapi.yaml`、各端当前 source/API adapter。本文不新增接口、不修改 OpenAPI，也不把 KESS 字段猜成临时契约。

## 判定口径

- **已有**：当前 OpenAPI 有可调用的路径，且已有端的代码已经映射或能明确复用该路径。
- **部分**：路径存在，但当前页面只读了部分字段、仅有 mock、或业务流程还没有端到端落点。
- **缺**：当前 OpenAPI 没有能表达该页面核心动作的路径。缺口只是待设计项，不代表本 PR 要实现它。
- 涉及金额、订单状态、收货地址、账户、提现、两人复核、鉴权升级或外部适配器的项目均标记为 **owner 亲审**。
- 页面名称沿用 #307 的原型缺口清单；“已有页面”只表示原型画板存在，不表示 V14 已交付。

## 现有接口基线

| 能力 | 当前 OpenAPI 路径 | 当前端使用情况 |
| --- | --- | --- |
| 公共目录 | `GET /v1/public/categories`, `/v1/public/products` | 买家首页、分类、商品列表使用；字段为 `public_id`、三语 `name`、分类、价格、图片引用、分页 `total/items`。 |
| 搜索 | `GET /v1/public/search/products`, `/v1/public/search/suggest` | 买家搜索使用；支持 `q`、分类、价格区间、排序、cursor/page size。 |
| 买家订单 | `GET /v1/buyer/orders`, `GET /v1/buyer/orders/{public_id}` | 买家订单列表/详情已映射；支付、取消另有 POST 路径，但当前 UI 仍是有限流程。金额单位为 `*_e4`，必须保持整数。**owner 亲审**。 |
| 买家支付/取消 | `POST /v1/buyer/orders/{public_id}/payments`, `/cancel` | OpenAPI 已定义；支付提供幂等键和支付状态，支付提供方/回调字段待 owner 与 KESS 规范确认。**owner 亲审，KESS 待规范**。 |
| 买家/商家通知 | 双端 `GET .../notifications`、read、read-all、settings | 买家、商家通知列表和设置已有 adapter；推送渠道与 Bot 池不是当前 OpenAPI 契约。 |
| 买家/商家身份 | `GET .../me`、sessions、devices、language、consents | 部分 adapter 已有语言/会话/consent 路径；个人资料、地址簿、关联账号不能由这些路径推导。**鉴权相关 owner 亲审**。 |
| 商家商品 | `GET/POST/PATCH /v1/merchant/products`、`publish` | 商家商品页面已接 OpenAPI；导入来源、批量任务、审核结果的异步状态仍缺少明确端点。 |
| 商家子订单 | `GET /v1/merchant/suborders`、`/{public_id}` | OpenAPI 已有商家范围隔离和脱敏收货地址；商家当前页面使用仍不完整。电话号码服务端固定返回 `****` 加末四位（短号码全遮蔽），证据见 `backend/internal/api/order_handler_test.go`。**owner 亲审**。 |
| 商家关联 | `GET /v1/merchant/me/link-candidates`、`POST /links`、`DELETE /links/{id}` | OpenAPI 已有路径；商家关联账号页面尚未交付，关联/切换的权限和并发语义需 owner 亲审。 |
| 文件 | presign、complete、download-url | 适合证照、签收照片、商品素材的上传链路；业务页面尚未把用途、状态和保留期绑定到 V14 页面。**涉及隐私/证照，owner 亲审**。 |
| 管理台认证/管理员 | admin login、refresh、step-up、password、admins CRUD/role/disable | 管理台已有真实 adapter；step-up 是敏感动作前置。**owner 亲审**。 |
| 管理台审核/订单 | admin catalog products、approve/reject、orders、order detail | 管理台已有真实 read/review adapter；商家审核列表仍映射为空，拒绝原因已有请求体。**金额/商家审核 owner 亲审**。 |
| 管理台财务 | admin financial-center、ledger accounts/postings/txns | OpenAPI 有读接口；提现出款、对账、两人复核的写动作未在当前契约中出现。**owner 亲审**。 |
| Driver | driver me/language/devices/sessions/consents/wallet transactions | 仅有基础身份/钱包契约；当前 driver 前端仍是 shell，无任务业务接口。**金额、司机权限 owner 亲审**。 |
| WMS | 当前 OpenAPI 无 `/v1/wms/*` 路径 | WMS 生产 adapter 未接真实后端，开发环境使用 mock；不能把 mock 字段当成生产接口。**全量待规范/owner 亲审**。 |

## 买家 Mini App：32 页对照

### 已有或可直接复用

| 原型页/入口 | 状态 | 可复用接口与当前字段 | 差异/风险 |
| --- | --- | --- | --- |
| 首页 | 部分 | categories + products；`items/total/page/page_size` | 原型展示推荐、Banner、运营位没有独立接口；不能把推荐当普通商品列表。 |
| 分类列表 | 已有基础 | categories、products(category) | 分类筛选字段与排序规则需确认；没有分类详情接口。 |
| 商品列表 | 已有基础 | products/search products | 列表能拿价格、三语名称、图片引用；库存、阶梯价、配送估价未在公共接口中出现。 |
| 商品详情 | 部分 | products list item，未见独立 public product detail | 详情字段、规格、库存、企业资料需要独立契约或明确复用列表字段。**owner 亲审**。 |
| 搜索 | 已有基础 | search products/suggest | 搜索历史、热词、联想分组无接口。 |
| 购物车 | 本地 | 无购物车 API | 当前 cart 为客户端状态；跨设备、登录后合并、库存校验缺接口。**金额/库存 owner 亲审**。 |
| 下单 | 部分 | `POST /v1/buyer/orders`，items + shipping_address_public_id | 配送报价、地址选择、价格快照、失败原因字段需按订单契约走；不能从 UI 猜 RouteFit 字段。**owner 亲审**。 |
| 付款 | 部分 | order payments | 支付 provider、回调、重试、结果跳转待规范；KESS 写“待规范”。**owner 亲审**。 |
| 订单列表 | 已有基础 | buyer orders cursor/page | 列表是 summary，故意没有完整 shipping address；页面不要读取缺失字段。 |
| 订单详情 | 已有基础 | buyer order detail | 有完整 buyer shipping snapshot、suborders、events、money；当前 mapper 未承载三语商家/商品展示字段。**owner 亲审**。 |
| 签收/反馈 | 部分 | order status/events 可展示状态历史；files 可上传 | 照片、验证码、签收动作和售后关联缺接口。**owner 亲审**。 |
| 找装修 / 企业 / 询价 | 原型有，接口缺 | 目前仅有公共目录/法律等通用接口 | 服务商案例、企业资料、询价提交/报价状态均缺路径。**资金/业务 owner 亲审**。 |
| 我的 | 部分 | buyer me、language、sessions/devices/consents | 个人信息、地址簿、账户余额/会员权益没有对应路径。**owner 亲审**。 |
| 钱包 / 资金计划 | 部分 | buyer wallet transactions | 钱包流水只覆盖交易查询；余额、充值、提现、资金计划没有完整契约。**金额 owner 亲审**。 |
| 通知 | 已有基础 | notifications、read/read-all、settings | Bot/推送渠道配置没有公开契约。 |
| 设置 / 帮助 / 协议 | 部分 | public legal/{kind}、buyer consents/revoke、language | 帮助内容、协议版本/强制同意 UI 需要把 legal response 与 consent state 对齐。**鉴权/合规 owner 亲审**。 |

### #307 列出的买家缺口

| 原型缺口页 | 状态 | 需要的接口/字段 | owner 亲审 |
| --- | --- | --- | --- |
| 案例列表 / 案例详情 | 缺 | 案例分页、三语标题/正文、图片、企业/项目关联、发布状态 | 是：内容与企业数据 |
| 配送追踪地图 | 缺 | 订单/子订单轨迹、司机位置、更新时间、地图 provider token/隐私策略 | 是：位置与外部适配器 |
| 售后详情 | 缺 | 售后单、原因、证据文件、状态、退款/换货动作 | 是：金额与售后 |
| AI 会员开通弹窗 | 缺 | 会员计划、价格、权益、订阅状态、支付关联 | 是：金额/KESS |
| 充值 / 提现 | 部分 | wallet 交易已有，充值意图、提现申请、复核状态、限额/风控缺 | 是：金额/两人复核 |
| 金融意向 | 缺 | 意向类型、金额区间、用途、状态、机构/隐私范围 | 是：金融合规 |
| 算报价 | 缺 | 输入材料、面积/单位、计算版本、报价项、有效期 | 是：金额与版本 |
| 关联账号（检测→关联→切换） | 缺 | candidate、link、切换后的 session/account scope | 是：鉴权/数据范围 |
| 个人信息 | 部分 | `/me` 有限；资料字段、头像、联系方式更新缺 | 是：隐私/鉴权 |
| 帮助与协议 | 部分 | legal/{kind} + consents 可复用 | 是：协议版本和三语发布 |
| 首次协议同意/拒绝 | 部分 | consent status/accept/revoke 已有 | 是：拒绝后的账号入口策略 |

## 商家 Mini App：16 页对照

| 原型页/缺口 | 状态 | 可复用/缺失接口与字段 | owner 亲审 |
| --- | --- | --- | --- |
| 商家首页/经营总览 | 部分 | me、notifications、products、suborders 可组成基础卡片 | 经营指标、金额口径需确认 |
| 商品列表/详情/新增/编辑 | 已有基础 | merchant products GET/POST/PATCH/publish；三语名称、价格、分类、图片引用 | 库存、批量导入、审核队列需补契约；金额字段需保持 `*_e4` 整数。 |
| 商品导入 | 缺 | 当前 `v7-api.ts` 只有 dev mock `source/rightsConfirmed`，没有 OpenAPI 映射 | 是：版权、异步任务 |
| 订单/子订单列表 | 部分 | merchant suborders list/detail | 当前 UI 接入不完整；需补配送、地址、事件字段展示 |
| 需求详情 | 缺 | 没有需求/询价资源路径 | 是：业务与金额 |
| 融资 | 缺 | 没有融资申请、额度、还款或状态路径 | 是：金融合规/金额 |
| AI 经营助手 | 缺 | 没有 AI 会话、工具调用、审计/额度路径 | 是：AI/KESS/数据权限 |
| 广告投放 | 缺 | 没有广告计划、预算、素材审核、投放统计路径 | 是：金额/运营 |
| 关联账号 | 部分 | link-candidates、links、delete 已有 | 切换后的数据范围与会话刷新需定稿 |
| 员工录入弹窗与三页签 | 缺 | 没有商家员工 CRUD、邀请、角色/数据范围路径 | 是：鉴权 |
| 提现表单与大额复核 | 部分 | merchant wallet transactions 只有流水查询 | 申请、限额、两人复核、出款状态缺失；金额 owner 亲审 |
| 通知与通知设置 | 已有基础 | notifications/read/read-all/settings | 事件字典、Bot 配置待规范 |
| 公司资料 / 入驻 | 部分 | `v7-api.ts` 仅 dev mock，`updateCompany/submitOnboarding` 会在生产抛 `backend_field_mapping_pending` | 是：企业资料/证照 |
| 个人资料 / 设置 | 部分 | merchant me/language/sessions/devices/consents | 可编辑字段和地址簿缺 |
| 帮助与协议/首次同意 | 部分 | legal + merchant consents | 协议版本和拒绝策略待 owner |

## Driver Mini App：原型已有、接口缺口大

当前 `apps/driver/src/main.tsx` 只渲染语言、Telegram 环境和主题 shell，没有业务 API adapter，也没有任务路由。OpenAPI 仅覆盖身份、设备、sessions、consents、language、wallet transactions、logout。

| 原型页 | 状态 | 缺失接口/字段 |
| --- | --- | --- |
| 任务地图 + 底部面板（新/已接/已完成） | 缺 | 任务列表、地图点位、状态筛选、分页、位置隐私、接单动作 |
| 任务详情 | 缺 | 子订单、取货/送达地址的最小披露、时窗、联系人脱敏、状态动作 |
| 记录签收（拍照/验证码） | 缺 | file purpose、上传完成关联、验证码核验、签收结果；**owner/KESS 亲审** |
| 我的（收入 + 提现主按钮） | 部分 | wallet transactions 仅流水，余额/提现申请/复核缺失；**金额 owner 亲审** |
| 提现页 | 缺 | 收款账户、限额、风控、复核、出款状态；**金额 owner 亲审** |
| 收入明细 | 部分 | wallet transactions 可作流水基线，但缺任务/结算关联字段 |
| 车辆与资料 | 缺 | 车辆、证照、审核状态、文件关联 |
| 弱网降级 | 缺 | 服务端没有离线同步/幂等/冲突契约；需另行设计后才可实现 |

## 物流仓储 Web（PC）

当前 `apps/wms` 是开发环境 mock 驱动的 PC 框架，已有 inventory、receiving、dispatch、inbound、interfaces 菜单入口；生产环境显示 `ProductionGate`。OpenAPI 没有 `/v1/wms/*`，因此以下均为缺口，不能把 mock 的 `Inventory`、`Receiving`、`PickTask` 字段视为生产契约。

| 原型页 | 当前实现 | 缺失接口/字段 |
| --- | --- | --- |
| 框架 + 左导航 | 有 | 登录、真实权限、数据范围、审计来源 |
| 在途进货 | mock/占位 | inbound shipment、预计到达、供应商、异常状态 |
| 入仓收货 | mock | 收货单、实测差异、阈值确认、证据文件、入库动作 |
| 库存 | mock | SKU、库位明细、可用/锁定/在途数量、盘点、红线规则 |
| 出库拣货打包 | mock/占位 | pick task、扫描、包装、幂等动作 |
| 配送派单 | mock | driver assignment、配送方式、状态回写、地图/轨迹 |
| 接口状态 | mock/占位 | integration health、延迟、失败原因、重试策略 |
| 仓库员工权限 | 缺 | 员工、角色、仓库范围、step-up/审计 |

这些接口涉及库存、配送、权限和外部适配器，需 owner 先定数据范围与事件/幂等契约；本 PR 不猜路径或字段。

## 平台后台 Web（PC）

当前 `apps/admin` 已有真实 adapter 的页面是认证、管理员、审计、商品审核、订单/订单详情；协议和 integrations 页面只有 UI/API 接口类型，生产 adapter 仍抛 `backend_field_mapping_pending`。

| 原型入口 | 当前状态 | 当前 API / 缺口 |
| --- | --- | --- |
| 框架、操作人、数据范围、敏感信息开关、分组导航 | 部分 | admin auth/admins/step-up 有基础；敏感信息开关、范围切换和全局审计缺 |
| 登录与首次改密 | 已有基础 | login/password/refresh/step-up；首次改密策略需 owner 确认 |
| 总览 | 部分 | 当前前端主要由 review/orders 组成；指标、时间窗、数据范围接口缺 |
| 用户与账户（详情抽屉） | 部分 | admins CRUD 有；买家/商家账户管理与详情缺 |
| 渠道与关联 | 缺 | 商家/买家关联、渠道归因、范围过滤缺 |
| 标准与画像 | 缺 | 标准、画像、版本和审核缺 |
| 商户 / 商品 / 分类 / 订单管理 | 部分 | catalog products、orders 有；商户与分类专属管理路径缺 |
| 财务中心 | 部分 | financial-center、ledger 读路径有；完整账务动作、对账和权限需补 |
| 资金流水 / 账户对账 | 部分 | ledger txns/accounts/postings 有 | 对账批次、差异处理、导出和审计动作缺；**金额 owner 亲审** |
| 提现出款两人复核 | 缺 | 当前 OpenAPI 没有申请、复核、出款路径；**金额/鉴权 owner 亲审** |
| 审计中心 | 已有基础 | `/v1/admin/audit` 支持 action/target/before_id；敏感字段开关与导出待明确 |
| 运营中心 / 广告管理 | 缺 | 活动、广告、预算、素材审核和统计缺；**金额 owner 亲审** |
| 协议与政策（三语发布） | UI 有，接口缺 | 当前 frontend adapter 对 list/save/publish 抛 `backend_field_mapping_pending`；**合规 owner 亲审** |
| AI 助手网关 / 审核 / 调度 | 缺 | 没有 AI gateway、审核、调度、额度、审计路径；KESS 写“待规范” |
| 消息通知 / Bot 池 | 部分 | 通知列表与设置有，Bot 池、模板、发送任务和失败重试缺 |
| 数据埋点 | 缺 | 事件字典、同意范围、采样和查询接口缺；**隐私 owner 亲审** |
| 集成管理 | UI 有，接口缺 | 当前 integrations adapter 全部 `backend_field_mapping_pending`；endpoint/环境/secret 不能自行定稿 |
| 权限与管理员 | 部分 | admins CRUD/role/disable + step-up 有；完整角色/资源/数据范围矩阵需 owner 定稿 |

## 商家电话脱敏核对（#236 前置）

结论：**服务端已脱敏，且有测试证据；商家接口不会返回完整号码。**

- `backend/internal/api/handler.go` 的商家订单映射调用 `maskPhone`，不是把 trade 层原始号码直接序列化。
- `backend/api/openapi.yaml` 的 `MerchantShippingAddressSnapshot.phone` pattern 为 `^\\*{4}[0-9]{4}$`，说明长号码只保留末四位；短于四位时全量显示为 `****`。
- `backend/internal/api/order_handler_test.go` 的 `TestMerchantSuborderMasksPhoneAndUsesMerchantScope` 断言 `****5678` 出现且原始 `+85512345678` 不出现；`TestMerchantSuborderMasksShortPhoneCompletely` 断言短号码不泄露。
- 买家端使用 `BuyerShippingAddressSnapshot`，完整号码只在 buyer-owned order response 出现；不能复用到 merchant response。

这只证明 API 序列化边界；前端日志、埋点、导出和管理台展示仍需按 #236 逐项验收。是否接受“末四位”策略由 owner 决定，本 PR 不调整策略。

## Owner 亲审 / NOT_RUN 清单

1. KESS 支付、AI、地图和外部 catalog/RouteFit 接口：当前只记录“待规范”，需要 owner 提供正式 schema、鉴权方式、错误/重试/幂等语义。
2. 所有金额动作：充值、提现、融资、广告预算、两人复核、支付回调，需要金额单位、限额、审计和回滚规则。
3. 身份与数据范围：关联账号切换、driver/WMS/admin 权限矩阵、敏感字段开关，需要 owner 定稿。
4. 三语内容：协议、帮助、案例、商品/企业字段需要发布版本和缺翻译行为；不得以机器翻译作为验收通过。
5. WMS/driver 真实接口：mock 仅用于开发，不代表后端已承诺的字段。

## 结论

当前 V14 的已知可复用基础主要是公共目录、买家订单/支付骨架、通知、身份/consent、商家商品/子订单、管理台认证/审核/订单/账务读模型。买家缺口、商家金融/员工/导入、完整 driver、WMS 和后台运营/协议/集成仍需要设计定稿或新增 API 契约。后续实现 PR 应先以本清单和 owner 亲审结果为准；本盘点不将任何缺口伪装成 PASS，也不修改 `backend/api/openapi.yaml`。

Refs #307
Refs #236
