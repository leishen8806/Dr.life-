-- +goose Up
-- SKU 是 catalog.product 的销售变体；product_tier 保留为旧读模型，后续版本再移除。
ALTER TABLE catalog.category
    ADD COLUMN km_pending_review boolean NOT NULL DEFAULT false;

CREATE TABLE catalog.category_attribute (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    category_id bigint NOT NULL REFERENCES catalog.category(id) ON DELETE CASCADE,
    key text NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{1,31}$'),
    name jsonb NOT NULL,
    type text NOT NULL CHECK (type IN ('enum', 'text', 'number')),
    unit text,
    required boolean NOT NULL DEFAULT false,
    is_sku_dimension boolean NOT NULL DEFAULT false,
    value_json jsonb,
    km_pending_review boolean NOT NULL DEFAULT false,
    sort integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ux_category_attribute_key UNIQUE (category_id, key),
    CHECK (type <> 'enum' OR (value_json IS NOT NULL AND jsonb_typeof(value_json) = 'array' AND jsonb_array_length(value_json) >= 1)),
    CHECK (type = 'enum' OR value_json IS NULL),
    CHECK (NOT is_sku_dimension OR type = 'enum')
);
CREATE INDEX ix_category_attribute_category_sort ON catalog.category_attribute (category_id, sort, id);

CREATE TABLE catalog.sku (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL CONSTRAINT ux_sku_public_id UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    product_id bigint NOT NULL REFERENCES catalog.product(id) ON DELETE CASCADE,
    attribute_values jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(attribute_values) = 'object'),
    merchant_sku_code text,
    status text NOT NULL DEFAULT 'off' CHECK (status IN ('on', 'off')),
    unit text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ux_sku_product_attribute_values UNIQUE (product_id, attribute_values),
    CONSTRAINT ux_sku_product_merchant_sku_code UNIQUE (product_id, merchant_sku_code)
);
CREATE INDEX ix_sku_product_status ON catalog.sku (product_id, status);
COMMENT ON COLUMN catalog.sku.unit IS 'NULL inherits catalog.product.unit; unit ratios belong to logistics.sku_map in R-05.';

CREATE TABLE catalog.sku_tier (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    sku_id bigint NOT NULL REFERENCES catalog.sku(id) ON DELETE CASCADE,
    min_qty numeric(18,3) NOT NULL CHECK (min_qty > 0),
    unit_price_e4 bigint NOT NULL CHECK (unit_price_e4 >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ux_sku_tier_min_qty UNIQUE (sku_id, min_qty)
);

-- Existing products receive an empty-attribute default SKU.  The fixed prefix and identity suffix form a valid 26-char ULID-shaped public id.
INSERT INTO catalog.sku (public_id, product_id, attribute_values, status)
SELECT '01H5K0' || lpad(p.id::text, 20, '0'), p.id, '{}'::jsonb,
       CASE WHEN p.status = 'on' THEN 'on' ELSE 'off' END
FROM catalog.product p;

INSERT INTO catalog.sku_tier (sku_id, min_qty, unit_price_e4, created_at, updated_at)
SELECT s.id, pt.min_qty, pt.unit_price_e4, pt.created_at, pt.updated_at
FROM catalog.product_tier pt
JOIN catalog.sku s ON s.product_id = pt.product_id AND s.attribute_values = '{}'::jsonb;

-- +goose Down
DROP TABLE catalog.sku_tier;
DROP TABLE catalog.sku;
DROP TABLE catalog.category_attribute;
ALTER TABLE catalog.category DROP COLUMN km_pending_review;
