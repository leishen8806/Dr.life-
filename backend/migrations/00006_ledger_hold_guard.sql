-- +goose Up
ALTER TABLE ledger.hold
    ADD COLUMN dispute_reason text,
    ADD COLUMN disputed_by bigint,
    ADD COLUMN disputed_at timestamptz,
    ADD COLUMN resolved_by bigint;

ALTER TABLE ledger.hold ADD CONSTRAINT ck_hold_state_metadata CHECK (
    (status = 'held' AND close_txn_id IS NULL AND closed_at IS NULL
        AND dispute_reason IS NULL AND disputed_by IS NULL AND disputed_at IS NULL AND resolved_by IS NULL)
 OR (status = 'disputed' AND close_txn_id IS NULL AND closed_at IS NULL
        AND dispute_reason IS NOT NULL AND length(btrim(dispute_reason)) > 0
        AND disputed_by IS NOT NULL AND disputed_by > 0 AND disputed_at IS NOT NULL AND resolved_by IS NULL)
 OR (status IN ('released', 'refunded') AND close_txn_id IS NOT NULL AND closed_at IS NOT NULL
        AND ((dispute_reason IS NULL AND disputed_by IS NULL AND disputed_at IS NULL AND resolved_by IS NULL)
          OR (dispute_reason IS NOT NULL AND length(btrim(dispute_reason)) > 0
            AND disputed_by IS NOT NULL AND disputed_by > 0 AND disputed_at IS NOT NULL
            AND resolved_by IS NOT NULL AND resolved_by > 0)))
);

-- +goose StatementBegin
CREATE FUNCTION ledger.guard_hold_insert() RETURNS trigger AS $$
DECLARE
    account_kind text;
    freeze_txn_type text;
BEGIN
    IF NEW.status IS DISTINCT FROM 'held'
       OR NEW.close_txn_id IS NOT NULL OR NEW.closed_at IS NOT NULL
       OR NEW.dispute_reason IS NOT NULL OR NEW.disputed_by IS NOT NULL
       OR NEW.disputed_at IS NOT NULL OR NEW.resolved_by IS NOT NULL THEN
        RAISE EXCEPTION 'new hold must start in held state without close or dispute metadata' USING ERRCODE = '55000';
    END IF;
    SELECT kind INTO account_kind FROM ledger.account WHERE id=NEW.account_id;
    IF account_kind IS DISTINCT FROM 'WALLET_FROZEN' THEN
        RAISE EXCEPTION 'hold account must be WALLET_FROZEN' USING ERRCODE = '55000';
    END IF;
    SELECT type INTO freeze_txn_type FROM ledger.txn WHERE id=NEW.freeze_txn_id;
    IF freeze_txn_type IS DISTINCT FROM 'freeze' THEN
        RAISE EXCEPTION 'hold freeze transaction must have type freeze' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_hold_insert_guard BEFORE INSERT ON ledger.hold
    FOR EACH ROW EXECUTE FUNCTION ledger.guard_hold_insert();

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_hold() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.id IS DISTINCT FROM OLD.id
           OR NEW.account_id IS DISTINCT FROM OLD.account_id
           OR NEW.ref_type IS DISTINCT FROM OLD.ref_type
           OR NEW.ref_id IS DISTINCT FROM OLD.ref_id
           OR NEW.amount IS DISTINCT FROM OLD.amount
           OR NEW.freeze_txn_id IS DISTINCT FROM OLD.freeze_txn_id
           OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
            RAISE EXCEPTION 'hold identity and amount are immutable' USING ERRCODE = '55000';
        END IF;
        IF NOT ((OLD.status = 'held' AND NEW.status IN ('released','refunded','disputed'))
             OR (OLD.status = 'disputed' AND NEW.status IN ('released','refunded'))) THEN
            RAISE EXCEPTION 'invalid hold status transition from % to %', OLD.status, NEW.status USING ERRCODE = '55000';
        END IF;
        IF NEW.status = 'disputed' THEN
            IF NEW.close_txn_id IS NOT NULL OR NEW.closed_at IS NOT NULL OR NEW.resolved_by IS NOT NULL
               OR NEW.dispute_reason IS NULL OR length(btrim(NEW.dispute_reason)) = 0
               OR NEW.disputed_by IS NULL OR NEW.disputed_by <= 0 OR NEW.disputed_at IS NULL THEN
                RAISE EXCEPTION 'disputed hold metadata is invalid' USING ERRCODE = '55000';
            END IF;
        ELSIF NEW.status IN ('released','refunded') THEN
            IF NEW.close_txn_id IS NULL OR NEW.closed_at IS NULL THEN
                RAISE EXCEPTION 'terminal hold requires close transaction and time' USING ERRCODE = '55000';
            END IF;
            IF OLD.status = 'disputed' THEN
                IF NEW.dispute_reason IS DISTINCT FROM OLD.dispute_reason
                   OR NEW.disputed_by IS DISTINCT FROM OLD.disputed_by
                   OR NEW.disputed_at IS DISTINCT FROM OLD.disputed_at
                   OR NEW.resolved_by IS NULL OR NEW.resolved_by <= 0 THEN
                    RAISE EXCEPTION 'dispute metadata is immutable and resolution actor is required' USING ERRCODE = '55000';
                END IF;
            ELSIF NEW.dispute_reason IS NOT NULL OR NEW.disputed_by IS NOT NULL
               OR NEW.disputed_at IS NOT NULL OR NEW.resolved_by IS NOT NULL THEN
                RAISE EXCEPTION 'non-disputed hold cannot acquire dispute metadata' USING ERRCODE = '55000';
            END IF;
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'holds cannot be deleted or truncated' USING ERRCODE = '55000';
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_hold_guard BEFORE UPDATE OR DELETE ON ledger.hold
    FOR EACH ROW EXECUTE FUNCTION ledger.protect_hold();
CREATE TRIGGER trg_hold_no_truncate BEFORE TRUNCATE ON ledger.hold
    FOR EACH STATEMENT EXECUTE FUNCTION ledger.protect_hold();

REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON ledger.hold FROM ledger_rw;
GRANT SELECT, INSERT ON ledger.hold TO ledger_rw;
GRANT UPDATE (status, close_txn_id, closed_at, dispute_reason, disputed_by, disputed_at, resolved_by) ON ledger.hold TO ledger_rw;

-- +goose Down
DROP TRIGGER IF EXISTS trg_hold_insert_guard ON ledger.hold;
DROP FUNCTION IF EXISTS ledger.guard_hold_insert();
DROP TRIGGER IF EXISTS trg_hold_no_truncate ON ledger.hold;
DROP TRIGGER IF EXISTS trg_hold_guard ON ledger.hold;
DROP FUNCTION IF EXISTS ledger.protect_hold();
ALTER TABLE ledger.hold DROP CONSTRAINT IF EXISTS ck_hold_state_metadata;
ALTER TABLE ledger.hold
    DROP COLUMN IF EXISTS resolved_by,
    DROP COLUMN IF EXISTS disputed_at,
    DROP COLUMN IF EXISTS disputed_by,
    DROP COLUMN IF EXISTS dispute_reason;
GRANT SELECT, INSERT, UPDATE ON ledger.hold TO ledger_rw;
REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER ON ledger.hold FROM ledger_rw;
