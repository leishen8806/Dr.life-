-- +goose Up
ALTER TABLE ledger.withdrawal ADD COLUMN request_key text;
UPDATE ledger.withdrawal SET request_key='legacy-' || public_id WHERE request_key IS NULL;
ALTER TABLE ledger.withdrawal
    ALTER COLUMN request_key SET NOT NULL,
    ADD CONSTRAINT ck_withdrawal_request_key CHECK (
        length(request_key) BETWEEN 1 AND 100
        AND request_key ~ '^[A-Za-z0-9_-]+$'
    ),
    ADD CONSTRAINT uq_withdrawal_request_key UNIQUE (request_key);

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_withdrawal_request_key() RETURNS trigger AS $$
BEGIN
    IF NEW.request_key IS DISTINCT FROM OLD.request_key THEN
        RAISE EXCEPTION 'withdrawal request key is immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_withdrawal_request_key_immutable
    BEFORE UPDATE ON ledger.withdrawal
    FOR EACH ROW EXECUTE FUNCTION ledger.protect_withdrawal_request_key();

-- +goose Down
DROP TRIGGER IF EXISTS trg_withdrawal_request_key_immutable ON ledger.withdrawal;
DROP FUNCTION IF EXISTS ledger.protect_withdrawal_request_key();
ALTER TABLE ledger.withdrawal
    DROP CONSTRAINT IF EXISTS uq_withdrawal_request_key,
    DROP CONSTRAINT IF EXISTS ck_withdrawal_request_key,
    DROP COLUMN IF EXISTS request_key;
