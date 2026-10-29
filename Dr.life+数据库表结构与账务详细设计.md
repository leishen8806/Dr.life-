# Dr.life+ 数据库表结构与账务详细设计

**版本：** 2026-10-09 草案　**性质：** 开工前最后一份设计文档　**数据库：** PostgreSQL（托管机柜自建，主 + 从）　**后端：** Go（pgx + sqlc，迁移用 goose）
**配套：** 《技术方案与系统边界》（边界、状态机、账务原则）· 《第一版交付形态与技术架构》（选型、部署）· 《平台后台权限与审计规范》· 可点击原型（账务与审计的行为参照）
**读者：** 后端开发（含 Codex 等开发助手）、DBA、测试
**说明：** 本文给出**可直接落地的表结构与约束**。字段名为建议，开发时可微调，但**第二章（约定）和第四～六章（账务）的规则不可改动**；未列出的辅助字段（如备注）按需补。

---

## 一、总体设计

### 1. 模块与 schema（从数据库层强制模块边界）
| schema | 模块 | 主要内容 |
|---|---|---|
| `iam` | 账号与权限 | 人、账号、Bot 池、登录、会话、商户员工、平台管理员与角色、账号关联 |
| `catalog` | 商品与目录 | 分类、商品、阶梯价、导入、标准分、搜索词 |
| `trade` | 订单与项目 | 订单、询价与报价、装修项目与分批计划、付款凭证、售后 |
| `wms` | 仓储 | 仓库分区、库位、库存、入仓、库存流水、仓储费计提与账单 |
| `logistics` | 物流 | 配送单、节点、司机、任务、配送费规则、签收凭证 |
| `ledger` | **账务（核心）** | 账户、流水、分录、余额、冻结、提现、对账 |
| `comms` | 通知 | 消息、发送记录、模板、Bot 发送记录 |
| `ai` | AI | 会话、消息、额度、会员、接口配置、调用日志、基础信息与标签 |
| `ops` | 运营 | 广告、公告、渠道与关系、协议与同意、埋点、集成配置、系统配置 |
| `audit` | 审计 | 审计日志（哈希链）、审计检查结果 |

**规则：**
1. **每个模块只允许读写自己 schema 里的表。** 跨模块只能调用对方的 Go 接口（或事件），不允许直接 `JOIN` 别人的表写入。只读报表（财务中心、总览）通过**只读视图**或从库实现，视图由数据所属模块提供。
2. 应用使用**分角色的数据库账号**：每个模块一个读写角色；`ledger` 的写角色只给账务模块；`audit` 的角色**只有 INSERT 和 SELECT**；报表使用只读角色连从库。

### 2. 通用约定
| 项 | 约定 |
|---|---|
| 主键 | `id bigint generated always as identity`（内部）；对外暴露用 `public_id`（ULID 文本，唯一），**接口里不出现自增 id** |
| 业务单号 | 订单 `ORD-yyyymmdd-xxxxxx`、流水 `L` + 12 位序号等，有唯一约束，便于人读 |
| 时间 | 一律 `timestamptz`（UTC 存储），界面按柬埔寨时区（UTC+7）显示；每张业务表有 `created_at`、`updated_at` |
| 金额 | **`bigint`，单位 = 0.0001 美元**（万分之一美元，即 `amount_e4`）。例：$14.00 存 `140000`。禁止 `float/double`。显示时四舍五入到分；货币字段单独存（第一版账务只有 `USD`） |
| 瑞尔 | 第一版**只做显示换算和账单双币显示**，不记账；汇率表 `ops.fx_rate` 记录每个生效汇率，账单/发票保存当时使用的汇率快照 |
| 数量与体积 | 数量 `numeric(18,3)`；体积（m³）`numeric(12,4)`；价格阶梯单价 `bigint`（e4） |
| 多语言文本 | 商品名、分类名、公告等用 `jsonb`：`{"zh":"…","en":"…","km":"…"}`，**三语齐全**由应用层校验；协议正文三语分列 |
| 枚举 | 用 `text` + `CHECK (col IN (...))`，不用 PostgreSQL 枚举类型（便于迁移）；取值在代码里有常量 |
| 软删除 | 业务数据一般不物理删除，用 `status`/`deleted_at`；**账务和审计表禁止删除** |
| 审计字段 | 重要表带 `created_by`、`updated_by`（指向 `iam.actor`，可以是人、系统任务或外部回调） |
| 并发 | 需要乐观锁的表带 `version int`；账务、库存使用行级锁（见第五章） |
| 迁移 | goose 迁移文件按序号提交，**只向前兼容**（先加列再改代码再删列）；生产迁移可回滚；账务表结构变更需双人评审 |
| 命名 | 表、列 `snake_case`；表名单数；外键 `xxx_id`；索引 `ix_表_列`；唯一约束 `ux_表_列` |

---

## 二、账号与权限（`iam`）

| 表 | 关键字段 | 约束与说明 |
|---|---|---|
| `person` | id, public_id, tg_user_id **bigint unique**, tg_first_name, tg_username, tg_lang_code, phone_enc, phone_verified_at, status | **一个 Telegram 号一个人**；`phone_enc bytea` 由应用层加密后存放 |
| `account` | id, public_id, person_id→person, role (`buyer/merchant_owner/merchant_staff/driver`), merchant_id→merchant(可空), status (`active/frozen/disabled`), status_reason, lang, lang_registered_at, kyc_status | `UNIQUE NULLS NOT DISTINCT(person_id, role, merchant_id)`，确保 `merchant_id` 为空的账号也不能重复；冻结/停用必须填 `status_reason`（CHECK）；`lang` 为 `zh/en/km` |
| `account_device` | account_id, device_id, first_seen_at, last_seen_at | 登录与刷新时更新的设备记录 |
| `bot` | id, public_id, end (`buyer/merchant/driver`), username unique, role_in_pool (`primary/standby`), status (`active/standby/limited/banned`), token_enc (bytea), token_last4, webhook_secret_enc, priority | **令牌信封加密**，AAD 为 `bot_token:<id>`；`token_last4` 仅用于显示；同一端 `priority` 唯一且最多一个 primary |
| `account_bot` | account_id, bot_id, bound_at, write_access boolean, unreachable_at | 用户在哪些 Bot 点过开始/允许私信；发送时据此选 Bot（第二章） |
| `login_event` | id, person_id, bot_id, valid_by_bot_id, tried_count, auth_date, ip, device_id, result, created_at | 登录审计；`result` 含 `ambiguous_account`；`iam_rw` 仅可 SELECT/INSERT，禁止 UPDATE/DELETE/TRUNCATE/TRIGGER/REFERENCES，校验命中的 Bot、尝试次数 |
| `account_status_log` | id, public_id, account_id, from_status, to_status, reason, actor_type, actor_id, created_at | 账号状态变更只追加日志；冻结、停用及恢复均须填写原因；`iam_rw` 仅可 SELECT/INSERT，拒绝修改、删除和截断；`report_ro` 不可读取 `reason` |
| `session` | id, account_id, family_id, refresh_token_hash, previous_hash, rotated_at, rotation, refresh_kid, device_id, ip, expires_at, last_used_at, revoked_at, revoked_reason | 刷新令牌只存哈希；旋转使用前一哈希和时间实现宽限期重放，冻结/停用时按 family 批量作废 |
| `security_event` | account_id, session_family_id, event, reason, created_at | 会话安全事件；不含令牌或哈希 |
| `merchant` | id, public_id, name jsonb, type, area, svc jsonb, description jsonb, phone_enc, address_enc, contact_enc, kind (`self/verified/pending`), status, owner_account_id | 商户资料；电话、地址、联系人由应用层加密后存放；`kind=verified` 须核验 |
| `merchant_staff` | id, public_id, merchant_id, account_id(可空,未接受前), name, phone_enc, phone_hash, tg_username, note, role (`manager/sales/warehouse/finance`), status (`invited/active/off`), invite_code unique, invited_by, last_login_at | 员工上限 10（应用层校验）；`phone_enc` 存密文，`phone_hash` 存规范化手机号的不可逆摘要并用于同一商户内唯一性 |
| `merchant_role_perm` | merchant_id, role, module (`dash/prod/orders/store/fin/ads/prof/staff/ai`), level (`none/view/edit`) | `PRIMARY KEY(merchant_id,role,module)`；`staff` 模块 level 只允许 `none/view` |
| `merchant_role_perm_template` | role, module, level | 四类商户角色的默认权限模板；创建商户时由应用复制到 `merchant_role_perm` |
| `merchant_op_log` | id, merchant_id, actor_account_id, action, detail, created_at | 员工操作记录（只追加） |
| `account_link` | id, buyer_account_id, merchant_account_id, linked_at, unlinked_at | 一键关联；`UNIQUE(buyer,merchant)` 且仅一条未解除；**只保存关联，不改变任何资金/订单归属** |
| `admin_user` | id, public_id, name, email, password_hash(argon2), totp_secret_enc, role, data_scope text[], status, last_login_at | 密码只存 Argon2 哈希，TOTP 密钥由应用层信封加密后存放（AAD=`admin_totp:<public_id>`）；平台与物流仓储 Web 账号；超级管理员至少 1 位启用（应用层 + 数据库触发器 + 定期审计） |
| `admin_role_perm` | role (`super/ops/finance/logi/cs/auditor`), module(17 个), level | 对应《权限与审计规范》矩阵；`super` 固定全 `edit` |
| `actor` | id, kind (`account/admin/system/external`), ref_id | 统一"操作人"，供 `created_by`、审计、流水引用 |

---

## 三、商品、订单、仓储、物流（概要）

### `catalog`
| 表 | 关键字段与约束 |
|---|---|
| `category` | id, code, name jsonb, enabled, default_zone, sort；自定义品类 `is_custom` |
| `product` | id, public_id, merchant_id, category_id, name jsonb, spec jsonb, unit, origin (`cn/lc`), lead_days, vol_m3, status (`draft/review/on/off/rejected`), source (`manual/amazon/shopee/a1688/csv`), source_ref, std_score smallint (0–100，由任务重算), search_text (tsvector) + `pg_trgm` 索引 |
| `product_tier` | product_id, min_qty, unit_price_e4；`UNIQUE(product_id,min_qty)`；至少 2 档由应用层要求 |
| `product_import` | id, merchant_id, source, status, items jsonb, rights_ack boolean, created_at；导入后商品 `status=review` |
| `search_synonym` | id, terms text[]（三语同义词组） |
| `search_log` | id, account_id, q, result_n, created_at（热词、联想来源，按天分区） |

### `trade`
| 表 | 关键字段与约束 |
|---|---|
| `order` | id, order_no unique, buyer_account_id, merchant_id, status (`unpaid/paid_proof/shipped/arrived/closed/problem/cancelled`), total_e4, delivery_mode (`zto/self`), delivery_fee_hold_txn_id, delivery_fee_release_txn_id, problem_note, closed_at |
| `order_item` | order_id, product_id, qty, unit_price_e4（下单时快照，含阶梯匹配结果）, vol_m3 |
| `payment_proof` | id, order_id / plan_batch_id, amount_e4, ref, proof_file_id, recorded_by, created_at | **货款凭证，不进账务分录**（线下付款，只留痕）；流水页通过视图并列显示 |
| `rfq` | id, buyer_account_id, name, area/loc, category_id, qty_text, budget_e4, due_date, target_merchant_id, status |
| `quote` | id, rfq_id, merchant_id, price_e4, lead_days, valid_until, note, selected boolean；`UNIQUE(rfq_id,merchant_id)` |
| `project` | id, buyer_account_id, company_merchant_id, name, area, budget_e4, status |
| `pay_plan` / `pay_plan_batch` | plan: id, project_id, total_e4, status；batch: id, plan_id, seq, payee_merchant_id, amount_e4, condition, status (`planned/frozen/released/disputed/refunded`), freeze_txn_id, release_txn_id；**`SUM(batch.amount)=plan.total`（应用层 + 触发器）** |
| `after_sale` | id, order_id, kind (`refund/return/damage`), status, initiator, decided_by, amount_e4, ledger_txn_id |

### `wms`
| 表 | 关键字段与约束 |
|---|---|
| `zone` / `bin` | zone: code, kind (`shuttle/pallet/bulky/overflow`), capacity_m3；bin: zone_id, code, capacity_m3；`UNIQUE(code)` |
| `stock` | product_id **PK**, qty numeric, `CHECK (qty >= 0)` |
| `stock_bin` | product_id, bin_id, qty；`CHECK(qty>=0)`；**`SUM(stock_bin.qty)=stock.qty`**（触发器 + 审计检查 #14）；库位容量不得超 |
| `stock_move` | id, product_id, bin_id, kind (`in/out/adjust/transfer`), qty_delta, ref_type, ref_id, actor_id, created_at | **只追加**；库存数量可由流水重算 |
| `inbound` | id, merchant_id, product_id, declared_qty, declared_vol, actual_qty, actual_vol, method, status (`booked/received/cancelled`), diff_confirmed_at | **按实测体积计费**；差异超阈值需商户确认 |
| `storage_fee_daily` | merchant_id, product_id, day, qty, vol_m3, rate_e4, amount_e4；`UNIQUE(merchant_id,product_id,day)` | 每日计提（子账，不进总账），`amount = qty × vol × rate ÷ 30` 保留 e4 精度 |
| `storage_statement` | id, merchant_id, period (yyyy-mm), accrued_e4, collected_e4, status (`open/collected/arrears`), ledger_txn_id, fx_rate_id, usd_total, khr_total | 月度对账单；出账时才记总账（见第六章 6.8） |

### `logistics`
| 表 | 关键字段与约束 |
|---|---|
| `driver` | id, account_id, plate, status, online |
| `shipment` | id, order_id unique, mode (`zto/self`), waybill_dom, waybill_xb, waybill_kh, driver_id, status, fee_total_e4, fee_driver_e4, fee_platform_e4, fee_carrier_e4, dist_km |
| `shipment_node` | shipment_id, seq, label, location, at, source (`zto/driver/manual`) | 节点只追加 |
| `delivery_proof` | shipment_id, kind (`photo/signature/code`), file_id, code_hash, at, actor_id | **有凭证才允许触发分账** |
| `delivery_rule` | id, base_e4, per_km_e4, margin_e4, zto_fee_e4, effective_from, created_by | **规则按生效时间版本化**，调整不影响已托管订单 |

---

## 四、账务详细设计（`ledger`）— 总则

### 1. 为什么要这样设计
- 钱真正在**合作银行专用账户**里；平台账务是对它的**逐笔解释**，所以采用**复式记账**：每笔资金动作 = 一笔交易（`txn`）+ 至少两条分录（`posting`），**借方合计 = 贷方合计**。
- 任何余额都**可以由分录重算**；系统里存的余额只是加速查询的缓存，必须与重算值一致，不一致就报警。
- 流水**只增不改**；错了用**冲销交易**更正。

### 2. 账户类型与科目表（chart of accounts）
符号约定：**资产账户借方为正**；**负债/权益账户贷方为正**。`ledger_balance.balance` 统一存**"正常方向"余额**（资产=借-贷；负债/权益=贷-借）。

| 科目（`kind`） | 类型 | 归属 | 对应原型的账户 | 说明 |
|---|---|---|---|---|
| `BANK_DEDICATED` | 资产 | 平台（单例） | EXT | 合作银行专用账户在平台账里的镜像；与银行对账文件逐笔对 |
| `WALLET_AVAIL` | 负债 | 每个账号一个 | o / m{id} / d{id} | 用户**可用资金**（可提现） |
| `WALLET_FROZEN` | 负债 | 每个账号一个（买家） | FRZ | 装修分批计划已冻结资金；明细由 `ledger_hold` 记录 |
| `PENDING_PAYOUT` | 负债 | 平台（单例），由 `ledger.withdrawal` 按申请人分明细 | WD | 已申请提现、待银行出款 |
| `DELIVERY_ESCROW` | 负债 | 平台（单例）按订单分明细 | DH | 配送费托管 |
| `AD_BUDGET` | 负债 | 每个商户一个 | ADB | 广告预算（已划扣未消耗） |
| `PLATFORM_HELD` | 权益 | 平台（单例） | P | **平台自有收入**（仍在专用账户里，待划出）；每条分录带 `category` 区分来源 |
| `PAYABLE_CARRIER` | 负债 | 平台（单例） | ZTO | ~~应付中通~~ **2026-10-29 起为“应付 Dr.RouteFit”**（仓配整体交给 RouteFit，中通由 RouteFit 对接），见第六章 6-B |
| `CHANNEL_KESS` | 资产 | 平台（单例） | — | **2026-10-29 新增**：已经 KESS 确认收款、尚未结算到银行专用账户的渠道款，见 6-B |
| `SUSPENSE` | 过渡 | 平台 | — | 对账差异、未识别入金暂存，**必须人工清零** |

> **平台收入的 `category`：** `storage`（仓储费）、`delivery_margin`（配送留存）、`ads`（广告）、`settle_fee`（结算服务费，第一版为 0）、`member`（AI 会员费）——与原型财务中心的 5 类收入一致，**财务中心、审计 #13 都按它汇总**。

### 3. 对账恒等式（最重要的不变量）
```
Σ WALLET_AVAIL + Σ WALLET_FROZEN + Σ PENDING_PAYOUT + Σ DELIVERY_ESCROW
  + Σ AD_BUDGET + PAYABLE_CARRIER + PLATFORM_HELD + SUSPENSE
  =  BANK_DEDICATED（银行专用账户余额）
```
**2026-10-29 修订（6-B）：** 右边改为 `BANK_DEDICATED + CHANNEL_KESS`。KESS 已确认、尚未结算到银行的渠道款也是客户资金，但不在银行余额里。`CHANNEL_KESS` 另与 KESS 结算报表逐笔核对。
这等价于原型财务中心的"内部账户合计 = 银行外部净流入"检查。**每天、每次对账文件导入后都要验证；不成立就是"异常"。**

### 4. 币种与精度
- 第一版账务**只有 `USD`**；金额 `bigint`，单位 0.0001 美元。
- 所有"分摊/按比例"计算统一用**整数运算 + 最后一项倒挤**，保证分账之和恒等于总额（例：配送费分三方，司机、平台、中通各自计算，最后一方 = 总额 − 其余）。
- 舍入规则：**四舍五入到 e4**（四位小数）；对用户显示时再按分（e2）四舍五入；账单合计以已入账的 e4 总和为准，**不再二次舍入**。

---

## 五、账务表结构与关键约束（可直接落地的 SQL）

```sql
-- 账户
create table ledger.account (
  id            bigint generated always as identity primary key,
  kind          text not null check (kind in ('BANK_DEDICATED','WALLET_AVAIL','WALLET_FROZEN','PENDING_PAYOUT',
                  'DELIVERY_ESCROW','AD_BUDGET','PLATFORM_HELD','PAYABLE_CARRIER','SUSPENSE')),
  normal_side   text not null check (normal_side in ('DEBIT','CREDIT')),     -- 资产=DEBIT；负债/权益=CREDIT
  owner_account bigint,        -- iam.account.id（钱包类）；merchant 的广告预算用商户所有者账号
  merchant_id   bigint,
  currency      text not null default 'USD' check (currency = 'USD'),
  allow_negative boolean not null default false,
  status        text not null default 'open' check (status in ('open','closed')),
  created_at    timestamptz not null default now(),
  check ((kind = 'BANK_DEDICATED' and normal_side = 'DEBIT') or
         (kind <> 'BANK_DEDICATED' and normal_side = 'CREDIT')),
  check ((kind in ('BANK_DEDICATED','PENDING_PAYOUT','DELIVERY_ESCROW','PLATFORM_HELD','PAYABLE_CARRIER','SUSPENSE') and owner_account is null) or
         (kind in ('WALLET_AVAIL','WALLET_FROZEN','AD_BUDGET') and owner_account is not null)),
  check (not allow_negative or kind in ('PAYABLE_CARRIER','SUSPENSE')),
  unique nulls not distinct (kind, owner_account)
);

-- 交易（一次业务动作）
create table ledger.txn (
  id            bigint generated always as identity primary key,
  txn_no        text   not null unique,            -- 'L000001' 规则：序列号补零
  type          text   not null,                   -- topup/freeze/settle/refund/withdraw_req/payout/payout_fail/storage_fee/
                                                   -- delivery_hold/delivery_release/ad_charge/ad_spend/ad_refund/member_fee/
                                                   -- sweep/carrier_pay/reversal/adjust/bank_diff
  amount        bigint not null check (amount > 0),-- 交易金额（借方合计）
  idem_key      text   not null unique,            -- 幂等键：同一业务动作只能入账一次
  ref_type      text,  ref_id bigint,              -- 关联单据（order/plan_batch/withdrawal/ad_campaign/statement …）
  note          text,
  reverses_txn_id bigint references ledger.txn(id),-- 冲销交易指向被冲销的原交易
  actor_id      bigint not null,                   -- iam.actor.id
  occurred_at   timestamptz not null default now(),
  created_at    timestamptz not null default now()
);

-- 分录（复式）
create table ledger.posting (
  id            bigint generated always as identity primary key,
  txn_id        bigint not null references ledger.txn(id),
  account_id    bigint not null references ledger.account(id),
  side          text   not null check (side in ('DEBIT','CREDIT')),
  amount        bigint not null check (amount > 0),
  role          text,              -- payee/driver/platform/carrier/fee_held … （分账明细的角色，对应原型的 split.role）
  category      text check (category in ('storage','delivery_margin','ads','settle_fee','member')),  -- 仅平台收入分录使用
  created_at    timestamptz not null default now()
);
create index ix_posting_account on ledger.posting(account_id, id);
create index ix_posting_txn on ledger.posting(txn_id);

-- 余额缓存（与分录在同一事务里更新）
create table ledger.balance (
  account_id    bigint primary key references ledger.account(id),
  balance       bigint not null default 0,         -- 正常方向余额
  allow_negative boolean not null,                 -- 从 account 冗余，数据库 CHECK 可直接约束余额
  version       bigint not null default 0,
  updated_at    timestamptz not null default now(),
  check (allow_negative or balance >= 0)
);
```

**数据库层的硬约束（不依赖应用代码）：**
```sql
-- (1) 每笔交易借贷平衡：延迟约束触发器，在事务提交时检查
create function ledger.check_txn_balanced() returns trigger as $$
declare d bigint; c bigint; a bigint;
begin
  select coalesce(sum(amount) filter (where side='DEBIT'),0),
         coalesce(sum(amount) filter (where side='CREDIT'),0) into d,c
    from ledger.posting where txn_id = new.txn_id;
  select amount into a from ledger.txn where id = new.txn_id;
  if d <> c or d <> a then raise exception 'txn % not balanced (debit %, credit %, amount %)', new.txn_id, d, c, a; end if;
  return null;
end $$ language plpgsql;
create constraint trigger trg_txn_balanced after insert on ledger.posting
  deferrable initially deferred for each row execute function ledger.check_txn_balanced();

-- (2) 流水只增不改不删
create function ledger.deny_mutation() returns trigger as $$
begin raise exception 'ledger rows are append-only'; end $$ language plpgsql;
create trigger trg_txn_ro     before update or delete on ledger.txn     for each row execute function ledger.deny_mutation();
create trigger trg_posting_ro before update or delete on ledger.posting for each row execute function ledger.deny_mutation();
create trigger trg_txn_no_truncate before truncate on ledger.txn for each statement execute function ledger.deny_mutation();
create trigger trg_posting_no_truncate before truncate on ledger.posting for each statement execute function ledger.deny_mutation();
-- 同时：REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON ledger.txn, ledger.posting FROM ledger_rw；

-- (3) 非负余额：由 balance.allow_negative + CHECK 兜底；客户资金账户永远是 false
```
> `SUSPENSE` 使用 CREDIT 正常方向并允许暂时为负，供对账差异双向暂存；`PAYABLE_CARRIER` 第一版保持不允许负数。`allow_negative` 在创建账户时由触发器复制到 `balance`，且余额写入触发器要求两处策略一致；**客户钱包类账户永远不允许为负。**

### 入账流程（对外仅限强类型业务方法，通用 `post` 为包内私有实现）
其他模块只能调用账务模块经过评审的强类型业务方法。通用入账核心不导出，不能用任意交易类型、账户和金额绕过业务校验、状态机或职责分离；测试通过仅在测试构建中存在的包装验证底层不变量。

1. 校验请求：金额 > 0；分录借贷平衡；账户存在且 `open`；金额与业务单据一致（例如分账之和 = 托管金额）。
2. **开启事务**（`READ COMMITTED` + 显式行锁）。
3. **按 `account_id` 升序**对涉及的 `ledger.balance` 行 `SELECT … FOR UPDATE`（**固定顺序加锁，避免死锁**）。
4. 检查余额：借记负债账户后不得小于 0（客户资金）；不足返回 `ERR_INSUFFICIENT_FUNDS`，**不记账**。
5. `INSERT ledger.txn`（带 `idem_key`）。**若唯一冲突，比较类型、金额、关联信息、发起人和规范化后的分录；内容相同才读取 outbox 中保存的原结果并返回，内容不同返回幂等冲突，不再入账。**
6. `INSERT ledger.posting`（批量）；更新各 `balance`（`balance = balance ± amount`，`version+1`）。
7. 如业务动作注册了事务后置钩子，在分录与余额写入后、提交前执行；冻结业务用它写入或关闭 `ledger.hold`，因此记账与状态变化只能一起成功或一起回滚。幂等重放不重复执行后置钩子，并在原事务中校验业务状态与原交易一致。
8. 同事务写入 outbox，再提交事务；提交时延迟触发器验证借贷平衡，任一步失败均整体回滚。
9. outbox 工作者用 `FOR UPDATE SKIP LOCKED` 并发领取，发送成功后标记 `sent_at`；发送失败释放事务锁以便重试，**不回滚已经提交的账务**。
10. `40001`（序列化失败）和 `40P01`（死锁）有限次数退避重试；其他数据库错误和业务错误不自动重试，上下文取消立即停止。

**幂等键规则（`idem_key`）：** `业务类型:业务单据:动作[:序号]`，例如 `delivery_hold:order:12345`、`payout:withdrawal:777`、`settle:batch:99`；重试同一动作必须用同一个键。

---

## 六、业务动作与分录（逐类写清）

> 记号：`DR` 借，`CR` 贷。金额均为 e4；`A` = 用户账号，`M` = 商户，`D` = 司机。

| # | 业务 | txn.type | 分录 | 说明与约束 |
|---|---|---|---|---|
| 6.1 | **充值入金**（银行确认到账） | `topup` | DR `BANK_DEDICATED` / CR `WALLET_AVAIL(A)` | 银行入账文件（或人工确认）后才入账；不确定的入金先入 `SUSPENSE` |
| 6.2 | **装修资金冻结**（计划某批） | `freeze` | DR `WALLET_AVAIL(buyer)` / CR `WALLET_FROZEN(buyer)` + 写 `ledger_hold(plan_batch)` | 余额不足不能冻结；`hold` 记录批次与金额 |
| 6.3 | **节点确认后结算给收款方** | `settle` | DR `WALLET_FROZEN(buyer)` / CR `WALLET_AVAIL(payee)`；若结算服务费 > 0：再 CR `PLATFORM_HELD` (category=`settle_fee`)，收款方少收相应金额 | 第一版 fee=0；`hold` 置为已释放 |
| 6.4 | **争议退回** | `refund` | DR `WALLET_FROZEN(buyer)` / CR `WALLET_AVAIL(buyer)` | 争议期间冻结不动；裁决后释放或退回；裁决人 ≠ 出款人（SoD） |
| 6.5 | **提现申请** | `withdraw_req` | DR `WALLET_AVAIL(A)` / CR `PENDING_PAYOUT`（单例） | 同事务写 `ledger.withdrawal` 作为申请人明细；账号冻结/停用禁止；大额进入复核 |
| 6.6 | **银行出款确认** | `payout` | DR `PENDING_PAYOUT`（单例） / CR `BANK_DEDICATED` | 金额从提现单读取，以唯一银行回单号确认；与 6.5 一一对应；**24 小时未出款 → 审计关注** |
| 6.7 | **提现失败/撤销** | `payout_fail` | DR `PENDING_PAYOUT`（单例） / CR `WALLET_AVAIL(A)` | 金额与退款钱包从提现单及账户归属读取，调用方不能传入 |
| 6.8 | **仓储费月度出账** | `storage_fee` | DR `WALLET_AVAIL(M)` / CR `PLATFORM_HELD`(category=`storage`) | 日计提在 `wms.storage_fee_daily`（子账，不进总账）；**出账日按账单合计一次扣款**；余额不足则账单 `status=arrears`，**不扣成负数**，待充值后再催收扣款；欠费超阈值 → 审计 #18 |
| 6.9 | **配送费托管**（发货） | `delivery_hold` | DR `WALLET_AVAIL(M)` / CR `DELIVERY_ESCROW`（明细=订单） | 商户资金不足不能发货；金额=规则当时版本计算出的 `fee_total`；写入 `shipment.fee_*` 快照 |
| 6.10 | **签收后分账** | `delivery_release` | DR `DELIVERY_ESCROW`（订单）/ CR `WALLET_AVAIL(D)`（司机份，自建配送时）+ CR `PLATFORM_HELD`(category=`delivery_margin`) + CR `PAYABLE_CARRIER`（中通份） | **必须有签收凭证**；各份用"整数 + 最后一项倒挤"，**之和 = 托管金额**；平台留存默认 0 |
| 6.11 | **取消订单/发货前退托管** | `delivery_refund` | DR `DELIVERY_ESCROW` / CR `WALLET_AVAIL(M)` | 仅当尚未签收分账 |
| 6.12 | **广告预算划扣** | `ad_charge` | DR `WALLET_AVAIL(M)` / CR `AD_BUDGET(M)` | 创建广告时划扣预算；免费演示广告不入账 |
| 6.13 | **广告消耗** | `ad_spend` | DR `AD_BUDGET(M)` / CR `PLATFORM_HELD`(category=`ads`) | 按曝光/点击聚合入账（**按广告、按小时或按天合并一条**，避免每次点击一条流水）；合并键保证幂等 |
| 6.14 | **广告退回** | `ad_refund` | DR `AD_BUDGET(M)` / CR `WALLET_AVAIL(M)` | 终止或被拒后退回剩余预算 |
| 6.15 | **AI 会员费** | `member_fee` | DR `WALLET_AVAIL(A)` / CR `PLATFORM_HELD`(category=`member`) | ⚠️ 数字商品支付规则待核对（见《技术方案》风险）；若改用外部支付，则改为入金 + 本分录两步 |
| 6.16 | **平台收入划出专用账户** | `sweep` | DR `PLATFORM_HELD` / CR `BANK_DEDICATED` | **只能与银行约定的流程触发**；金额不得超过 `PLATFORM_HELD` 余额；双人复核 |
| 6.17 | **应付中通付款** | `carrier_pay` | DR `PAYABLE_CARRIER` / CR `BANK_DEDICATED` | 对应中通对账单；双人复核 |
| 6.18 | **冲销** | `reversal` | 原交易分录**借贷互换**（逐条对称） | 指向 `reverses_txn_id`；已冲销的交易不得再次冲销；需"资金"编辑权限并写审计 |
| 6.19 | **对账差异调整** | `bank_diff` | 差异金额入 `SUSPENSE`（DR/CR `BANK_DEDICATED` 对 `SUSPENSE`） | **`SUSPENSE` 余额必须在 N 天内清零**，超期告警 |
| — | **货款凭证**（买家线下付商家） | 不入账 | 写 `trade.payment_proof` | **不进分录**，流水页并列显示"凭证（仅留痕）" |

#### 6.18 冲销补充规则

- 第一版只允许冲销 `topup`、`storage_fee`、`member_fee`、`ad_charge`、`ad_spend`、`ad_refund`、`bank_diff`、`sweep`、`carrier_pay`、`adjust`。冻结与结算必须走 `hold` 状态机，提现必须走 `withdrawal` 状态机，配送托管需按订单状态处理，不能用通用冲销绕过业务状态。
- 冲销交易必须逐条复制原分录的账户、金额、`role`、`category`，并只互换 `DEBIT` / `CREDIT`。延迟约束触发器在提交时比较两个分录多重集合，防止绕过服务直接写入不对称冲销。
- 同一原交易最多一条冲销；冲销交易本身不能再次冲销。冲销仍执行账户余额非负检查，已花掉的充值不能用冲销强制形成客户负余额。
- 服务调用必须携带非空原因、操作人及授权人；权限判断由 API 层执行。账务事务写 `ledger.txn.reversed` outbox 事件，审计模块消费事件并追加哈希链审计记录，账务模块不跨 schema 写审计表。

### 6-B 商城与 Dr.RouteFit 分工后的分录修订（2026-10-29，待 LedgerAuditor 复核）

依据：项目负责人 2026-10-29 的决定（《试点上线最小范围清单》第 1、6 节；《Dr.life+与Dr.RouteFit对接契约》v0.2 第 4.5、7、8、10 节）。

- Dr.life+ 只做商城，仓储与配送由 Dr.RouteFit 承担。
- 入金优先走 KESS。
- 商户包邮或买家付运费，两种情况下运费都结算给 RouteFit。
- 货款在 RouteFit 签收复核后满 7 天、且无未结售后时，结算给商户。
- Dr.life+ 与 RouteFit 同属一家公司，**只记内部往来账，不实际转账**。

本节**取代**下列行的适用范围：6.8（贷方改变）、6.9、6.10、6.11（停用）、6.17（停用，改为 6-B.8）、“货款凭证”行（停用，被 KESS 付款取代）。已合并的迁移不修改，新增类型与科目用新迁移追加 CHECK 取值；停用的类型保留取值，由服务层不再提供入口，审计 #13 继续能汇总历史数据。

#### 6-B.1 新增科目

- **`CHANNEL_KESS`**：资产，平台单例，借方为正，不允许负数。含义见第四章第 2 节。
- **`PAYABLE_CARRIER`**：语义改为“应付 RouteFit”，结构不变（平台单例，负债）。

#### 6-B.2 新增与改用的分录

| # | 业务 | txn.type | 分录 | 说明与约束 |
|---|---|---|---|---|
| 6-B.3 | **KESS 收款入钱包**（充值，或买家为订单付款） | `topup_kess`（新增） | DR `CHANNEL_KESS` / CR `WALLET_AVAIL(A)` | 只在服务端**主动查单**确认 KESS `SUCCESS` 后入账；幂等键 = `topup_kess:<商户支付号>`，同一支付号只入一次。`UNKNOWN` 不入账。金额取支付意向的服务端应收，不取浏览器回传。**订单付款时，6-B.3 与 6-B.5 必须在同一事务内完成**，钱不能在可用余额里停留，否则买家可在两步之间提现 |
| 6-B.4 | **KESS 结算到银行** | `channel_settle`（新增） | DR `BANK_DEDICATED`（净额）+ DR `PLATFORM_HELD`(category=`channel_fee`，手续费) / CR `CHANNEL_KESS`（毛额） | 按 KESS 结算报表逐批入账；毛额 = 净额 + 手续费；与银行流水逐笔核对。手续费由平台承担（6-B.11 Q1，含余额不足时的处理） |
| 6-B.5 | **订单付款进托管** | `freeze`（沿用 6.2，`FreezeHold`） | DR `WALLET_AVAIL(buyer)` / CR `WALLET_FROZEN(buyer)` | 6-B.3 入账后，同一事务按子订单建**两个 hold**：`hold(order_goods)`（货款，收款方为商户）；买家付运费时另建 `hold(order_shipping)`（运费，收款方为 RouteFit 与平台加价）。商户包邮的子订单只有货款 hold |
| 6-B.6 | **货款结算给商户** | `settle`（沿用 6.3，`SettleHold`） | DR `WALLET_FROZEN(buyer)` / CR `WALLET_AVAIL(M)` | 触发条件：RouteFit `delivered_verified` 后满 7 天（后台可调）且无未结售后。佣金为 0，`Fee` 仍必须为 0 |
| 6-B.7 | **买家运费结算** | `shipping_settle`（新增） | DR `WALLET_FROZEN(buyer)` / CR `PAYABLE_CARRIER`（RouteFit 报价部分）+ CR `PLATFORM_HELD`(category=`delivery_margin`，加价部分) | 触发条件：RouteFit `delivered_verified`，不等 7 天，因为配送服务已完成。金额与拆分取下单时的报价快照：RouteFit 部分 = 报价，加价部分 = 托管额 − 报价（最后一项倒挤）。走 hold 状态机：需新增“按快照拆分结算”的强类型方法，不复用 `SettleHold`（后者只支持单一收款钱包）。加价率为 0 时只有一条贷方分录 |
| 6-B.8a | **退款**（拒单、取消、售后退款） | `refund`（沿用 6.4，`RefundHold`） | DR `WALLET_FROZEN(buyer)` / CR `WALLET_AVAIL(buyer)` | 先退回买家钱包；买家可一键选择原路退回 KESS，流程见 6-B.11 Q3（`withdraw_req` 加新增的 `payout_kess`）。已结算的运费是否退给买家由售后判定，判定退的由商户承担：从商户钱包扣，类型 `adjust`，需审批 |
| 6-B.8b | **仓配费用行扣款：仓储类**（storage、inbound、outbound、packing） | `storage_fee`（沿用类型，**贷方改变**） | DR `WALLET_AVAIL(M)` / CR `PAYABLE_CARRIER` | 金额 = RouteFit 费用行金额（美分 × 100 → e4，精确）。幂等键 = `rf_fee:<feeLineId>:v<version>`。余额不足时不扣成负数，费用行记为欠费，充值后按先后顺序补扣（沿用 6.8 的欠费规则），超过宽限期后暂停出库（R-06） |
| 6-B.8c | **仓配费用行扣款：商户包邮的配送费** | `delivery_fee`（新增） | DR `WALLET_AVAIL(M)` / CR `PAYABLE_CARRIER`（报价部分）+ CR `PLATFORM_HELD`(category=`delivery_margin`，加价部分) | 金额 = 报价 × (1 + 加价率)，取下单时的快照；欠费处理同 6-B.8b |
| 6-B.8d | **费用行调整** | `reversal`（沿用 6.18）后再按新版本扣款 | 原交易借贷互换，再记新的 6-B.8b 或 8c | RouteFit `fee.adjusted`：先冲销旧版本，再按新版本扣款，两笔同一事务。冲销白名单需加入 `delivery_fee` |
| 6-B.9 | **与 RouteFit 内部往来核销**（月度） | `intercompany_settle`（新增） | DR `PAYABLE_CARRIER` / CR `PLATFORM_HELD`(category=`routefit`，新增) | 同一公司，不实际转账：每月双方财务核对，确认“应付 RouteFit 余额 = 累计日结单合计 − 已核销”后，把已确认部分从“应付”重分类为公司自有收入（RouteFit 业务线）。双人复核，附月结单编号。之后与其他平台收入一样，只能经银行约定流程 `sweep`（6.16）划出专用账户；**不得因核销减少客户资金** |

#### 6-B.10 恒等式与审计影响

- 第四章第 3 节恒等式右边加 `CHANNEL_KESS`，左边不变（`PAYABLE_CARRIER` 已在左边）。
- 平台收入 `category` 新增三类，财务中心与审计 #13 按新类别汇总：
  - `channel_fee`：KESS 手续费，由平台承担，记借方，是负向收入；
  - `routefit`：RouteFit 业务线收入；
  - `capital`：公司注资，用于承担手续费，不是收入，财务中心单列。
- 新增审计检查：
  - `CHANNEL_KESS` 余额 = KESS 已确认未结算金额（日）；
  - `PAYABLE_CARRIER` = RouteFit 日结单累计 − 已核销（日）；
  - 买家付运费的子订单 `delivered_verified` 超过 24 小时仍未 `shipping_settle` 的，列为关注。
- 停用的 `DELIVERY_ESCROW` 账户应保持余额 0，审计检查改为“非 0 即异常”。

#### 6-B.11 已确认（项目负责人 2026-10-29）

| # | 事项 | 结论与账务落点 |
|---|---|---|
| Q1 | KESS 手续费由谁承担 | **平台承担**。买家与商户付多少就到账多少；6-B.4 的手续费记 DR `PLATFORM_HELD`(category=`channel_fee`)，作为平台成本。费率与结算周期以 KESS 合同为准（E-4）。**约束**：`PLATFORM_HELD` 不允许负数，而试点期佣金为 0、加价默认为 0，平台收入可能不足以承担手续费。处理顺序：① 6-B.9 核销后的 `routefit` 收入先承担；② 仍不足时，由公司向专用账户注资，新增交易类型 `capital_in`：DR `BANK_DEDICATED` / CR `PLATFORM_HELD`(category=`capital`)，双人复核并附银行回单。不足时 `channel_settle` 整批挂起并告警，不得部分入账 |
| Q2 | RouteFit 业务线收入是否划出专用账户 | **每月核销后划出**。6-B.9 `intercompany_settle` 完成后，与其他平台收入一起，按银行约定流程执行 6.16 `sweep`（DR `PLATFORM_HELD` / CR `BANK_DEDICATED`），双人复核。划出金额不得超过当时 `PLATFORM_HELD` 余额，也不得动用 Q1 中为承担手续费预留的部分 |
| Q3 | 买家退款怎么退 | **默认退回买家钱包，买家可一键选择原路退回**。原路退复用提现状态机，出款渠道为 KESS：申请时沿用 6.5 `withdraw_req`（DR `WALLET_AVAIL(buyer)` / CR `PENDING_PAYOUT`），提现单记录 `channel=kess` 与原商户支付号；财务审核后在 KESS 人工退款，确认后新增 `payout_kess`：DR `PENDING_PAYOUT` / CR `CHANNEL_KESS`；KESS 退款失败或撤销沿用 6.7 `payout_fail`。原路退金额不得超过该支付号已收、未退的金额；KESS 退款手续费（如有）按 Q1 由平台承担 |

#### 6-B.12 实现落点

| 任务 | 落点 |
|---|---|
| P-02 | 6-B.3、6-B.4、6-B.11 Q1 的 `capital_in`、Q3 的 `payout_kess` |
| T-10 | 6-B.5、6-B.6、6-B.7、6-B.8a |
| R-04 | 6-B.8b、8c、8d、6-B.9 |

所有新增类型、科目与类别，用**同一个新迁移**追加到 `ledger.txn.type`、`ledger.account.kind`、`ledger.posting.category` 的 CHECK 中，由 GoSenior 或 DBEngineer 实现，经代码审查 → SecurityReviewer → LedgerAuditor 三道复核。

#### 6-B.13 账务审计后的修订（2026-10-29，回应 LedgerAuditor 在 CMP-655 的 B1–B3 与 A1–A3；**与上文冲突处以本节为准**）

**(1) 手续费改记独立的借方科目，从结构上消除负数（回应 B1、A1-1、A1-2、A1-4）**

- 新增科目 **`PLATFORM_COST`**：平台单例，`normal_side = DEBIT`，不允许负数，表示平台已发生、尚未用平台资金冲抵的成本。
- 6-B.4 `channel_settle` 改为：DR `BANK_DEDICATED`（净额）+ DR `PLATFORM_COST`（手续费）/ CR `CHANNEL_KESS`（毛额）。这笔分录不再依赖 `PLATFORM_HELD` 有余额，任何一批都能入账，因此**取消“整批挂起”**，也不存在挂起期间银行流水无人认领的窗口。
- 恒等式（第四章第 3 节）最终形式：
  ```
  Σ WALLET_AVAIL + Σ WALLET_FROZEN + Σ PENDING_PAYOUT + Σ DELIVERY_ESCROW + Σ AD_BUDGET
    + PAYABLE_CARRIER + PLATFORM_HELD + SUSPENSE
    = BANK_DEDICATED + CHANNEL_KESS + PLATFORM_COST
  ```
- **客户资金不被占用的约束**：平台净头寸 `PLATFORM_HELD − PLATFORM_COST ≥ 0`。
  - 它小于 0，说明手续费正在由客户资金垫付，必须禁止。
  - 保证手段（三层）：① **上线前预存手续费备付金**（`capital_in`，见第 (3) 条），金额 ≥ 预估首月手续费的 2 倍；② 新增每日审计项“平台净头寸 ≥ 0”，低于“预估下月手续费”时告警，小于 0 时列为异常；③ `sweep` 强校验（第 (4) 条）。
- 月度冲抵：`cost_close`（新增）：DR `PLATFORM_HELD`(category=`channel_fee`) / CR `PLATFORM_COST`。双人复核，金额不得使 `PLATFORM_HELD` 为负。冲抵后，财务中心的“平台成本”按 `channel_fee` 类别汇总。
- RouteFit 业务线收入不再“先承担手续费”。它只在月度核销（6-B.9）后进入 `PLATFORM_HELD`，自然增加平台净头寸。

**(2) 对账程序与财务中心必须同批改造（回应 B2）**

- `checkIdentity`（`backend/internal/ledger/recon.go`）与财务中心 `financialPosition`（`financial_center.go`）必须改成上面的恒等式：右边为 `BANK_DEDICATED + CHANNEL_KESS + PLATFORM_COST`，按科目 `normal_side` 区分左右两边，不再写死“`kind <> 'BANK_DEDICATED'` 合计 = 银行”。
- 这项改造与 P-02 **同一个 PR 上线**，不得先于或晚于 P-02。6-B.12 落点表新增一行：**L-07/L-10 改造 → 与 P-02 同批**。
- 新增对账项：
  - `CHANNEL_KESS` 余额 = KESS 已确认、未结算金额（与 KESS 结算报表逐笔核对）；
  - `PLATFORM_COST` 余额 = 已入账手续费 − 已冲抵。

**(3) `capital_in` 的完整定义（回应 A1-3）**

- 新表 `ledger.capital_injection(id, bank_ref text NOT NULL UNIQUE, amount bigint CHECK (> 0), purpose text CHECK (purpose IN ('fee_reserve','other')), maker_actor, checker_actor CHECK (checker_actor <> maker_actor), txn_id, created_at)`。同一张银行回单只能注资一次。
- 分录：DR `BANK_DEDICATED` / CR `PLATFORM_HELD`(category=`capital`)。幂等键 = `capital_in:<bank_ref>`。
- 新增强类型方法 `InjectCapital`：双人复核，`BankRef` 必填，与 `ConfirmPayout` 同一模式。不经 `Adjust`，因为 `Adjust` 的白名单不含 `BANK_DEDICATED`。
- **不受出款冻结限制**：注资只增加资金，冻结期间也必须能注资，否则“恒等式失败 → 全局冻结 → 无法注资”会形成死锁。
- **资金来源（项目负责人 2026-10-29 确认，E-7）**：允许注资。手续费备付金存放在公司另开的**备用金账户**（由一家合作银行开立），不与客户资金专用账户混用。需要时从备用金账户转入专用账户补足手续费，每笔转账对应一张银行回单，即一条 `capital_in`。`ledger.capital_injection` 新增 `source_account_last4`，记录备用金账户尾号，便于对账。备用金账户本身是公司资金，不进平台账务科目，只在注资时留痕。
- **上线前置**：备用金账户开立，且首笔备付金转入专用账户并入账（`capital_in`）。

**(4) `sweep` 不得动用预留（回应 A2）**

- 预留额 `fee_reserve` = 未结算 `CHANNEL_KESS` 上各笔付款的预期手续费 + 已出报表、未入账批次的手续费 + 预估下月手续费。预期手续费按下面的后台设置计算。
- **KESS 手续费后台设置**（项目负责人 2026-10-29）：费率不写死在代码里，平台后台提供设置入口（集成配置 N-10 的 KESS 一项，后台页面 U-08）。
  - 扣费方式二选一：**按订单固定金额**（每笔付款 X 美元，e4 存储）或**按金额比例**（万分比 bps，四舍五入到 e4）。
  - 每个版本有生效时间，只增不改，修改需“集成配置”编辑权限并写审计。
  - 每笔付款按付款时生效的版本计算预期手续费，并快照到支付意向上。
- **用途与边界**：
  - 后台设置**只用于预期**：计算 `fee_reserve`、在 KESS 结算报表到达时逐笔核对手续费。
  - **实际入账永远以 KESS 结算报表为准**（6-B.4）。
  - 报表手续费与预期不一致的，生成对账差异并告警，由财务核对合同或更新设置。不按设置值改写入账金额。
- `sweep` 服务层强校验：`amount ≤ PLATFORM_HELD − PLATFORM_COST − fee_reserve`。每次 `sweep` 都要把当时的 `PLATFORM_HELD`、`PLATFORM_COST`、`fee_reserve` 及其计算输入落库到划款明细表 `ledger.sweep_detail`，事后可审计。
- 新增每日审计项：`PLATFORM_HELD − PLATFORM_COST ≥ fee_reserve`，不满足时告警。

**(5) 内部核销的前置条件与跨期调整（回应 B3）**

- 6-B.9 `intercompany_settle` 只能核销同时满足两个条件的费用行：
  - 已与 RouteFit 日结单逐行对账、无差异；
  - 已过**费用调整窗口**：服务日期所在月结束后 10 天。窗口天数写入对接契约 7.3，RouteFit 在窗口后不得再发 `fee.adjusted`。
- 核销金额不得超过 `PAYABLE_CARRIER` 余额；`PAYABLE_CARRIER.allow_negative` 保持 false。
- 窗口后仍出现的调整，不走 6.18 冲销，走新增的 **`fee_period_adjust`**（跨期调整），双人复核：
  - 多收商户（退商户）：DR `PLATFORM_HELD`(category=`routefit`) / CR `WALLET_AVAIL(M)`；
  - 少收商户（补扣）：按 6-B.8b、8c 正常扣款。

**(6) 原路退回 KESS 的两条路径与表结构（回应 A3）**

- **项目负责人 2026-10-29 决定：试点只支持“未结算付款的原路退”。**判定依据是该支付号所在的 KESS 批次**是否已经 `channel_settle` 入账**，由服务层按批次记录判断，不由操作人选择：
  - **未入账**：可以原路退。`payout_kess` = DR `PENDING_PAYOUT` / CR `CHANNEL_KESS`，金额不得超过该支付号在本批次中的未结算金额。
  - **已入账**：不提供原路退。退款留在买家钱包，买家可走银行提现（6.5–6.7）。买家端“原路退回”按钮只对未入账的支付显示；按钮显示后批次才入账的，服务层拒绝并提示改为提现。
- `CHANNEL_KESS` **不允许为负**（`allow_negative = false`，不设例外）。`payout_kess` 因批次已入账而余额不足时，整笔回滚，不会动用其他买家的未结算款，因为金额上限已按该支付号本批次的未结算额限定。
- 新增对账项：每笔 `payout_kess` 都能追溯到同一批次内的原支付号，且该批次在退款时尚未入账。
- **表结构**：`ledger.withdrawal` 新增 `channel text NOT NULL DEFAULT 'bank' CHECK (channel IN ('bank','kess'))` 和 `kess_trade_no text`（`channel = kess` 时必填，引用原支付号）。支付号不写进 `payee_account_text`、`payee_account_last4`。
  - `channel = kess` 不得走 `ConfirmPayout`（它硬编码 CR `BANK_DEDICATED`），只能走 `ConfirmKessRefund`；`channel = bank` 不得走后者。两个方法互相拒绝对方的渠道。
- **风控**：原路退款把钱退回原支付来源，不经用户提供的新收款账户。
  - **适用**：大额双人复核（沿用提现阈值）、全局出款冻结（冻结期间暂停，与提现一致）。
  - **豁免**：账号冻结或停用（买家被冻结也能拿回退款）、二次验证。

**(7) 落点表补充**

| 任务 | 补充落点 |
|---|---|
| P-02 | `PLATFORM_COST` 科目、`cost_close`、`capital_in`（`ledger.capital_injection`）、`payout_kess` 两条路径与 `ledger.withdrawal` 新增列、L-07/L-10 恒等式改造（同一 PR） |
| R-04 | 6-B.9 的前置条件、`fee_period_adjust`、`sweep` 预留校验与 `ledger.sweep_detail` |

**(8) 外部事项**

- **E-4**：KESS 手续费的费率与口径仍需从 KESS 合同取得（决定 `fee_reserve` 的计算）。退款扣回方式已不影响设计，因为试点只做未结算付款的原路退。
- **E-7**：已确认方向。待办是在一家合作银行开立公司备用金账户，并完成首笔备付金转入；完成前 P-02 不得上线。

### 配套表
```sql
create table ledger.hold (          -- 冻结明细
  id bigint generated always as identity primary key,
  account_id bigint not null references ledger.account(id),   -- WALLET_FROZEN 账户
  ref_type text not null, ref_id bigint not null,              -- plan_batch / 其他
  amount bigint not null check (amount > 0),
  status text not null check (status in ('held','released','refunded','disputed')),
  freeze_txn_id bigint not null references ledger.txn(id),
  close_txn_id bigint references ledger.txn(id),
  created_at timestamptz not null default now(), closed_at timestamptz,
  dispute_reason text, disputed_by bigint, disputed_at timestamptz,
  resolved_by bigint,
  unique (ref_type, ref_id)
);

create table ledger.withdrawal (
  id bigint generated always as identity primary key,
  public_id text unique not null,
  request_key text unique not null,                       -- API Idempotency-Key；仅 A-Z/a-z/0-9/_/-，最长 100
  account_id bigint not null,                           -- 申请人账号
  amount bigint not null check (amount > 0),
  payee_account_text text not null,                     -- 收款账户（加密）
  payee_account_last4 text,
  status text not null check (status in ('requested','review','approved','paid','failed','cancelled')),
  requester_actor bigint not null,                      -- 申请提现的用户 actor
  maker_actor bigint, checker_actor bigint,             -- 管理员发起处理；大额由另一管理员复核
  requires_review boolean not null,                     -- 申请时按阈值固定，后续调阈值不追溯
  req_txn_id bigint references ledger.txn(id), paid_txn_id bigint references ledger.txn(id),
  bank_ref text, requested_at timestamptz not null default now(), reviewed_at timestamptz, paid_at timestamptz,
  failed_reason text,
  check (checker_actor is null or (maker_actor is not null and checker_actor <> maker_actor and checker_actor <> requester_actor))
);
create index ix_wd_status on ledger.withdrawal(status, requested_at);
create unique index uq_withdrawal_bank_ref on ledger.withdrawal(bank_ref) where bank_ref is not null;

create table ledger.outbox (        -- 事务性发件箱：账已入则事件必发
  id bigint generated always as identity primary key,
  txn_id bigint, topic text, payload jsonb, created_at timestamptz default now(), sent_at timestamptz
);
```

`hold` 状态机为 `held → released | refunded | disputed`，以及 `disputed → released | refunded`；`released/refunded` 为终态。争议只改变状态与争议元数据，不动余额；必须通过 `ResolveDispute` 裁决后才可释放或退款，且裁决人与出款人不得相同。第一版一个 hold 只能整笔结算或整笔退回，部分结算待确认。

冻结操作通过 `FreezeHold` 同事务写冻结分录与 hold；释放、退款通过 `SettleHold`、`RefundHold`，金额和冻结账户均取自 hold，调用方不能传入。第一版 `SettleHold.Fee` 必须为 0，参数仅为以后确定收费规则保留。L-03 的无状态冻结、结算、退款函数为包内私有，外部模块不能绕过状态机。锁顺序固定为：先由 `Post` 按 `account_id` 升序锁定余额行，再锁 hold 行。数据库触发器限制合法状态迁移，保护金额、账户、业务引用、冻结交易及创建时间不可变，并拒绝删除和截断。

每日对账除总账恒等式外，还必须验证每个 `WALLET_FROZEN` 账户的余额等于该账户所有 `held/disputed` hold 金额之和；`VerifyHoldsMatchFrozen` 返回全部不一致账户。

提现状态机为：大额 `requested → review → approved → paid`，小额在申请事务中直接进入 `approved`；`requested/review/approved → cancelled`，`approved → failed`，三个结束状态均不可再修改。默认大额阈值为 1,000 美元（`10_000_000` e4），申请时写入 `requires_review` 固定判定。申请、出款确认、失败和撤销均通过 `Post` 的事务钩子把分录与提现状态一次提交；金额和申请人钱包以提现行及 `owner_account` 关系为准。全局锁顺序与 hold 相同：先按账户编号升序锁余额，再锁 withdrawal 行。

提现申请必须携带 API 层传入的 `Idempotency-Key`，保存为不可变的 `request_key`；格式限定为 `[A-Za-z0-9_-]` 且最长 100 个字符。事务幂等键为 `withdraw_req:request:<request_key>`。重放时必须核对申请人账号、钱包、金额及解密后的收款账户；任一内容不同均返回幂等冲突。复核开始、复核通过、出款确认、失败和撤销分别写事务性 outbox 事件，事件只包含提现 `public_id`、金额、相关操作人和时间，不包含收款账户全文。

提现前由调用方完成真正的二次验证，并通过注入的 `AccountGuard` 检查账号可提现；任一检查缺失均默认拒绝。收款账户使用信封加密，AAD 为 `withdrawal_payee:<public_id>`，普通查询只返回末四位，全文仅由专用解密方法读取。银行回单号全表唯一。L-03 的无状态 `WithdrawReq/Payout/PayoutFail` 已收为包内方法，外部只能使用提现状态机。`VerifyPendingPayoutMatchesWithdrawals` 校验单例 `PENDING_PAYOUT` 余额等于所有 `requested/review/approved` 提现之和；24 小时未完成和所有大额未完成提现进入审计关注，列表不含收款账户全文。

> **待确认：** `approved` 状态撤销与银行实际付款存在并发风险。当前约束要求流程保证：一旦已提交银行转账，不得再撤销；后续接入银行适配层时需确认可执行的状态凭证和互斥方案。

---

## 七、对账与审计

### 1. 三层对账
| 层 | 内容 | 频率 | 失败处理 |
|---|---|---|---|
| ① 内部 | 所有账户余额缓存 = 分录重算；全部分录借=贷；第三章恒等式 | 每次入账抽检 + **每日全量** | 报"异常"，冻结相关出款，通知财务和审计 |
| ② 银行 | **导入银行对账文件**，与 `BANK_DEDICATED` 分录逐笔匹配（按金额、日期、银行参考号） | 每日 | 未匹配项列入"对账差异"，差额入 `SUSPENSE`，必须人工处理并留理由 |
| ③ 外部 | 应付中通 = 中通对账单；平台收入划出有凭证 | 每月 | 差异单列，处理后做 `carrier_pay`/调整 |

```sql
create table ledger.bank_statement_line (
  id bigint generated always as identity primary key,
  bank_ref text not null, booked_on date not null, amount bigint not null, direction text not null check (direction in ('IN','OUT')),
  counterparty text, raw jsonb,
  matched_txn_id bigint references ledger.txn(id), match_status text not null default 'unmatched' check (match_status in ('unmatched','matched','ignored','suspense')),
  imported_batch text not null, unique (bank_ref, booked_on, amount, direction)
);
create table ledger.snapshot_daily (   -- 每日余额快照（用于对账、报表、审计留证）
  day date, account_id bigint, balance bigint, primary key (day, account_id)
);

-- 内部对账运行、异常明细与出款冻结（完整约束见迁移 00010）
create table ledger.recon_run (
  id bigint generated always as identity primary key,
  day date not null, kind text not null check (kind in ('daily','spot')),
  attempt integer not null, started_by bigint, force_reason text,
  started_at timestamptz not null, finished_at timestamptz,
  status text not null check (status in ('running','ok','failed')),
  summary jsonb not null, unique (day, kind, attempt)
);
create table ledger.recon_issue (
  id bigint generated always as identity primary key,
  run_id bigint not null references ledger.recon_run(id), check_code text not null,
  account_id bigint references ledger.account(id), txn_id bigint references ledger.txn(id),
  expected bigint, actual bigint, detail text, created_at timestamptz not null
);
create table ledger.payout_freeze (
  id bigint generated always as identity primary key,
  scope text not null check (scope in ('global','account')), account_id bigint references ledger.account(id),
  reason text not null, run_id bigint not null references ledger.recon_run(id), created_at timestamptz not null,
  released_at timestamptz, released_by bigint, release_reason text
);
```

内部对账先按 `LEDGER_RECON_TIMEZONE`（默认 `Asia/Phnom_Penh`）的日界线写入只增不改的余额快照，再在**同一个只读 `REPEATABLE READ` 事务**中执行 `BALANCE_CACHE`、`TXN_BALANCED`、`IDENTITY`、`HOLD_FROZEN`、`PENDING_PAYOUT`、`REVERSAL`、`ORPHAN` 七项检查。快照重复写使用 `ON CONFLICT DO NOTHING`；已有快照与分录重算不一致时记录 `SNAPSHOT_DRIFT`。

任何异常都写 `recon_issue` 与 `ledger.recon.failed` outbox 事件。`BALANCE_CACHE` 与 `HOLD_FROZEN` 异常建立该冻结账户同一 `owner_account` 的 `WALLET_AVAIL` 账户级出款冻结；`PENDING_PAYOUT`、`TXN_BALANCED`、`IDENTITY`、`SNAPSHOT_DRIFT`、`REVERSAL`、`ORPHAN` 建立全局冻结。冻结阻止提现申请、出款确认、平台归集及承运商付款，不阻止提现失败或撤销时将款项退还用户。解除必须给出理由；人工发起的对账，其发起人不能解除自己触发的冻结。

每日快照和每日对账只允许处理已经结束的业务日；当日和未来日期不产生快照、运行记录或冻结。默认重复执行同一天的每日对账会返回已有完成结果。若最新运行仍为 `running`，未超过 30 分钟默认超时阈值则返回运行中错误；超过阈值则标记为 `failed`（`STALE_RUN`）、写 `ledger.recon.stale_run`，再以递增 `attempt` 继续。显式强制重跑必须填写原因，并以递增 `attempt` 留下新记录；因此唯一键采用 `(day, kind, attempt)`，保留每次强制重跑的审计证据。抽检仅比较相关账户余额缓存；正常抽检不创建 `recon_run`，仅发现不一致时创建 `kind='spot'` 运行并冻结账户。已完成运行、异常明细与每日快照不可更新、删除或截断；冻结记录仅允许一次性填写 `released_*` 字段。

### 2. 与审计中心 18 项检查的对应
| 审计项 | 数据来源 |
|---|---|
| #1 总账平衡 | `posting` 借贷合计相等 |
| #2 账户对账差异 | `balance` 与分录重算 |
| #3/#16 大额与超时提现 | `withdrawal.status`、`requested_at` |
| #4 负余额 | `balance < 0`（理论上被约束拦截，仍每日检查） |
| #11 配送费托管异常 | `shipment` 状态 vs `DELIVERY_ESCROW` 明细 |
| #12 流水完整性 | 必填字段、流水号唯一、分账之和 |
| #13 收入与流水一致 | `PLATFORM_HELD` 各 `category` 汇总 = 财务中心 |
| #17 收款账户重复 | `withdrawal.payee_account_*` 多账号共用 |
| #18 商户欠费 | `storage_statement.status='arrears'` 金额 |
| 新增 | `SUSPENSE` 超期未清零；恒等式（第四章 3）不成立 |

### 3. 审计日志（`audit`）— 哈希链
```sql
create table audit.log (
  id bigint generated always as identity primary key,
  ts timestamptz not null,
  actor_id bigint not null, actor_role text not null, action text not null, target text not null,
  detail jsonb not null,                      -- 含脱敏后的前后值
  ip inet, request_id text not null,
  prev_hash bytea not null, hash bytea not null
);
-- chain_head 单行加锁串行化追加；event_seen 按 event_id 幂等；anchor 保存每日链尾；check_result 供 18 项检查。
-- hash = SHA-256(每字段 uint64 大端长度前缀 + 字段内容)：prev_hash、UTC RFC3339Nano 时间、actor_id、
-- actor_role、action、target、规范 JSON detail、ip、request_id；创世 prev_hash 为 32 字节全零。
-- audit_writer 只给 INSERT、SELECT 和受限链头函数 EXECUTE；触发器拒绝 UPDATE/DELETE/TRUNCATE；
-- 每日把当天最后一个 hash 写入异地（防止整表被替换）。锚点异地位置待运维确认，审计默认永久保留。
```

---

## 八、其他模块的表（概要）

| schema | 表 | 要点 |
|---|---|---|
| `comms` | `message`(to_account, event, params jsonb, read_at)、`delivery_log`(message_id, channel, bot_id, ok, why)、`template`(event, text jsonb 三语) | 每次发送一条记录，含选用的 Bot 与失败原因；P0 资金类全渠道失败时记短信兜底 |
| `ai` | `conversation`/`message`（**正文加密**）、`usage`(account, month, n)、`membership`(account, until)、`provider`(id, name, priority, status, endpoint, key_enc, monthly_budget_e4)、`call_log`(account, provider, ok, ms, tokens, failed_providers[])、`basic_info`(account, key, value, status(`ai/ok`), src, suggested)、`tag`(account, key, weight, src jsonb, at) | 聊天原文只保留约定期限；画像表不含原文；`call_log` 用于网关统计与预算 |
| `ops` | `ad_slot`(place, on, model, price)、`ad_campaign`(merchant, place, kind, ref, model, price, budget, spent, imp, clk, status)、`notice`、`channel_acq`(account, channel, by)、`relation`(up, down, type, src, at)、`legal_doc`(kind, n, ver, eff, zh, en, km, note, published_by, published_at)、`consent`(account, kind, ver, at)、`event`(ts, ev, uid, role, lang, sess, props jsonb，**按天分区，保留约 13 个月**)、`integration`(id, group, provider, env, endpoint, key_enc, status, last)、`setting`(key, value, updated_by)、`fx_rate`(base, quote, rate, effective_from) | `legal_doc` 已发布版本只增不改；`consent` 带版本和时间；`event` 不含个人信息 |
| `audit` | `log`、`check_result`(day, check_id, severity, detail) | 见第七章 |

---

## 九、索引、分区与容量

- **热点索引：** `order(buyer_account_id, created_at desc)`、`order(merchant_id, status)`、`posting(account_id, id)`、`stock_move(product_id, id)`、`message(to_account, read_at)`、`withdrawal(status, requested_at)`、`product` 的 `gin(search_text)` 与 `gin(name jsonb_path_ops)` 以及 `pg_trgm`。
- **分区：** `ops.event`、`catalog.search_log`、`ai.call_log` 按**天/月分区**，到期直接 `DROP PARTITION`；`ledger.posting` 数据量大时按**月**分区（保留所有历史，不删）。
- **容量假设：** 首批 3–5 商户，年交易几万笔；账务表十年内不会成为瓶颈，重点是**正确性**而不是规模；留 20 倍余量即可。
- **备份：** 主库每日全量 + WAL 连续归档到异地加密存储；备份每季度做恢复演练；从库用于报表和故障切换。
- **读写分离：** 财务中心、审计、总览等报表走从库；账务写入和需要强一致的读（提现前查余额）走主库。

---

## 十、迁移与种子数据

- **初始数据：** 科目表（`ledger.account` 的单例账户）、角色权限默认矩阵、6 类管理员角色、分类、费率（仓储费 14 美元/立方米/月起）、配送规则版本 1、Bot 池占位、协议 v1.0（三语草案）、汇率。
- **演示数据：** 仅开发/测试环境加载（订单、需求、商户），**生产不得加载**；演示数据需通过真实流程（调用账务模块）生成，不直接插表。
- **每次迁移的检查清单：** 向前兼容、可回滚、账务表变更双人评审、迁移后跑**账务不变量测试**（借贷平衡、恒等式、非负）。

---

## 十一、测试清单（账务必须全部通过才能上线）

| 类别 | 用例 |
|---|---|
| 平衡 | 每种业务动作（6.1–6.19）入账后借=贷；随机大量交易后恒等式成立 |
| 幂等 | 同一 `idem_key` 重复请求 N 次只入账一次，返回相同结果 |
| 并发 | 同一商户并发发货/提现/广告划扣，**余额不为负、不重复扣款**；死锁测试（交叉账户并发） |
| 分账 | 配送费分三方之和恒等于托管金额（含奇数、极小金额、舍入边界） |
| 不可变 | 尝试 UPDATE/DELETE 流水和审计日志必须失败 |
| 冲销 | 冲销后账户回到原值；重复冲销被拒；冲销链可追溯 |
| 提现 | 发起人 = 复核人被数据库拒绝；失败回滚；24 小时超时检测 |
| 对账 | 导入银行文件：匹配、未匹配、重复导入；差异入 `SUSPENSE` 并告警 |
| 权限 | 非账务角色无法直接写 `ledger` 表（数据库账号层面） |
| 灾备 | 从备份恢复后，恒等式成立，`balance` 与分录重算一致 |

---

## 十二、待确认（不阻塞开工，但影响细节）

1. **银行专用账户的对账文件格式与入出金回调**（决定 6.1、6.6 的触发方式与匹配规则）。
2. **平台收入划出与应付中通的付款流程**（6.16、6.17 的审批与凭证）。
3. **AI 会员费的支付方式**（6.15 可能变为"外部支付入金 + 记账"两步）。
4. **瑞尔显示汇率的来源**（手工维护还是接央行/银行牌价）。
5. **仓储费出账日与欠费处理策略**（欠费是否限制发货、宽限期多长）。
6. **数据是否必须留在柬埔寨境内**（影响异地备份位置）。
