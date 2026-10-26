-- +goose Up
-- catalog 模块表结构（任务 T-01）。
-- 编号 00012：main 最大为 00010，00011 已被 A-02 分支占用。
-- schema catalog 与角色 catalog_rw / report_ro 已由 00001/00002 建立，本迁移只建表、索引与种子分类。

-- 商品分类。内置分类 is_custom = false 且三语齐全；商家自定义分类由商家填名称。
CREATE TABLE catalog.category (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL CONSTRAINT ux_category_public_id UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    code text NOT NULL CONSTRAINT ux_category_code UNIQUE CHECK (code ~ '^[a-z][a-z0-9_]{1,31}$'),
    name jsonb NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    default_zone text,
    sort integer NOT NULL DEFAULT 0,
    is_custom boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON COLUMN catalog.category.name IS '三语名称 jsonb：{"zh":…,"en":…,"km":…}，上架/启用由应用层校验齐全';
COMMENT ON COLUMN catalog.category.default_zone IS '默认配送分区代码；分区取值待确认，暂不设 CHECK';
COMMENT ON COLUMN catalog.category.is_custom IS '商家自定义分类（不做多语言翻译）';
CREATE INDEX ix_category_sort ON catalog.category (sort);

-- 商品。merchant_id 指向 iam.merchant.id，但按 AGENTS.md 规则 8 不建跨 schema 外键，
-- 归属校验由服务端的 MerchantDirectory 端口完成。
CREATE TABLE catalog.product (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL CONSTRAINT ux_product_public_id UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    merchant_id bigint NOT NULL,
    category_id bigint NOT NULL REFERENCES catalog.category(id),
    name jsonb NOT NULL,
    spec jsonb,
    unit text NOT NULL,
    origin text NOT NULL CHECK (origin IN ('cn', 'lc')),
    lead_days integer CHECK (lead_days IS NULL OR lead_days >= 0),
    vol_m3 numeric(12,4) CHECK (vol_m3 IS NULL OR vol_m3 >= 0),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'review', 'on', 'off', 'rejected')),
    source text NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'amazon', 'shopee', 'a1688', 'csv')),
    source_ref text,
    std_score smallint CHECK (std_score IS NULL OR (std_score >= 0 AND std_score <= 100)),
    search_text tsvector GENERATED ALWAYS AS (
        to_tsvector('simple',
            coalesce(name ->> 'zh', '') || ' ' || coalesce(name ->> 'en', '') || ' ' || coalesce(name ->> 'km', ''))
    ) STORED,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON COLUMN catalog.product.name IS '三语名称 jsonb；上架前必须三语齐全（应用层校验）';
COMMENT ON COLUMN catalog.product.std_score IS '商品标准分 0–100，由 T-02 重算，本任务只存列';
CREATE INDEX ix_product_search_text ON catalog.product USING gin (search_text);
CREATE INDEX ix_product_name ON catalog.product USING gin (name jsonb_path_ops);
CREATE INDEX ix_product_name_trgm ON catalog.product USING gin (
    (coalesce(name ->> 'zh', '') || ' ' || coalesce(name ->> 'en', '') || ' ' || coalesce(name ->> 'km', '')) public.gin_trgm_ops
);
CREATE INDEX ix_product_merchant_status ON catalog.product (merchant_id, status);
CREATE INDEX ix_product_category_status ON catalog.product (category_id, status);

-- 阶梯价。至少 2 档、起订量唯一由应用层要求；这里用唯一约束兜底起订量不重复。
CREATE TABLE catalog.product_tier (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    product_id bigint NOT NULL REFERENCES catalog.product(id) ON DELETE CASCADE,
    min_qty numeric(18,3) NOT NULL CHECK (min_qty > 0),
    unit_price_e4 bigint NOT NULL CHECK (unit_price_e4 >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ux_product_tier_min_qty UNIQUE (product_id, min_qty)
);
COMMENT ON COLUMN catalog.product_tier.unit_price_e4 IS '阶梯单价，bigint 万分之一美元';

-- 商品导入批次。导入后商品 status = review（应用层负责）。
CREATE TABLE catalog.product_import (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    merchant_id bigint NOT NULL,
    source text NOT NULL CHECK (source IN ('amazon', 'shopee', 'a1688', 'csv')),
    status text NOT NULL DEFAULT 'review' CHECK (status IN ('draft', 'review', 'on', 'off', 'rejected')),
    items jsonb NOT NULL DEFAULT '[]'::jsonb,
    rights_ack boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON COLUMN catalog.product_import.rights_ack IS '商家确认拥有导入商品图片/内容的使用权，默认为否';
CREATE INDEX ix_product_import_merchant ON catalog.product_import (merchant_id, created_at DESC);

-- 搜索同义词组（三语混排），一组至少两个词。
CREATE TABLE catalog.search_synonym (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    terms text[] NOT NULL CHECK (array_length(terms, 1) >= 2),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- 搜索日志：按天分区，热词与联想来源。默认分区兜底，日分区由运维按需创建，到期整体 DROP。
CREATE TABLE catalog.search_log (
    id bigint GENERATED ALWAYS AS IDENTITY,
    account_id bigint,
    q text NOT NULL CHECK (btrim(q) <> ''),
    result_n integer CHECK (result_n IS NULL OR result_n >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);
CREATE TABLE catalog.search_log_default PARTITION OF catalog.search_log DEFAULT;
CREATE INDEX ix_search_log_created_at ON catalog.search_log (created_at DESC);
CREATE INDEX ix_search_log_q ON catalog.search_log USING gin (q public.gin_trgm_ops);

-- 默认权限已给 report_ro 整表 SELECT；搜索日志含账号与检索内容，收窄到不含 account_id 的列。
REVOKE SELECT ON catalog.search_log FROM report_ro;
GRANT SELECT (id, q, result_n, created_at) ON catalog.search_log TO report_ro;
-- 默认分区在 00002 的 ALTER DEFAULT PRIVILEGES 下同样产生整表 SELECT，必须单独收窄。
REVOKE SELECT ON catalog.search_log_default FROM report_ro;
GRANT SELECT (id, q, result_n, created_at) ON catalog.search_log_default TO report_ro;

-- seed:start
INSERT INTO catalog.category (public_id, code, name, enabled, sort, is_custom) VALUES
    ('01JCATCA000000000000000001', 'c1', '{"zh":"橱柜","en":"Cabinets","km":"ទូផ្ទះបាយ"}', true, 1, false),
    ('01JCATCA000000000000000002', 'c2', '{"zh":"地板","en":"Flooring","km":"កម្រាលឥដ្ឋ"}', true, 2, false),
    ('01JCATCA000000000000000003', 'c3', '{"zh":"卫浴洁具","en":"Bathroom","km":"បន្ទប់ទឹក"}', true, 3, false),
    ('01JCATCA000000000000000004', 'c4', '{"zh":"瓷砖","en":"Tiles","km":"ក្បឿង"}', true, 4, false),
    ('01JCATCA000000000000000005', 'c5', '{"zh":"门窗","en":"Doors & windows","km":"ទ្វារ បង្អួច"}', true, 5, false),
    ('01JCATCA000000000000000006', 'c6', '{"zh":"家具","en":"Furniture","km":"គ្រឿងសង្ហារិម"}', true, 6, false),
    ('01JCATCA000000000000000007', 'c7', '{"zh":"基础建材","en":"Basic materials","km":"សម្ភារៈមូលដ្ឋាន"}', true, 7, false),
    ('01JCATCA000000000000000008', 'c8', '{"zh":"墙饰涂料","en":"Paint & wall","km":"ថ្នាំលាប"}', true, 8, false)
ON CONFLICT (code) DO NOTHING;
-- seed:end

-- +goose Down
DROP TABLE catalog.search_log;
DROP TABLE catalog.search_synonym;
DROP TABLE catalog.product_import;
DROP TABLE catalog.product_tier;
DROP TABLE catalog.product;
DROP TABLE catalog.category;