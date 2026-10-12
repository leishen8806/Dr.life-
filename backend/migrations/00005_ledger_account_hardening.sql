-- +goose Up
REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON ledger.account FROM ledger_rw;
GRANT SELECT, INSERT ON ledger.account TO ledger_rw;
GRANT UPDATE (status) ON ledger.account TO ledger_rw;

REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON ledger.balance FROM ledger_rw;
GRANT SELECT ON ledger.balance TO ledger_rw;
GRANT UPDATE (balance, version, updated_at) ON ledger.balance TO ledger_rw;

REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER ON
    ledger.hold,
    ledger.withdrawal,
    ledger.outbox,
    ledger.bank_statement_line,
    ledger.snapshot_daily
FROM ledger_rw;
REVOKE UPDATE ON ledger.outbox FROM ledger_rw;
GRANT UPDATE (sent_at) ON ledger.outbox TO ledger_rw;

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_account() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.id IS DISTINCT FROM OLD.id
           OR NEW.kind IS DISTINCT FROM OLD.kind
           OR NEW.normal_side IS DISTINCT FROM OLD.normal_side
           OR NEW.owner_account IS DISTINCT FROM OLD.owner_account
           OR NEW.merchant_id IS DISTINCT FROM OLD.merchant_id
           OR NEW.currency IS DISTINCT FROM OLD.currency
           OR NEW.allow_negative IS DISTINCT FROM OLD.allow_negative
           OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
            RAISE EXCEPTION 'ledger account identity and policy are immutable' USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'ledger accounts cannot be deleted or truncated' USING ERRCODE = '55000';
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_account_immutable BEFORE UPDATE OR DELETE ON ledger.account
    FOR EACH ROW EXECUTE FUNCTION ledger.protect_account();
CREATE TRIGGER trg_account_no_truncate BEFORE TRUNCATE ON ledger.account
    FOR EACH STATEMENT EXECUTE FUNCTION ledger.protect_account();

-- +goose Down
DROP TRIGGER IF EXISTS trg_account_no_truncate ON ledger.account;
DROP TRIGGER IF EXISTS trg_account_immutable ON ledger.account;
DROP FUNCTION IF EXISTS ledger.protect_account();

GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON
    ledger.account,
    ledger.balance,
    ledger.hold,
    ledger.withdrawal,
    ledger.outbox,
    ledger.bank_statement_line,
    ledger.snapshot_daily
TO ledger_rw;

REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON ledger.txn, ledger.posting FROM ledger_rw;
GRANT SELECT, INSERT ON ledger.txn, ledger.posting TO ledger_rw;
REVOKE DELETE, TRUNCATE, TRIGGER ON ledger.balance FROM ledger_rw;
