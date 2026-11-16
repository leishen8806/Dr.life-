-- +goose Up
-- The original chain-head guard used current_user <> session_user to identify
-- the SECURITY DEFINER updater. That is false when migrations and tests are
-- owned by the same database role as the session. Keep the guard fail-closed
-- for ordinary updates and use a transaction-local marker set only by the
-- SECURITY DEFINER function that has already verified the expected hash.
--
-- The marker is not a privilege grant: callers still need UPDATE on the table,
-- which is intentionally not granted to audit_writer or report_ro.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION audit.guard_chain_head() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND current_setting('audit.chain_head_mutation', true) = '1' THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'audit chain head is managed internally' USING ERRCODE = '55000';
END
$$;

CREATE OR REPLACE FUNCTION audit.advance_chain_head(expected_hash bytea, new_id bigint, new_hash bytea)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, audit
AS $$
BEGIN
    IF octet_length(expected_hash) <> 32 OR octet_length(new_hash) <> 32 THEN
        RAISE EXCEPTION 'invalid audit hash length' USING ERRCODE = '22023';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM audit.log
         WHERE id = new_id AND prev_hash = expected_hash AND hash = new_hash
    ) THEN
        RAISE EXCEPTION 'audit chain row does not match requested head' USING ERRCODE = '55000';
    END IF;
    PERFORM set_config('audit.chain_head_mutation', '1', true);
    UPDATE audit.chain_head
       SET last_id = new_id, last_hash = new_hash
     WHERE singleton AND last_hash = expected_hash;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'audit chain head changed' USING ERRCODE = '40001';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Keep the corrected implementation on rollback targets; removing it would
-- reintroduce the same owner/session-role false negative.
