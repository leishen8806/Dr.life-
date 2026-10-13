-- +goose Up
ALTER TABLE ledger.withdrawal
    ALTER COLUMN maker_actor DROP NOT NULL,
    ADD COLUMN requester_actor bigint NOT NULL,
    ADD COLUMN requires_review boolean NOT NULL,
    ADD COLUMN reviewed_at timestamptz,
    ADD COLUMN failed_reason text;

ALTER TABLE ledger.withdrawal DROP CONSTRAINT ck_withdrawal_separation;
ALTER TABLE ledger.withdrawal ADD CONSTRAINT ck_withdrawal_separation CHECK (
    (maker_actor IS NULL OR maker_actor > 0)
    AND (checker_actor IS NULL OR checker_actor > 0)
    AND requester_actor > 0
    AND (checker_actor IS NULL OR maker_actor IS NOT NULL)
    AND (checker_actor IS NULL OR checker_actor <> maker_actor)
    AND (checker_actor IS NULL OR checker_actor <> requester_actor)
    AND (NOT (status IN ('approved','paid') AND requires_review)
         OR (maker_actor IS NOT NULL AND checker_actor IS NOT NULL))
);

CREATE UNIQUE INDEX uq_withdrawal_bank_ref ON ledger.withdrawal(bank_ref) WHERE bank_ref IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION ledger.guard_withdrawal_insert() RETURNS trigger AS $$
BEGIN
    IF NEW.public_id !~ '^[0-9A-HJKMNP-TV-Z]{26}$'
       OR NEW.requester_actor <= 0
       OR NEW.req_txn_id IS NOT NULL OR NEW.paid_txn_id IS NOT NULL
       OR NEW.bank_ref IS NOT NULL OR NEW.paid_at IS NOT NULL
       OR NEW.reviewed_at IS NOT NULL OR NEW.failed_reason IS NOT NULL
       OR NEW.maker_actor IS NOT NULL OR NEW.checker_actor IS NOT NULL
       OR NOT ((NEW.requires_review AND NEW.status = 'requested')
            OR (NOT NEW.requires_review AND NEW.status = 'approved')) THEN
        RAISE EXCEPTION 'invalid initial withdrawal state' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_withdrawal_insert_guard BEFORE INSERT ON ledger.withdrawal
    FOR EACH ROW EXECUTE FUNCTION ledger.guard_withdrawal_insert();

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_withdrawal() RETURNS trigger AS $$
DECLARE
    linked_type text;
    linked_amount bigint;
    linked_ref_type text;
    linked_ref_id bigint;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF OLD.status IN ('paid','failed','cancelled') THEN
            RAISE EXCEPTION 'terminal withdrawal is immutable' USING ERRCODE = '55000';
        END IF;
        IF NEW.id IS DISTINCT FROM OLD.id
           OR NEW.public_id IS DISTINCT FROM OLD.public_id
           OR NEW.account_id IS DISTINCT FROM OLD.account_id
           OR NEW.amount IS DISTINCT FROM OLD.amount
           OR NEW.payee_account_text IS DISTINCT FROM OLD.payee_account_text
           OR NEW.payee_account_last4 IS DISTINCT FROM OLD.payee_account_last4
           OR NEW.requester_actor IS DISTINCT FROM OLD.requester_actor
           OR NEW.requires_review IS DISTINCT FROM OLD.requires_review
           OR NEW.requested_at IS DISTINCT FROM OLD.requested_at THEN
            RAISE EXCEPTION 'withdrawal identity and request are immutable' USING ERRCODE = '55000';
        END IF;
        IF OLD.maker_actor IS NOT NULL AND NEW.maker_actor IS DISTINCT FROM OLD.maker_actor
           OR OLD.checker_actor IS NOT NULL AND NEW.checker_actor IS DISTINCT FROM OLD.checker_actor
           OR OLD.req_txn_id IS NOT NULL AND NEW.req_txn_id IS DISTINCT FROM OLD.req_txn_id
           OR OLD.paid_txn_id IS NOT NULL AND NEW.paid_txn_id IS DISTINCT FROM OLD.paid_txn_id THEN
            RAISE EXCEPTION 'withdrawal actors and transaction links are immutable once set' USING ERRCODE = '55000';
        END IF;

        IF NEW.status = OLD.status THEN
            IF OLD.req_txn_id IS NULL AND NEW.req_txn_id IS NOT NULL
               AND NEW.maker_actor IS NOT DISTINCT FROM OLD.maker_actor
               AND NEW.checker_actor IS NOT DISTINCT FROM OLD.checker_actor
               AND NEW.reviewed_at IS NOT DISTINCT FROM OLD.reviewed_at
               AND NEW.paid_txn_id IS NOT DISTINCT FROM OLD.paid_txn_id
               AND NEW.bank_ref IS NOT DISTINCT FROM OLD.bank_ref
               AND NEW.paid_at IS NOT DISTINCT FROM OLD.paid_at
               AND NEW.failed_reason IS NOT DISTINCT FROM OLD.failed_reason THEN
                SELECT type,amount,ref_type,ref_id
                  INTO linked_type,linked_amount,linked_ref_type,linked_ref_id
                  FROM ledger.txn WHERE id=NEW.req_txn_id;
                IF linked_type IS DISTINCT FROM 'withdraw_req'
                   OR linked_amount IS DISTINCT FROM NEW.amount
                   OR linked_ref_type IS DISTINCT FROM 'withdrawal'
                   OR linked_ref_id IS DISTINCT FROM NEW.id THEN
                    RAISE EXCEPTION 'withdrawal request transaction link is invalid' USING ERRCODE = '55000';
                END IF;
                RETURN NEW;
            END IF;
            RAISE EXCEPTION 'withdrawal same-state update is not allowed' USING ERRCODE = '55000';
        END IF;

        IF NOT ((OLD.status='requested' AND NEW.status IN ('review','cancelled'))
             OR (OLD.status='review' AND NEW.status IN ('approved','cancelled'))
             OR (OLD.status='approved' AND NEW.status IN ('paid','failed','cancelled'))) THEN
            RAISE EXCEPTION 'invalid withdrawal status transition from % to %', OLD.status, NEW.status USING ERRCODE = '55000';
        END IF;

        IF OLD.status='requested' AND NEW.status='review' THEN
            IF NOT OLD.requires_review OR NEW.maker_actor IS NULL OR NEW.maker_actor <= 0
               OR NEW.checker_actor IS NOT NULL OR NEW.reviewed_at IS NOT NULL
               OR NEW.paid_txn_id IS NOT NULL OR NEW.bank_ref IS NOT NULL OR NEW.paid_at IS NOT NULL
               OR NEW.failed_reason IS NOT NULL THEN
                RAISE EXCEPTION 'invalid review transition metadata' USING ERRCODE = '55000';
            END IF;
        ELSIF OLD.status='review' AND NEW.status='approved' THEN
            IF NEW.maker_actor IS DISTINCT FROM OLD.maker_actor OR NEW.checker_actor IS NULL
               OR NEW.reviewed_at IS NULL OR NEW.paid_txn_id IS NOT NULL
               OR NEW.bank_ref IS NOT NULL OR NEW.paid_at IS NOT NULL OR NEW.failed_reason IS NOT NULL THEN
                RAISE EXCEPTION 'invalid approval transition metadata' USING ERRCODE = '55000';
            END IF;
        ELSIF NEW.status='paid' THEN
            IF NEW.paid_txn_id IS NULL OR NEW.bank_ref IS NULL OR length(btrim(NEW.bank_ref))=0
               OR NEW.paid_at IS NULL OR NEW.failed_reason IS NOT NULL THEN
                RAISE EXCEPTION 'invalid paid transition metadata' USING ERRCODE = '55000';
            END IF;
            SELECT type,amount,ref_type,ref_id
              INTO linked_type,linked_amount,linked_ref_type,linked_ref_id
              FROM ledger.txn WHERE id=NEW.paid_txn_id;
            IF linked_type IS DISTINCT FROM 'payout'
               OR linked_amount IS DISTINCT FROM NEW.amount
               OR linked_ref_type IS DISTINCT FROM 'withdrawal'
               OR linked_ref_id IS DISTINCT FROM NEW.id THEN
                RAISE EXCEPTION 'withdrawal payout transaction link is invalid' USING ERRCODE = '55000';
            END IF;
        ELSIF NEW.status IN ('failed','cancelled') THEN
            IF NEW.failed_reason IS NULL OR length(btrim(NEW.failed_reason))=0
               OR NEW.paid_txn_id IS NOT NULL OR NEW.bank_ref IS NOT NULL OR NEW.paid_at IS NOT NULL THEN
                RAISE EXCEPTION 'invalid refund transition metadata' USING ERRCODE = '55000';
            END IF;
        END IF;
        IF NEW.status IN ('paid','failed','cancelled')
           AND (NEW.maker_actor IS DISTINCT FROM OLD.maker_actor
             OR NEW.checker_actor IS DISTINCT FROM OLD.checker_actor
             OR NEW.reviewed_at IS DISTINCT FROM OLD.reviewed_at) THEN
            RAISE EXCEPTION 'withdrawal review metadata cannot change while closing' USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'withdrawals cannot be deleted or truncated' USING ERRCODE = '55000';
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_withdrawal_guard BEFORE UPDATE OR DELETE ON ledger.withdrawal
    FOR EACH ROW EXECUTE FUNCTION ledger.protect_withdrawal();
CREATE TRIGGER trg_withdrawal_no_truncate BEFORE TRUNCATE ON ledger.withdrawal
    FOR EACH STATEMENT EXECUTE FUNCTION ledger.protect_withdrawal();

REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON ledger.withdrawal FROM ledger_rw;
GRANT SELECT, INSERT ON ledger.withdrawal TO ledger_rw;
GRANT UPDATE (status,maker_actor,checker_actor,reviewed_at,req_txn_id,paid_txn_id,bank_ref,paid_at,failed_reason) ON ledger.withdrawal TO ledger_rw;

-- +goose Down
REVOKE UPDATE ON ledger.withdrawal FROM ledger_rw;
DROP TRIGGER IF EXISTS trg_withdrawal_no_truncate ON ledger.withdrawal;
DROP TRIGGER IF EXISTS trg_withdrawal_guard ON ledger.withdrawal;
DROP FUNCTION IF EXISTS ledger.protect_withdrawal();
DROP TRIGGER IF EXISTS trg_withdrawal_insert_guard ON ledger.withdrawal;
DROP FUNCTION IF EXISTS ledger.guard_withdrawal_insert();
DROP INDEX IF EXISTS ledger.uq_withdrawal_bank_ref;
ALTER TABLE ledger.withdrawal DROP CONSTRAINT IF EXISTS ck_withdrawal_separation;
UPDATE ledger.withdrawal SET maker_actor=COALESCE(maker_actor,requester_actor);
ALTER TABLE ledger.withdrawal
    DROP COLUMN IF EXISTS failed_reason,
    DROP COLUMN IF EXISTS reviewed_at,
    DROP COLUMN IF EXISTS requires_review,
    DROP COLUMN IF EXISTS requester_actor,
    ALTER COLUMN maker_actor SET NOT NULL;
ALTER TABLE ledger.withdrawal ADD CONSTRAINT ck_withdrawal_separation CHECK (checker_actor IS NULL OR checker_actor <> maker_actor);
GRANT UPDATE ON ledger.withdrawal TO ledger_rw;
REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER ON ledger.withdrawal FROM ledger_rw;
