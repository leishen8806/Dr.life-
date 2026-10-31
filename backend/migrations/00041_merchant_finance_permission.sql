-- +goose Up
-- Merchant finance staff may view financial information but may not initiate
-- financial actions. Existing copies are repaired without touching any other
-- role/module setting a merchant may have customized.
UPDATE iam.merchant_role_perm_template
SET level = 'view', updated_at = now()
WHERE role = 'finance' AND module = 'fin' AND level = 'edit';

UPDATE iam.merchant_role_perm
SET level = 'view', updated_at = now()
WHERE role = 'finance' AND module = 'fin' AND level = 'edit';

-- +goose Down
-- Intentionally no-op: a rollback must not restore finance edit permission.
