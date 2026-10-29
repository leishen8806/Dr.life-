-- +goose Up
REVOKE SELECT ON iam.account FROM report_ro;
GRANT SELECT (id, public_id, person_id, role, merchant_id, status, lang, lang_registered_at, kyc_status, created_at, updated_at) ON iam.account TO report_ro;

-- +goose Down
GRANT SELECT ON iam.account TO report_ro;
