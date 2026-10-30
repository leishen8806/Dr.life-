-- +goose Up
-- P-01 KESS payment intent. This records a server-side receivable snapshot only;
-- it does not create any ledger transaction, balance, or settlement entitlement.
CREATE TABLE trade.kess_payment_intent (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    company_id bigint NOT NULL,
    channel text NOT NULL CHECK (channel = 'kess'),
    trade_no text NOT NULL CHECK (trade_no ~ '^DRL-[0-9A-HJKMNP-TV-Z]{26}$'),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency text NOT NULL CHECK (currency IN ('USD', 'KHR')),
    seller_code text NOT NULL CHECK (btrim(seller_code) <> ''),
    status text NOT NULL CHECK (status IN ('PENDING', 'WAITING', 'SUCCESS', 'FAILED', 'CLOSED', 'UNKNOWN', 'REFUND')),
    channel_status text,
    channel_transaction_id text,
    payment_url text,
    create_attempts integer NOT NULL DEFAULT 0 CHECK (create_attempts >= 0),
    last_checked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ux_kess_payment_intent_company_channel_trade UNIQUE (company_id, channel, trade_no)
);
CREATE INDEX ix_kess_payment_intent_recovery ON trade.kess_payment_intent (status, updated_at) WHERE status = 'UNKNOWN';

-- Intent identity and receivable facts are immutable after persistence. The
-- worker may only record channel facts and state observations.
-- +goose StatementBegin
CREATE FUNCTION trade.guard_kess_payment_intent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.public_id <> OLD.public_id OR NEW.company_id <> OLD.company_id OR NEW.channel <> OLD.channel
       OR NEW.trade_no <> OLD.trade_no OR NEW.amount_minor <> OLD.amount_minor OR NEW.currency <> OLD.currency
       OR NEW.seller_code <> OLD.seller_code OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'kess payment intent receivable facts are immutable' USING ERRCODE = 'P0001';
    END IF;
    IF NOT ((OLD.status = 'PENDING' AND NEW.status IN ('PENDING', 'WAITING', 'UNKNOWN'))
        OR (OLD.status IN ('WAITING', 'UNKNOWN') AND NEW.status IN ('WAITING', 'SUCCESS', 'FAILED', 'CLOSED', 'UNKNOWN', 'REFUND'))
        OR (OLD.status = 'SUCCESS' AND NEW.status IN ('SUCCESS', 'REFUND'))
        OR (OLD.status IN ('FAILED', 'CLOSED', 'REFUND') AND NEW.status = OLD.status)) THEN
        RAISE EXCEPTION 'invalid kess payment intent status transition' USING ERRCODE = 'P0001';
    END IF;
    NEW.updated_at := now();
    RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_guard_kess_payment_intent
BEFORE UPDATE ON trade.kess_payment_intent
FOR EACH ROW EXECUTE FUNCTION trade.guard_kess_payment_intent();

REVOKE ALL ON trade.kess_payment_intent FROM trade_rw, report_ro;
GRANT SELECT, INSERT ON trade.kess_payment_intent TO trade_rw;
GRANT UPDATE (status, channel_status, channel_transaction_id, payment_url, create_attempts, last_checked_at) ON trade.kess_payment_intent TO trade_rw;
GRANT USAGE, SELECT ON SEQUENCE trade.kess_payment_intent_id_seq TO trade_rw;
GRANT SELECT (id, public_id, company_id, channel, trade_no, amount_minor, currency, status, channel_status, channel_transaction_id, created_at, updated_at, last_checked_at) ON trade.kess_payment_intent TO report_ro;

-- +goose Down
DROP TABLE trade.kess_payment_intent;
DROP FUNCTION trade.guard_kess_payment_intent();
