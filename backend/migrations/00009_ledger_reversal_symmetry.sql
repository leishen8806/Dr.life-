-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION ledger.check_reversal_symmetry() RETURNS trigger AS $$
DECLARE
    original_id bigint;
    original_type text;
BEGIN
    SELECT reverses_txn_id INTO original_id FROM ledger.txn WHERE id=NEW.id;
    IF original_id IS NULL THEN
        RETURN NULL;
    END IF;

    SELECT type INTO original_type FROM ledger.txn WHERE id=original_id;
    IF original_type NOT IN (
        'topup','storage_fee','member_fee','ad_charge','ad_spend',
        'ad_refund','bank_diff','sweep','carrier_pay','adjust'
    ) THEN
        RAISE EXCEPTION 'transaction type % cannot be reversed', original_type USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT account_id,CASE side WHEN 'DEBIT' THEN 'CREDIT' ELSE 'DEBIT' END,amount,role,category
          FROM ledger.posting WHERE txn_id=original_id
        EXCEPT ALL
        SELECT account_id,side,amount,role,category
          FROM ledger.posting WHERE txn_id=NEW.id
    ) OR EXISTS (
        SELECT account_id,side,amount,role,category
          FROM ledger.posting WHERE txn_id=NEW.id
        EXCEPT ALL
        SELECT account_id,CASE side WHEN 'DEBIT' THEN 'CREDIT' ELSE 'DEBIT' END,amount,role,category
          FROM ledger.posting WHERE txn_id=original_id
    ) THEN
        RAISE EXCEPTION 'reversal postings must exactly mirror the original transaction' USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER trg_reversal_symmetry
    AFTER INSERT ON ledger.txn
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ledger.check_reversal_symmetry();

-- +goose Down
DROP TRIGGER IF EXISTS trg_reversal_symmetry ON ledger.txn;
DROP FUNCTION IF EXISTS ledger.check_reversal_symmetry();
