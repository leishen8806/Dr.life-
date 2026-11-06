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

CREATE TABLE trade.order_event_source (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    buyer_order_id bigint NOT NULL REFERENCES trade.buyer_order(id),
    source_type text NOT NULL CHECK (source_type IN ('kess_result','routefit_event','system')),
    source_ref text NOT NULL CHECK (length(btrim(source_ref)) > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ux_trade_order_event_source_ref UNIQUE (source_type, source_ref),
    CONSTRAINT ux_trade_order_event_source_owner UNIQUE (buyer_order_id, source_type, source_ref)
);

CREATE TABLE trade.order_event (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    merchant_order_id bigint NOT NULL REFERENCES trade.merchant_order(id),
    buyer_order_id bigint NOT NULL,
    from_status text NOT NULL CHECK (from_status IN ('awaiting_payment','paid','submitted','accepted','in_fulfillment','shipped','delivered_verified','settled','rejected','cancelled','delivery_failed','returning','returned','exception')),
    to_status text NOT NULL CHECK (to_status IN ('awaiting_payment','paid','submitted','accepted','in_fulfillment','shipped','delivered_verified','settled','rejected','cancelled','delivery_failed','returning','returned','exception')),
    source_type text NOT NULL CHECK (source_type IN ('kess_result','routefit_event','system')),
    source_ref text NOT NULL CHECK (length(btrim(source_ref)) > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT fk_trade_order_event_source FOREIGN KEY (buyer_order_id, source_type, source_ref)
        REFERENCES trade.order_event_source(buyer_order_id, source_type, source_ref),
    CONSTRAINT ux_trade_order_event_source UNIQUE (merchant_order_id, source_type, source_ref)
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trade.reject_order_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'trade.order_event is append-only' USING ERRCODE = '55000';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_trade_order_event_immutable BEFORE UPDATE OR DELETE ON trade.order_event
    FOR EACH ROW EXECUTE FUNCTION trade.reject_order_event_mutation();
CREATE TRIGGER trg_trade_order_event_no_truncate BEFORE TRUNCATE ON trade.order_event
    FOR EACH STATEMENT EXECUTE FUNCTION trade.reject_order_event_mutation();

-- +goose StatementBegin
CREATE FUNCTION trade.assert_merchant_order_total() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    target_id bigint;
    target_ids bigint[];
    expected_e4 bigint;
    actual_e4 bigint;
BEGIN
    IF TG_TABLE_NAME = 'merchant_order' THEN
        IF TG_OP = 'INSERT' THEN target_ids := ARRAY[NEW.id];
        ELSIF TG_OP = 'DELETE' THEN target_ids := ARRAY[OLD.id];
        ELSE target_ids := ARRAY[OLD.id, NEW.id];
        END IF;
    ELSE
        IF TG_OP = 'INSERT' THEN target_ids := ARRAY[NEW.merchant_order_id];
        ELSIF TG_OP = 'DELETE' THEN target_ids := ARRAY[OLD.merchant_order_id];
        ELSE target_ids := ARRAY[OLD.merchant_order_id, NEW.merchant_order_id];
        END IF;
    END IF;
    FOR target_id IN SELECT DISTINCT unnest(target_ids) LOOP
        SELECT total_e4 INTO expected_e4 FROM trade.merchant_order WHERE id = target_id;
        IF FOUND THEN
            SELECT COALESCE(sum(line_total_e4), 0) INTO actual_e4 FROM trade.order_line WHERE merchant_order_id = target_id;
            IF expected_e4 <> actual_e4 THEN
                RAISE EXCEPTION 'merchant order total does not equal line total' USING ERRCODE = '23514';
            END IF;
        END IF;
    END LOOP;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION trade.assert_buyer_order_total() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    target_id bigint;
    target_ids bigint[];
    expected_e4 bigint;
    actual_e4 bigint;
BEGIN
    IF TG_TABLE_NAME = 'buyer_order' THEN
        IF TG_OP = 'INSERT' THEN target_ids := ARRAY[NEW.id];
        ELSIF TG_OP = 'DELETE' THEN target_ids := ARRAY[OLD.id];
        ELSE target_ids := ARRAY[OLD.id, NEW.id];
        END IF;
    ELSE
        IF TG_OP = 'INSERT' THEN target_ids := ARRAY[NEW.buyer_order_id];
        ELSIF TG_OP = 'DELETE' THEN target_ids := ARRAY[OLD.buyer_order_id];
        ELSE target_ids := ARRAY[OLD.buyer_order_id, NEW.buyer_order_id];
        END IF;
    END IF;
    FOR target_id IN SELECT DISTINCT unnest(target_ids) LOOP
        SELECT total_e4 INTO expected_e4 FROM trade.buyer_order WHERE id = target_id;
        IF FOUND THEN
            SELECT COALESCE(sum(total_e4), 0) INTO actual_e4 FROM trade.merchant_order WHERE buyer_order_id = target_id;
            IF expected_e4 <> actual_e4 THEN
                RAISE EXCEPTION 'buyer order total does not equal merchant order total' USING ERRCODE = '23514';
            END IF;
        END IF;
    END LOOP;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER trg_trade_merchant_order_total
    AFTER INSERT OR UPDATE OR DELETE ON trade.merchant_order
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION trade.assert_merchant_order_total();
CREATE CONSTRAINT TRIGGER trg_trade_order_line_total
    AFTER INSERT OR UPDATE OR DELETE ON trade.order_line
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION trade.assert_merchant_order_total();
CREATE CONSTRAINT TRIGGER trg_trade_buyer_order_total
    AFTER INSERT OR UPDATE OR DELETE ON trade.buyer_order
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION trade.assert_buyer_order_total();
CREATE CONSTRAINT TRIGGER trg_trade_merchant_order_parent_total
    AFTER INSERT OR UPDATE OR DELETE ON trade.merchant_order
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION trade.assert_buyer_order_total();

REVOKE ALL ON trade.order_event, trade.order_event_source FROM trade_rw, report_ro;
GRANT SELECT, INSERT ON trade.order_event, trade.order_event_source TO trade_rw;
GRANT USAGE, SELECT ON SEQUENCE trade.order_event_id_seq, trade.order_event_source_id_seq TO trade_rw;
GRANT SELECT ON trade.order_event, trade.order_event_source TO report_ro;
REVOKE UPDATE, DELETE, TRUNCATE ON trade.order_line FROM trade_rw;
REVOKE EXECUTE ON FUNCTION trade.reject_order_event_mutation(), trade.assert_merchant_order_total(), trade.assert_buyer_order_total() FROM PUBLIC;

-- +goose Down
DROP TRIGGER trg_trade_merchant_order_parent_total ON trade.merchant_order;
DROP TRIGGER trg_trade_buyer_order_total ON trade.buyer_order;
DROP TRIGGER trg_trade_order_line_total ON trade.order_line;
DROP TRIGGER trg_trade_merchant_order_total ON trade.merchant_order;
DROP FUNCTION trade.assert_buyer_order_total();
DROP FUNCTION trade.assert_merchant_order_total();
DROP TRIGGER trg_trade_order_event_no_truncate ON trade.order_event;
DROP TRIGGER trg_trade_order_event_immutable ON trade.order_event;
DROP FUNCTION trade.reject_order_event_mutation();
DROP TABLE trade.order_event;
DROP TABLE trade.order_event_source;
DROP TABLE trade.order_line;
DROP TABLE trade.merchant_order;
DROP TABLE trade.buyer_order;
