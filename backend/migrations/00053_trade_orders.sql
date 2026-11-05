-- +goose Up
-- T-05a order-domain tables. Cross-module identities are opaque snapshots;
-- trade does not join or constrain iam/catalog schemas.
CREATE TABLE trade.buyer_order (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    buyer_account_id bigint NOT NULL,
    total_e4 bigint NOT NULL CHECK (total_e4 >= 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE trade.merchant_order (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    buyer_order_id bigint NOT NULL REFERENCES trade.buyer_order(id),
    merchant_id bigint NOT NULL,
    external_order_id text GENERATED ALWAYS AS ('DRL-' || public_id) STORED UNIQUE,
    status text NOT NULL DEFAULT 'awaiting_payment' CHECK (status IN (
        'awaiting_payment','paid','submitted','accepted','in_fulfillment','shipped','delivered_verified','settled',
        'rejected','cancelled','delivery_failed','returning','returned','exception')),
    total_e4 bigint NOT NULL CHECK (total_e4 >= 0),
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ux_trade_merchant_order_parent_merchant UNIQUE (buyer_order_id, merchant_id)
);
CREATE INDEX ix_trade_buyer_order_buyer_created ON trade.buyer_order (buyer_account_id, created_at DESC);
CREATE INDEX ix_trade_merchant_order_merchant_status ON trade.merchant_order (merchant_id, status);

CREATE TABLE trade.order_line (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    merchant_order_id bigint NOT NULL REFERENCES trade.merchant_order(id),
    sku_public_id text NOT NULL CHECK (sku_public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    quantity bigint NOT NULL CHECK (quantity > 0),
    unit_price_e4 bigint NOT NULL CHECK (unit_price_e4 >= 0),
    line_total_e4 bigint NOT NULL CHECK (line_total_e4 >= 0 AND line_total_e4 = quantity * unit_price_e4),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX ix_trade_order_line_merchant_order ON trade.order_line (merchant_order_id, id);

CREATE TABLE trade.order_event (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    merchant_order_id bigint NOT NULL REFERENCES trade.merchant_order(id),
    from_status text NOT NULL CHECK (from_status IN ('awaiting_payment','paid','submitted','accepted','in_fulfillment','shipped','delivered_verified','settled','rejected','cancelled','delivery_failed','returning','returned','exception')),
    to_status text NOT NULL CHECK (to_status IN ('awaiting_payment','paid','submitted','accepted','in_fulfillment','shipped','delivered_verified','settled','rejected','cancelled','delivery_failed','returning','returned','exception')),
    source_type text NOT NULL CHECK (source_type IN ('kess_result','routefit_event','system')),
    source_ref text NOT NULL CHECK (length(btrim(source_ref)) > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ux_trade_order_event_source UNIQUE (merchant_order_id, source_type, source_ref)
);

CREATE OR REPLACE FUNCTION trade.reject_order_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'trade.order_event is append-only' USING ERRCODE = '55000';
END;
$$;
CREATE TRIGGER trg_trade_order_event_immutable BEFORE UPDATE OR DELETE ON trade.order_event
    FOR EACH ROW EXECUTE FUNCTION trade.reject_order_event_mutation();

REVOKE UPDATE, DELETE, TRUNCATE ON trade.order_event FROM trade_rw;
REVOKE UPDATE, DELETE, TRUNCATE ON trade.order_line FROM trade_rw;

-- +goose Down
DROP TRIGGER trg_trade_order_event_immutable ON trade.order_event;
DROP FUNCTION trade.reject_order_event_mutation();
DROP TABLE trade.order_event;
DROP TABLE trade.order_line;
DROP TABLE trade.merchant_order;
DROP TABLE trade.buyer_order;
