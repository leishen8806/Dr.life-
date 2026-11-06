-- +goose Up
-- P-01 KESS payment intent. This records a server-side receivable snapshot only;
-- it does not create any ledger transaction, balance, or settlement entitlement.
CREATE TABLE trade.kess_payment_intent (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    company_id bigint NOT NULL,
    channel text NOT NULL CHECK (channel = 'kess'),
    trade_no text NOT NULL UNIQUE CHECK (trade_no ~ '^DRL-[0-9A-HJKMNP-TV-Z]{26}-[1-9][0-9]*$'),
    source_ref text NOT NULL CHECK (source_ref ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    attempt_no integer NOT NULL CHECK (attempt_no > 0),
    amount_e4 bigint NOT NULL CHECK (amount_e4 > 0 AND amount_e4 % 100 = 0),
    currency text NOT NULL CHECK (currency = 'USD'),
    seller_code text NOT NULL CHECK (btrim(seller_code) <> ''),
    status text NOT NULL CHECK (status IN ('PENDING', 'WAITING', 'SUCCESS', 'FAILED', 'CLOSED', 'UNKNOWN', 'REFUND')),
    channel_status text,
    channel_transaction_id text,
    payment_url text,
    create_attempts integer NOT NULL DEFAULT 0 CHECK (create_attempts >= 0),
    create_claimed_at timestamptz,
    create_sent_at timestamptz,
    last_checked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ux_kess_payment_intent_company_source_attempt UNIQUE (company_id, source_ref, attempt_no)
);
CREATE INDEX ix_kess_payment_intent_recovery ON trade.kess_payment_intent (status, updated_at) WHERE status IN ('UNKNOWN', 'WAITING');

-- Intent identity and receivable facts are immutable after persistence. The
-- worker may only record channel facts and state observations.
-- +goose StatementBegin
CREATE FUNCTION trade.guard_kess_payment_intent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'PENDING' OR NEW.create_attempts <> 0 OR NEW.channel_status IS NOT NULL
           OR NEW.channel_transaction_id IS NOT NULL OR NEW.payment_url IS NOT NULL OR NEW.create_claimed_at IS NOT NULL
           OR NEW.create_sent_at IS NOT NULL OR NEW.last_checked_at IS NOT NULL THEN
            RAISE EXCEPTION 'invalid initial kess payment intent state' USING ERRCODE = 'P0001';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.public_id <> OLD.public_id OR NEW.company_id <> OLD.company_id OR NEW.channel <> OLD.channel
       OR NEW.trade_no <> OLD.trade_no OR NEW.source_ref <> OLD.source_ref OR NEW.attempt_no <> OLD.attempt_no
       OR NEW.amount_e4 <> OLD.amount_e4 OR NEW.currency <> OLD.currency
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
BEFORE INSERT OR UPDATE ON trade.kess_payment_intent
FOR EACH ROW EXECUTE FUNCTION trade.guard_kess_payment_intent();

REVOKE ALL ON trade.kess_payment_intent FROM trade_rw, report_ro;
GRANT SELECT, INSERT ON trade.kess_payment_intent TO trade_rw;
GRANT UPDATE (status, channel_status, channel_transaction_id, payment_url, create_attempts, create_claimed_at, create_sent_at, last_checked_at) ON trade.kess_payment_intent TO trade_rw;
REVOKE ALL ON SEQUENCE trade.kess_payment_intent_id_seq FROM trade_rw, report_ro, PUBLIC;
GRANT USAGE, SELECT ON SEQUENCE trade.kess_payment_intent_id_seq TO trade_rw;
GRANT SELECT (id, public_id, company_id, channel, trade_no, source_ref, attempt_no, amount_e4, currency, status, channel_status, channel_transaction_id, created_at, updated_at, last_checked_at) ON trade.kess_payment_intent TO report_ro;

-- +goose Down
DROP TABLE trade.kess_payment_intent;
DROP FUNCTION trade.guard_kess_payment_intent();
