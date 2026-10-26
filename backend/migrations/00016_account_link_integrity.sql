-- +goose Up
-- Existing account_link rows are preserved. New links are constrained to the
-- same Telegram person and to the active owner account of its merchant.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION iam.enforce_account_link_integrity()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM iam.account buyer
        JOIN iam.account merchant ON merchant.id = NEW.merchant_account_id
        JOIN iam.merchant m ON m.id = merchant.merchant_id
        WHERE buyer.id = NEW.buyer_account_id
          AND buyer.person_id = merchant.person_id
          AND buyer.role = 'buyer' AND buyer.status = 'active'
          AND merchant.role = 'merchant_owner' AND merchant.status = 'active'
          AND m.owner_account_id = merchant.id
    ) THEN
        RAISE EXCEPTION 'account link requires active buyer and active merchant owner for same person'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER trg_account_link_integrity
BEFORE INSERT OR UPDATE OF buyer_account_id, merchant_account_id ON iam.account_link
FOR EACH ROW EXECUTE FUNCTION iam.enforce_account_link_integrity();

-- +goose Down
DROP TRIGGER trg_account_link_integrity ON iam.account_link;
-- +goose StatementBegin
DROP FUNCTION iam.enforce_account_link_integrity();
-- +goose StatementEnd
