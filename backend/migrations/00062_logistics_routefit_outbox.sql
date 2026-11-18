-- +goose Up
CREATE TABLE logistics.fulfillment_outbox (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_order_id text NOT NULL CHECK (length(btrim(external_order_id)) > 0),
    idempotency_key text NOT NULL UNIQUE CHECK (length(btrim(idempotency_key)) BETWEEN 1 AND 128),
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'retry', 'processing', 'delivered', 'exception')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error_code text,
    next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    fulfillment_id text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX ix_logistics_fulfillment_outbox_pending
    ON logistics.fulfillment_outbox (next_attempt_at, id)
    WHERE status IN ('pending', 'retry');
GRANT SELECT, INSERT, UPDATE ON logistics.fulfillment_outbox TO logistics_rw;
GRANT USAGE, SELECT ON SEQUENCE logistics.fulfillment_outbox_id_seq TO logistics_rw;

-- +goose Down
REVOKE USAGE, SELECT ON SEQUENCE logistics.fulfillment_outbox_id_seq FROM logistics_rw;
REVOKE SELECT, INSERT, UPDATE ON logistics.fulfillment_outbox FROM logistics_rw;
DROP INDEX logistics.ix_logistics_fulfillment_outbox_pending;
DROP TABLE logistics.fulfillment_outbox;
