-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE
    fractional_rows text;
BEGIN
    SELECT string_agg(row_text, E'\n' ORDER BY row_text)
    INTO fractional_rows
    FROM (
        SELECT format('catalog.product_tier id=%s product_id=%s min_qty=%s', id, product_id, min_qty) AS row_text
        FROM catalog.product_tier
        WHERE min_qty <> trunc(min_qty)
        UNION ALL
        SELECT format('catalog.sku_tier id=%s sku_id=%s min_qty=%s', id, sku_id, min_qty) AS row_text
        FROM catalog.sku_tier
        WHERE min_qty <> trunc(min_qty)
    ) AS invalid_tiers;

    IF fractional_rows IS NOT NULL THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = E'fractional catalog tier min_qty rows block migration 00057:\n' || fractional_rows;
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE catalog.product_tier
    ADD CONSTRAINT ck_product_tier_min_qty_integer CHECK (min_qty = trunc(min_qty));
ALTER TABLE catalog.sku_tier
    ADD CONSTRAINT ck_sku_tier_min_qty_integer CHECK (min_qty = trunc(min_qty));

-- +goose Down
ALTER TABLE catalog.sku_tier
    DROP CONSTRAINT ck_sku_tier_min_qty_integer;
ALTER TABLE catalog.product_tier
    DROP CONSTRAINT ck_product_tier_min_qty_integer;
