-- +goose Up
-- The existing guard permits the only required terminal transition: running to
-- failed, including stale-run recovery. Recreate it in a forward migration so
-- this permission is explicit in the migration history.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_recon_run() RETURNS trigger AS $$
BEGIN
    IF TG_OP <> 'UPDATE' THEN RAISE EXCEPTION 'reconciliation runs cannot be deleted or truncated' USING ERRCODE = '55000'; END IF;
    IF OLD.status <> 'running' OR NEW.id IS DISTINCT FROM OLD.id OR NEW.day IS DISTINCT FROM OLD.day OR NEW.kind IS DISTINCT FROM OLD.kind OR NEW.attempt IS DISTINCT FROM OLD.attempt OR NEW.started_by IS DISTINCT FROM OLD.started_by OR NEW.force_reason IS DISTINCT FROM OLD.force_reason OR NEW.started_at IS DISTINCT FROM OLD.started_at OR NEW.status NOT IN ('ok','failed') OR NEW.finished_at IS NULL THEN
        RAISE EXCEPTION 'reconciliation run may only transition once from running to completed' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_recon_run() RETURNS trigger AS $$
BEGIN
    IF TG_OP <> 'UPDATE' THEN
        RAISE EXCEPTION 'reconciliation runs cannot be deleted or truncated' USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> 'running' THEN
        RAISE EXCEPTION 'completed reconciliation runs are immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.day IS DISTINCT FROM OLD.day
       OR NEW.kind IS DISTINCT FROM OLD.kind OR NEW.attempt IS DISTINCT FROM OLD.attempt
       OR NEW.started_by IS DISTINCT FROM OLD.started_by OR NEW.force_reason IS DISTINCT FROM OLD.force_reason
       OR NEW.started_at IS DISTINCT FROM OLD.started_at OR NEW.status NOT IN ('ok','failed')
       OR NEW.finished_at IS NULL THEN
        RAISE EXCEPTION 'reconciliation run may only transition once from running to completed' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
