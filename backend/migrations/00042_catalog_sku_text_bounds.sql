-- +goose Up
-- Keep externally supplied SKU values bounded before the unique btree index is reached.
ALTER TABLE catalog.sku
    ADD CONSTRAINT ck_sku_merchant_sku_code_length
        CHECK (merchant_sku_code IS NULL OR char_length(merchant_sku_code) <= 128),
    ADD CONSTRAINT ck_sku_unit_length
        CHECK (unit IS NULL OR char_length(unit) <= 32);

-- +goose Down
ALTER TABLE catalog.sku
    DROP CONSTRAINT ck_sku_unit_length,
    DROP CONSTRAINT ck_sku_merchant_sku_code_length;
