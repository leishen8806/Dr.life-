-- +goose Up
CREATE TABLE logistics.sku_map (
    product_id bigint PRIMARY KEY REFERENCES catalog.product(id) ON DELETE CASCADE,
    routefit_sku_id text NOT NULL CHECK (length(btrim(routefit_sku_id)) > 0),
    warehouse_id text NOT NULL CHECK (length(btrim(warehouse_id)) > 0),
    unit_ratio numeric(20,8) NOT NULL DEFAULT 1 CHECK (unit_ratio > 0),
    state text NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'disabled')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX ix_logistics_sku_map_routefit_active ON logistics.sku_map (routefit_sku_id, warehouse_id) WHERE state = 'active';
GRANT SELECT ON logistics.sku_map TO logistics_rw;

-- +goose Down
REVOKE SELECT ON logistics.sku_map FROM logistics_rw;
DROP INDEX logistics.ix_logistics_sku_map_routefit_active;
DROP TABLE logistics.sku_map;
