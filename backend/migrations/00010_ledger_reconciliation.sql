-- +goose Up
CREATE TABLE ledger.recon_run (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    day date NOT NULL,
    kind text NOT NULL CHECK (kind IN ('daily','spot')),
    attempt integer NOT NULL DEFAULT 1 CHECK (attempt > 0),
    started_by bigint,
    force_reason text,
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    status text NOT NULL DEFAULT 'running' CHECK (status IN ('running','ok','failed')),
    summary jsonb NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT ck_recon_finished CHECK (
        (status = 'running' AND finished_at IS NULL) OR
        (status IN ('ok','failed') AND finished_at IS NOT NULL)
    ),
    CONSTRAINT ck_recon_force_reason CHECK (attempt = 1 OR btrim(COALESCE(force_reason,'')) <> ''),
    UNIQUE (day, kind, attempt)
);

CREATE TABLE ledger.recon_issue (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id bigint NOT NULL REFERENCES ledger.recon_run(id),
    check_code text NOT NULL CHECK (check_code IN (
        'BALANCE_CACHE','TXN_BALANCED','IDENTITY','HOLD_FROZEN',
        'PENDING_PAYOUT','REVERSAL','ORPHAN','SNAPSHOT_DRIFT'
    )),
    account_id bigint REFERENCES ledger.account(id),
    txn_id bigint REFERENCES ledger.txn(id),
    expected bigint,
    actual bigint,
    detail text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ix_recon_issue_run ON ledger.recon_issue(run_id, check_code, id);

CREATE TABLE ledger.payout_freeze (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    scope text NOT NULL CHECK (scope IN ('global','account')),
    account_id bigint REFERENCES ledger.account(id),
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    run_id bigint NOT NULL REFERENCES ledger.recon_run(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    released_at timestamptz,
    released_by bigint,
    release_reason text,
    CONSTRAINT ck_payout_freeze_scope CHECK (
        (scope = 'global' AND account_id IS NULL) OR
        (scope = 'account' AND account_id IS NOT NULL)
    ),
    CONSTRAINT ck_payout_freeze_release CHECK (
        (released_at IS NULL AND released_by IS NULL AND release_reason IS NULL) OR
        (released_at IS NOT NULL AND released_by IS NOT NULL AND btrim(COALESCE(release_reason,'')) <> '')
    )
);
CREATE UNIQUE INDEX uq_payout_freeze_global_open ON ledger.payout_freeze(scope)
    WHERE scope = 'global' AND released_at IS NULL;
CREATE UNIQUE INDEX uq_payout_freeze_account_open ON ledger.payout_freeze(account_id)
    WHERE scope = 'account' AND released_at IS NULL;

CREATE TRIGGER trg_snapshot_daily_ro BEFORE UPDATE OR DELETE ON ledger.snapshot_daily
    FOR EACH ROW EXECUTE FUNCTION ledger.deny_mutation();
CREATE TRIGGER trg_snapshot_daily_no_truncate BEFORE TRUNCATE ON ledger.snapshot_daily
    FOR EACH STATEMENT EXECUTE FUNCTION ledger.deny_mutation();
CREATE TRIGGER trg_recon_issue_ro BEFORE UPDATE OR DELETE ON ledger.recon_issue
    FOR EACH ROW EXECUTE FUNCTION ledger.deny_mutation();
CREATE TRIGGER trg_recon_issue_no_truncate BEFORE TRUNCATE ON ledger.recon_issue
    FOR EACH STATEMENT EXECUTE FUNCTION ledger.deny_mutation();

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_recon_run() RETURNS trigger AS $$
BEGIN
    IF TG_OP <> 'UPDATE' THEN
        RAISE EXCEPTION 'reconciliation runs cannot be deleted or truncated' USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> 'running' THEN
        RAISE EXCEPTION 'completed reconciliation runs are immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.day IS DISTINCT FROM OLD.day
       OR NEW.kind IS DISTINCT FROM OLD.kind
       OR NEW.attempt IS DISTINCT FROM OLD.attempt
       OR NEW.started_by IS DISTINCT FROM OLD.started_by
       OR NEW.force_reason IS DISTINCT FROM OLD.force_reason
       OR NEW.started_at IS DISTINCT FROM OLD.started_at
       OR NEW.status NOT IN ('ok','failed')
       OR NEW.finished_at IS NULL THEN
        RAISE EXCEPTION 'reconciliation run may only transition once from running to completed' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER trg_recon_run_guard BEFORE UPDATE OR DELETE ON ledger.recon_run
    FOR EACH ROW EXECUTE FUNCTION ledger.protect_recon_run();
CREATE TRIGGER trg_recon_run_no_truncate BEFORE TRUNCATE ON ledger.recon_run
    FOR EACH STATEMENT EXECUTE FUNCTION ledger.protect_recon_run();

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_payout_freeze() RETURNS trigger AS $$
BEGIN
    IF TG_OP <> 'UPDATE' THEN
        RAISE EXCEPTION 'payout freezes cannot be deleted or truncated' USING ERRCODE = '55000';
    END IF;
    IF OLD.released_at IS NOT NULL
       OR NEW.id IS DISTINCT FROM OLD.id
       OR NEW.scope IS DISTINCT FROM OLD.scope
       OR NEW.account_id IS DISTINCT FROM OLD.account_id
       OR NEW.reason IS DISTINCT FROM OLD.reason
       OR NEW.run_id IS DISTINCT FROM OLD.run_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR NEW.released_at IS NULL
       OR NEW.released_by IS NULL
       OR btrim(COALESCE(NEW.release_reason,'')) = '' THEN
        RAISE EXCEPTION 'payout freeze may only be released once' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER trg_payout_freeze_guard BEFORE UPDATE OR DELETE ON ledger.payout_freeze
    FOR EACH ROW EXECUTE FUNCTION ledger.protect_payout_freeze();
CREATE TRIGGER trg_payout_freeze_no_truncate BEFORE TRUNCATE ON ledger.payout_freeze
    FOR EACH STATEMENT EXECUTE FUNCTION ledger.protect_payout_freeze();

REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON ledger.snapshot_daily FROM ledger_rw;
GRANT SELECT, INSERT ON ledger.snapshot_daily TO ledger_rw;

REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON ledger.recon_issue FROM ledger_rw;
GRANT SELECT, INSERT ON ledger.recon_issue TO ledger_rw;

REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON ledger.recon_run FROM ledger_rw;
GRANT SELECT, INSERT ON ledger.recon_run TO ledger_rw;
GRANT UPDATE (status, finished_at, summary) ON ledger.recon_run TO ledger_rw;

REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON ledger.payout_freeze FROM ledger_rw;
GRANT SELECT, INSERT ON ledger.payout_freeze TO ledger_rw;
GRANT UPDATE (released_at, released_by, release_reason) ON ledger.payout_freeze TO ledger_rw;

-- +goose Down
GRANT UPDATE ON ledger.snapshot_daily TO ledger_rw;

DROP TRIGGER IF EXISTS trg_payout_freeze_no_truncate ON ledger.payout_freeze;
DROP TRIGGER IF EXISTS trg_payout_freeze_guard ON ledger.payout_freeze;
DROP FUNCTION IF EXISTS ledger.protect_payout_freeze();
DROP TRIGGER IF EXISTS trg_recon_run_no_truncate ON ledger.recon_run;
DROP TRIGGER IF EXISTS trg_recon_run_guard ON ledger.recon_run;
DROP FUNCTION IF EXISTS ledger.protect_recon_run();
DROP TRIGGER IF EXISTS trg_recon_issue_no_truncate ON ledger.recon_issue;
DROP TRIGGER IF EXISTS trg_recon_issue_ro ON ledger.recon_issue;
DROP TRIGGER IF EXISTS trg_snapshot_daily_no_truncate ON ledger.snapshot_daily;
DROP TRIGGER IF EXISTS trg_snapshot_daily_ro ON ledger.snapshot_daily;

DROP TABLE IF EXISTS ledger.payout_freeze;
DROP TABLE IF EXISTS ledger.recon_issue;
DROP TABLE IF EXISTS ledger.recon_run;
