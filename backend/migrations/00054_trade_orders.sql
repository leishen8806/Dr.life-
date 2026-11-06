-- +goose Up
-- T-05a order-domain tables. Cross-module identities are opaque snapshots;
-- trade does not join or constrain iam/catalog schemas.
CREATE TABLE trade.buyer_order (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    buyer_account_id bigint NOT NULL,
    idempotency_key text NOT NULL CHECK (length(btrim(idempotency_key)) BETWEEN 1 AND 200),
    request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    total_e4 bigint NOT NULL CHECK (total_e4 >= 0),
    reservation_released_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT ux_trade_buyer_order_idempotency UNIQUE (buyer_account_id, idempotency_key)
);

CREATE TABLE trade.reservation_release_outbox (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    reservation_key text NOT NULL UNIQUE CHECK (length(btrim(reservation_key)) > 0),
    reason text NOT NULL CHECK (length(btrim(reason)) > 0),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
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
ALTER TABLE trade.merchant_order ADD CONSTRAINT ux_trade_merchant_order_id_parent UNIQUE (id, buyer_order_id);
CREATE INDEX ix_trade_buyer_order_buyer_created ON trade.buyer_order (buyer_account_id, created_at DESC);
CREATE INDEX ix_trade_merchant_order_merchant_status ON trade.merchant_order (merchant_id, status);

CREATE TABLE trade.order_line (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    merchant_order_id bigint NOT NULL REFERENCES trade.merchant_order(id),
    sku_public_id text NOT NULL CHECK (sku_public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    quantity bigint NOT NULL CHECK (quantity > 0),
    unit_price_e4 bigint NOT NULL CHECK (unit_price_e4 >= 0 AND unit_price_e4 % 100 = 0),
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
    merchant_order_id bigint NOT NULL,
    buyer_order_id bigint NOT NULL,
    from_status text NOT NULL CHECK (from_status IN ('awaiting_payment','paid','submitted','accepted','in_fulfillment','shipped','delivered_verified','settled','rejected','cancelled','delivery_failed','returning','returned','exception')),
    to_status text NOT NULL CHECK (to_status IN ('awaiting_payment','paid','submitted','accepted','in_fulfillment','shipped','delivered_verified','settled','rejected','cancelled','delivery_failed','returning','returned','exception')),
    source_type text NOT NULL CHECK (source_type IN ('kess_result','routefit_event','system')),
    source_ref text NOT NULL CHECK (length(btrim(source_ref)) > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT fk_trade_order_event_source FOREIGN KEY (buyer_order_id, source_type, source_ref)
        REFERENCES trade.order_event_source(buyer_order_id, source_type, source_ref),
    CONSTRAINT fk_trade_order_event_order FOREIGN KEY (merchant_order_id, buyer_order_id)
        REFERENCES trade.merchant_order(id, buyer_order_id),
    CONSTRAINT ux_trade_order_event_source UNIQUE (merchant_order_id, source_type, source_ref)
);

-- +goose StatementBegin
CREATE FUNCTION trade.guard_buyer_order_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.public_id <> OLD.public_id OR NEW.buyer_account_id <> OLD.buyer_account_id
       OR NEW.idempotency_key <> OLD.idempotency_key OR NEW.request_hash <> OLD.request_hash
       OR NEW.total_e4 <> OLD.total_e4 OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'trade.buyer_order business facts are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_trade_buyer_order_guard BEFORE UPDATE ON trade.buyer_order
    FOR EACH ROW EXECUTE FUNCTION trade.guard_buyer_order_update();

-- +goose StatementBegin
CREATE FUNCTION trade.guard_merchant_order_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.public_id <> OLD.public_id OR NEW.buyer_order_id <> OLD.buyer_order_id
       OR NEW.merchant_id <> OLD.merchant_id OR NEW.external_order_id <> OLD.external_order_id
       OR NEW.total_e4 <> OLD.total_e4 OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'trade.merchant_order business facts are immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.status <> OLD.status THEN
        IF NOT ((OLD.status = 'awaiting_payment' AND NEW.status IN ('paid','cancelled'))
            OR (OLD.status = 'paid' AND NEW.status = 'submitted')
            OR (OLD.status = 'submitted' AND NEW.status IN ('accepted','rejected'))
            OR (OLD.status = 'accepted' AND NEW.status = 'in_fulfillment')
            OR (OLD.status = 'in_fulfillment' AND NEW.status = 'shipped')
            OR (OLD.status = 'shipped' AND NEW.status = 'delivered_verified')
            OR (OLD.status = 'delivered_verified' AND NEW.status = 'settled')) THEN
            RAISE EXCEPTION 'invalid merchant order transition % -> %', OLD.status, NEW.status USING ERRCODE = '23514';
        END IF;
        IF NEW.version <> OLD.version + 1 OR NOT EXISTS (
            SELECT 1 FROM trade.order_event e
            WHERE e.merchant_order_id = OLD.id AND e.buyer_order_id = OLD.buyer_order_id
              AND e.from_status = OLD.status AND e.to_status = NEW.status
        ) THEN
            RAISE EXCEPTION 'merchant order transition requires matching event' USING ERRCODE = '55000';
        END IF;
    ELSIF NEW.version <> OLD.version THEN
        RAISE EXCEPTION 'merchant order version may change only with status' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_trade_merchant_order_guard BEFORE UPDATE ON trade.merchant_order
    FOR EACH ROW EXECUTE FUNCTION trade.guard_merchant_order_update();

-- +goose StatementBegin
CREATE FUNCTION trade.guard_order_line_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM trade.merchant_order WHERE id = NEW.merchant_order_id AND status = 'awaiting_payment') THEN
        RAISE EXCEPTION 'order lines may be inserted only while awaiting payment' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_trade_order_line_insert BEFORE INSERT ON trade.order_line
    FOR EACH ROW EXECUTE FUNCTION trade.guard_order_line_insert();

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
REVOKE ALL ON trade.buyer_order, trade.merchant_order, trade.order_line FROM trade_rw, report_ro;
REVOKE ALL ON trade.reservation_release_outbox FROM trade_rw, report_ro;
GRANT SELECT, INSERT ON trade.buyer_order, trade.merchant_order, trade.order_line TO trade_rw;
GRANT UPDATE (reservation_released_at, updated_at) ON trade.buyer_order TO trade_rw;
GRANT UPDATE (status, version, updated_at) ON trade.merchant_order TO trade_rw;
GRANT SELECT ON trade.buyer_order, trade.merchant_order, trade.order_line TO report_ro;
GRANT SELECT, INSERT, UPDATE (reason, attempts, updated_at), DELETE ON trade.reservation_release_outbox TO trade_rw;
GRANT SELECT, INSERT ON trade.order_event, trade.order_event_source TO trade_rw;
GRANT USAGE, SELECT ON SEQUENCE trade.buyer_order_id_seq, trade.merchant_order_id_seq, trade.order_line_id_seq,
    trade.order_event_id_seq, trade.order_event_source_id_seq, trade.reservation_release_outbox_id_seq TO trade_rw;
GRANT SELECT ON trade.order_event, trade.order_event_source TO report_ro;
REVOKE UPDATE, DELETE, TRUNCATE ON trade.order_line FROM trade_rw;
REVOKE DELETE, TRUNCATE ON trade.buyer_order, trade.merchant_order FROM trade_rw;
REVOKE UPDATE ON SEQUENCE trade.buyer_order_id_seq, trade.merchant_order_id_seq, trade.order_line_id_seq,
    trade.order_event_id_seq, trade.order_event_source_id_seq, trade.reservation_release_outbox_id_seq FROM trade_rw;
REVOKE EXECUTE ON FUNCTION trade.guard_buyer_order_update(), trade.guard_merchant_order_update(),
    trade.guard_order_line_insert(), trade.reject_order_event_mutation(), trade.assert_merchant_order_total(),
    trade.assert_buyer_order_total() FROM PUBLIC;

-- +goose Down
DROP TRIGGER trg_trade_merchant_order_parent_total ON trade.merchant_order;
DROP TRIGGER trg_trade_buyer_order_total ON trade.buyer_order;
DROP TRIGGER trg_trade_order_line_total ON trade.order_line;
DROP TRIGGER trg_trade_merchant_order_total ON trade.merchant_order;
DROP FUNCTION trade.assert_buyer_order_total();
DROP FUNCTION trade.assert_merchant_order_total();
DROP TRIGGER trg_trade_order_line_insert ON trade.order_line;
DROP FUNCTION trade.guard_order_line_insert();
DROP TRIGGER trg_trade_merchant_order_guard ON trade.merchant_order;
DROP FUNCTION trade.guard_merchant_order_update();
DROP TRIGGER trg_trade_buyer_order_guard ON trade.buyer_order;
DROP FUNCTION trade.guard_buyer_order_update();
DROP TRIGGER trg_trade_order_event_no_truncate ON trade.order_event;
DROP TRIGGER trg_trade_order_event_immutable ON trade.order_event;
DROP FUNCTION trade.reject_order_event_mutation();
DROP TABLE trade.order_event;
DROP TABLE trade.order_event_source;
DROP TABLE trade.order_line;
DROP TABLE trade.merchant_order;
DROP TABLE trade.reservation_release_outbox;
DROP TABLE trade.buyer_order;
