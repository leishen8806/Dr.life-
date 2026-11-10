-- +goose Up
ALTER TABLE ops.file
    ADD COLUMN cleanup_failures integer NOT NULL DEFAULT 0 CHECK (cleanup_failures >= 0),
    ADD COLUMN cleanup_next_attempt_at timestamptz;

CREATE INDEX idx_ops_file_cleanup_retry
    ON ops.file(cleanup_next_attempt_at, created_at)
    WHERE status IN ('pending', 'verifying', 'ready', 'rejected');

-- +goose StatementBegin
CREATE FUNCTION ops.prevent_referenced_file_terminal_status()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ops, pg_temp
AS $$
BEGIN
    IF NEW.status IN ('deleted', 'expired')
       AND OLD.status IS DISTINCT FROM NEW.status
       AND EXISTS (
           SELECT 1 FROM file_reference r
           WHERE r.file_id = NEW.id AND r.released_at IS NULL
       ) THEN
        RAISE EXCEPTION 'active file reference prevents terminal status'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION ops.prevent_referenced_file_terminal_status() FROM PUBLIC;

CREATE TRIGGER trg_ops_file_reference_terminal_guard
BEFORE UPDATE OF status ON ops.file
FOR EACH ROW
EXECUTE FUNCTION ops.prevent_referenced_file_terminal_status();

GRANT UPDATE (cleanup_failures, cleanup_next_attempt_at) ON ops.file TO ops_file_worker;

-- +goose Down
REVOKE UPDATE (cleanup_failures, cleanup_next_attempt_at) ON ops.file FROM ops_file_worker;
DROP TRIGGER trg_ops_file_reference_terminal_guard ON ops.file;
DROP FUNCTION ops.prevent_referenced_file_terminal_status();
DROP INDEX ops.idx_ops_file_cleanup_retry;
ALTER TABLE ops.file
    DROP COLUMN cleanup_next_attempt_at,
    DROP COLUMN cleanup_failures;
