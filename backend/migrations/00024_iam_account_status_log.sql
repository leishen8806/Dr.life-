-- +goose Up
CREATE TABLE iam.account_status_log (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    account_id bigint NOT NULL REFERENCES iam.account(id),
    from_status text NOT NULL CHECK (from_status IN ('active', 'frozen', 'disabled')),
    to_status text NOT NULL CHECK (to_status IN ('active', 'frozen', 'disabled')),
    reason text NOT NULL CHECK (nullif(btrim(reason), '') IS NOT NULL),
    actor_type text NOT NULL CHECK (actor_type IN ('admin', 'system')),
    actor_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (from_status <> to_status)
);
CREATE INDEX ix_account_status_log_account_created_at ON iam.account_status_log (account_id, created_at DESC);

-- +goose StatementBegin
CREATE FUNCTION iam.reject_account_status_log_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'account status logs are append-only' USING ERRCODE = '55000';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_account_status_log_no_update_delete BEFORE UPDATE OR DELETE ON iam.account_status_log
    FOR EACH ROW EXECUTE FUNCTION iam.reject_account_status_log_mutation();
CREATE TRIGGER trg_account_status_log_no_truncate BEFORE TRUNCATE ON iam.account_status_log
    FOR EACH STATEMENT EXECUTE FUNCTION iam.reject_account_status_log_mutation();

REVOKE ALL ON iam.account_status_log FROM iam_rw;
GRANT SELECT, INSERT ON iam.account_status_log TO iam_rw;
REVOKE SELECT ON iam.account_status_log FROM report_ro;
GRANT SELECT (id, public_id, account_id, from_status, to_status, actor_type, actor_id, created_at) ON iam.account_status_log TO report_ro;

-- +goose Down
REVOKE SELECT ON iam.account_status_log FROM report_ro;
GRANT SELECT ON iam.account_status_log TO report_ro;
GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON iam.account_status_log TO iam_rw;
DROP TRIGGER IF EXISTS trg_account_status_log_no_truncate ON iam.account_status_log;
DROP TRIGGER IF EXISTS trg_account_status_log_no_update_delete ON iam.account_status_log;
DROP FUNCTION IF EXISTS iam.reject_account_status_log_mutation();
DROP TABLE iam.account_status_log;
