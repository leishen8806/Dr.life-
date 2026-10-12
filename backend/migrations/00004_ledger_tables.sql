-- +goose Up
CREATE TABLE ledger.account (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN (
        'BANK_DEDICATED', 'WALLET_AVAIL', 'WALLET_FROZEN', 'PENDING_PAYOUT',
        'DELIVERY_ESCROW', 'AD_BUDGET', 'PLATFORM_HELD', 'PAYABLE_CARRIER', 'SUSPENSE'
    )),
    normal_side text NOT NULL CHECK (normal_side IN ('DEBIT', 'CREDIT')),
    owner_account bigint,
    merchant_id bigint,
    currency text NOT NULL DEFAULT 'USD' CHECK (currency = 'USD'),
    allow_negative boolean NOT NULL DEFAULT false,
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_account_normal_side CHECK (
        (kind = 'BANK_DEDICATED' AND normal_side = 'DEBIT') OR
        (kind <> 'BANK_DEDICATED' AND normal_side = 'CREDIT')
    ),
    CONSTRAINT ck_account_owner CHECK (
        (kind IN ('BANK_DEDICATED', 'PENDING_PAYOUT', 'DELIVERY_ESCROW', 'PLATFORM_HELD', 'PAYABLE_CARRIER', 'SUSPENSE') AND owner_account IS NULL) OR
        (kind IN ('WALLET_AVAIL', 'WALLET_FROZEN', 'AD_BUDGET') AND owner_account IS NOT NULL)
    ),
    CONSTRAINT ck_account_negative_policy CHECK (NOT allow_negative OR kind IN ('PAYABLE_CARRIER', 'SUSPENSE')),
    CONSTRAINT uq_account_kind_owner UNIQUE NULLS NOT DISTINCT (kind, owner_account)
);
COMMENT ON COLUMN ledger.account.owner_account IS 'iam.account.id; no cross-schema foreign key so the ledger module remains isolated';

CREATE TABLE ledger.txn (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    txn_no text NOT NULL UNIQUE,
    type text NOT NULL CHECK (type IN (
        'topup', 'freeze', 'settle', 'refund', 'withdraw_req', 'payout', 'payout_fail',
        'storage_fee', 'delivery_hold', 'delivery_release', 'delivery_refund', 'ad_charge',
        'ad_spend', 'ad_refund', 'member_fee', 'sweep', 'carrier_pay', 'reversal', 'adjust', 'bank_diff'
    )),
    amount bigint NOT NULL CHECK (amount > 0),
    idem_key text NOT NULL UNIQUE,
    ref_type text,
    ref_id bigint,
    note text,
    reverses_txn_id bigint REFERENCES ledger.txn(id),
    actor_id bigint NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_txn_reversal_reference CHECK (
        (type = 'reversal' AND reverses_txn_id IS NOT NULL) OR
        (type <> 'reversal' AND reverses_txn_id IS NULL)
    ),
    CONSTRAINT ck_txn_not_self_reversal CHECK (reverses_txn_id IS NULL OR reverses_txn_id <> id)
);
COMMENT ON COLUMN ledger.txn.actor_id IS 'iam.actor.id; no cross-schema foreign key so the ledger module remains isolated';
CREATE UNIQUE INDEX uq_txn_reverses_once ON ledger.txn(reverses_txn_id) WHERE reverses_txn_id IS NOT NULL;

CREATE TABLE ledger.posting (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    txn_id bigint NOT NULL REFERENCES ledger.txn(id),
    account_id bigint NOT NULL REFERENCES ledger.account(id),
    side text NOT NULL CHECK (side IN ('DEBIT', 'CREDIT')),
    amount bigint NOT NULL CHECK (amount > 0),
    role text,
    category text CHECK (category IN ('storage', 'delivery_margin', 'ads', 'settle_fee', 'member')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ix_posting_account ON ledger.posting(account_id, id);
CREATE INDEX ix_posting_txn ON ledger.posting(txn_id);

CREATE TABLE ledger.balance (
    account_id bigint PRIMARY KEY REFERENCES ledger.account(id),
    balance bigint NOT NULL DEFAULT 0,
    allow_negative boolean NOT NULL,
    version bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_balance_nonnegative CHECK (allow_negative OR balance >= 0)
);

CREATE TABLE ledger.hold (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id bigint NOT NULL REFERENCES ledger.account(id),
    ref_type text NOT NULL,
    ref_id bigint NOT NULL,
    amount bigint NOT NULL CHECK (amount > 0),
    status text NOT NULL CHECK (status IN ('held', 'released', 'refunded', 'disputed')),
    freeze_txn_id bigint NOT NULL REFERENCES ledger.txn(id),
    close_txn_id bigint REFERENCES ledger.txn(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    closed_at timestamptz,
    UNIQUE (ref_type, ref_id)
);

CREATE TABLE ledger.withdrawal (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE,
    account_id bigint NOT NULL,
    amount bigint NOT NULL CHECK (amount > 0),
    payee_account_text text NOT NULL,
    payee_account_last4 text,
    status text NOT NULL CHECK (status IN ('requested', 'review', 'approved', 'paid', 'failed', 'cancelled')),
    maker_actor bigint NOT NULL,
    checker_actor bigint,
    req_txn_id bigint REFERENCES ledger.txn(id),
    paid_txn_id bigint REFERENCES ledger.txn(id),
    bank_ref text,
    requested_at timestamptz NOT NULL DEFAULT now(),
    paid_at timestamptz,
    CONSTRAINT ck_withdrawal_separation CHECK (checker_actor IS NULL OR checker_actor <> maker_actor)
);
COMMENT ON COLUMN ledger.withdrawal.payee_account_text IS 'Application-encrypted payee account ciphertext';
CREATE INDEX ix_wd_status ON ledger.withdrawal(status, requested_at);

CREATE TABLE ledger.outbox (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    txn_id bigint,
    topic text,
    payload jsonb,
    created_at timestamptz DEFAULT now(),
    sent_at timestamptz
);

CREATE TABLE ledger.bank_statement_line (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    bank_ref text NOT NULL,
    booked_on date NOT NULL,
    amount bigint NOT NULL,
    direction text NOT NULL CHECK (direction IN ('IN', 'OUT')),
    counterparty text,
    raw jsonb,
    matched_txn_id bigint REFERENCES ledger.txn(id),
    match_status text NOT NULL DEFAULT 'unmatched' CHECK (match_status IN ('unmatched', 'matched', 'ignored', 'suspense')),
    imported_batch text NOT NULL,
    UNIQUE (bank_ref, booked_on, amount, direction)
);

CREATE TABLE ledger.snapshot_daily (
    day date NOT NULL,
    account_id bigint NOT NULL,
    balance bigint NOT NULL,
    PRIMARY KEY (day, account_id)
);

-- +goose StatementBegin
CREATE FUNCTION ledger.create_balance() RETURNS trigger AS $$
BEGIN
    INSERT INTO ledger.balance (account_id, allow_negative) VALUES (NEW.id, NEW.allow_negative);
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER trg_account_balance AFTER INSERT ON ledger.account
    FOR EACH ROW EXECUTE FUNCTION ledger.create_balance();

-- +goose StatementBegin
CREATE FUNCTION ledger.validate_balance_policy() RETURNS trigger AS $$
DECLARE
    account_allows_negative boolean;
BEGIN
    SELECT allow_negative INTO account_allows_negative FROM ledger.account WHERE id = NEW.account_id;
    IF NEW.allow_negative IS DISTINCT FROM account_allows_negative THEN
        RAISE EXCEPTION 'balance negative policy must match its account' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER trg_balance_policy BEFORE INSERT OR UPDATE ON ledger.balance
    FOR EACH ROW EXECUTE FUNCTION ledger.validate_balance_policy();

-- +goose StatementBegin
CREATE FUNCTION ledger.check_txn_balanced() RETURNS trigger AS $$
DECLARE
    checked_txn_id bigint;
    posting_count bigint;
    debit_total bigint;
    credit_total bigint;
    txn_amount bigint;
BEGIN
    checked_txn_id := CASE
        WHEN TG_TABLE_NAME = 'txn' THEN (to_jsonb(NEW)->>'id')::bigint
        ELSE (to_jsonb(NEW)->>'txn_id')::bigint
    END;
    SELECT count(*),
           coalesce(sum(amount) FILTER (WHERE side = 'DEBIT'), 0),
           coalesce(sum(amount) FILTER (WHERE side = 'CREDIT'), 0)
      INTO posting_count, debit_total, credit_total
      FROM ledger.posting
     WHERE txn_id = checked_txn_id;
    SELECT amount INTO txn_amount FROM ledger.txn WHERE id = checked_txn_id;

    IF posting_count < 2 OR debit_total <> credit_total OR debit_total <> txn_amount THEN
        RAISE EXCEPTION 'txn % is not balanced (postings %, debit %, credit %, amount %)',
            checked_txn_id, posting_count, debit_total, credit_total, txn_amount
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER trg_txn_balanced
    AFTER INSERT ON ledger.txn DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ledger.check_txn_balanced();
CREATE CONSTRAINT TRIGGER trg_posting_balanced
    AFTER INSERT ON ledger.posting DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ledger.check_txn_balanced();

-- +goose StatementBegin
CREATE FUNCTION ledger.validate_posting_category() RETURNS trigger AS $$
DECLARE
    account_kind text;
BEGIN
    IF NEW.category IS NOT NULL THEN
        SELECT kind INTO account_kind FROM ledger.account WHERE id = NEW.account_id;
        IF account_kind IS DISTINCT FROM 'PLATFORM_HELD' THEN
            RAISE EXCEPTION 'posting category is allowed only for PLATFORM_HELD accounts'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER trg_posting_category BEFORE INSERT OR UPDATE ON ledger.posting
    FOR EACH ROW EXECUTE FUNCTION ledger.validate_posting_category();

-- +goose StatementBegin
CREATE FUNCTION ledger.validate_reversal() RETURNS trigger AS $$
DECLARE
    original_amount bigint;
BEGIN
    IF NEW.reverses_txn_id IS NOT NULL THEN
        IF NEW.reverses_txn_id = NEW.id THEN
            RAISE EXCEPTION 'a transaction cannot reverse itself' USING ERRCODE = '23514';
        END IF;
        SELECT amount INTO original_amount FROM ledger.txn WHERE id = NEW.reverses_txn_id;
        IF original_amount IS NOT NULL AND original_amount <> NEW.amount THEN
            RAISE EXCEPTION 'reversal amount must equal original transaction amount' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER trg_txn_reversal BEFORE INSERT OR UPDATE ON ledger.txn
    FOR EACH ROW EXECUTE FUNCTION ledger.validate_reversal();

-- +goose StatementBegin
CREATE FUNCTION ledger.deny_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ledger rows are append-only' USING ERRCODE = '55000';
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER trg_txn_ro BEFORE UPDATE OR DELETE ON ledger.txn
    FOR EACH ROW EXECUTE FUNCTION ledger.deny_mutation();
CREATE TRIGGER trg_posting_ro BEFORE UPDATE OR DELETE ON ledger.posting
    FOR EACH ROW EXECUTE FUNCTION ledger.deny_mutation();
CREATE TRIGGER trg_txn_no_truncate BEFORE TRUNCATE ON ledger.txn
    FOR EACH STATEMENT EXECUTE FUNCTION ledger.deny_mutation();
CREATE TRIGGER trg_posting_no_truncate BEFORE TRUNCATE ON ledger.posting
    FOR EACH STATEMENT EXECUTE FUNCTION ledger.deny_mutation();
CREATE TRIGGER trg_balance_no_delete BEFORE DELETE ON ledger.balance
    FOR EACH ROW EXECUTE FUNCTION ledger.deny_mutation();
CREATE TRIGGER trg_balance_no_truncate BEFORE TRUNCATE ON ledger.balance
    FOR EACH STATEMENT EXECUTE FUNCTION ledger.deny_mutation();

REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON ledger.txn, ledger.posting FROM ledger_rw;
GRANT SELECT, INSERT ON ledger.txn, ledger.posting TO ledger_rw;
REVOKE DELETE, TRUNCATE, TRIGGER ON ledger.balance FROM ledger_rw;

INSERT INTO ledger.account (kind, normal_side, allow_negative) VALUES
    ('BANK_DEDICATED', 'DEBIT', false),
    ('PENDING_PAYOUT', 'CREDIT', false),
    ('DELIVERY_ESCROW', 'CREDIT', false),
    ('PLATFORM_HELD', 'CREDIT', false),
    ('PAYABLE_CARRIER', 'CREDIT', false),
    ('SUSPENSE', 'CREDIT', true)
ON CONFLICT ON CONSTRAINT uq_account_kind_owner DO NOTHING;

-- +goose Down
DROP TRIGGER IF EXISTS trg_balance_no_truncate ON ledger.balance;
DROP TRIGGER IF EXISTS trg_balance_no_delete ON ledger.balance;
DROP TRIGGER IF EXISTS trg_posting_no_truncate ON ledger.posting;
DROP TRIGGER IF EXISTS trg_txn_no_truncate ON ledger.txn;
DROP TRIGGER IF EXISTS trg_posting_ro ON ledger.posting;
DROP TRIGGER IF EXISTS trg_txn_ro ON ledger.txn;
DROP TRIGGER IF EXISTS trg_txn_reversal ON ledger.txn;
DROP TRIGGER IF EXISTS trg_posting_category ON ledger.posting;
DROP TRIGGER IF EXISTS trg_posting_balanced ON ledger.posting;
DROP TRIGGER IF EXISTS trg_txn_balanced ON ledger.txn;
DROP TRIGGER IF EXISTS trg_balance_policy ON ledger.balance;
DROP TRIGGER IF EXISTS trg_account_balance ON ledger.account;

DROP TABLE IF EXISTS ledger.snapshot_daily;
DROP TABLE IF EXISTS ledger.bank_statement_line;
DROP TABLE IF EXISTS ledger.outbox;
DROP TABLE IF EXISTS ledger.withdrawal;
DROP TABLE IF EXISTS ledger.hold;
DROP TABLE IF EXISTS ledger.balance;
DROP TABLE IF EXISTS ledger.posting;
DROP TABLE IF EXISTS ledger.txn;
DROP TABLE IF EXISTS ledger.account;

DROP FUNCTION IF EXISTS ledger.deny_mutation();
DROP FUNCTION IF EXISTS ledger.validate_reversal();
DROP FUNCTION IF EXISTS ledger.validate_posting_category();
DROP FUNCTION IF EXISTS ledger.check_txn_balanced();
DROP FUNCTION IF EXISTS ledger.validate_balance_policy();
DROP FUNCTION IF EXISTS ledger.create_balance();
