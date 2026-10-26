-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;

CREATE TABLE audit.chain_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    last_hash bytea CHECK (last_hash IS NULL OR octet_length(last_hash) = 32)
);
INSERT INTO audit.chain_state (singleton, last_hash) VALUES (true, NULL);

CREATE TABLE audit.log (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ts timestamptz NOT NULL DEFAULT now(),
    actor_id bigint NOT NULL,
    actor_role text,
    action text NOT NULL CHECK (nullif(btrim(action), '') IS NOT NULL),
    target text,
    detail jsonb,
    ip inet,
    request_id text,
    prev_hash bytea CHECK (prev_hash IS NULL OR octet_length(prev_hash) = 32),
    hash bytea NOT NULL UNIQUE CHECK (octet_length(hash) = 32),
    -- The hash payload joins fields with chr(31); forbidding it in free-text fields keeps the encoding unambiguous.
    CONSTRAINT ck_audit_log_no_separator CHECK (
        position(chr(31) in action) = 0 AND (target IS NULL OR position(chr(31) in target) = 0)
    )
);
CREATE INDEX ix_audit_log_ts ON audit.log (ts);
CREATE INDEX ix_audit_log_actor_id ON audit.log (actor_id, ts DESC);
CREATE INDEX ix_audit_log_request_id ON audit.log (request_id) WHERE request_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION audit.log_hash_payload(
    p_prev_hash bytea,
    p_ts timestamptz,
    p_actor_id bigint,
    p_action text,
    p_target text,
    p_detail jsonb
) RETURNS bytea
LANGUAGE sql
IMMUTABLE
SET search_path = pg_catalog
AS $$
    SELECT coalesce(p_prev_hash, ''::bytea) || convert_to(
        to_char(p_ts AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') || chr(31) ||
        p_actor_id::text || chr(31) ||
        p_action || chr(31) ||
        coalesce(p_target, '') || chr(31) ||
        coalesce(p_detail::text, ''),
        'UTF8'
    );
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION audit.assign_log_hash()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, audit
AS $$
DECLARE
    prior_hash bytea;
BEGIN
    IF NEW.actor_id <= 0 THEN
        RAISE EXCEPTION 'audit actor_id must be positive' USING ERRCODE = '22023';
    END IF;

    NEW.ts := transaction_timestamp();
    PERFORM pg_advisory_xact_lock(hashtextextended('audit.log.hash_chain', 0));
    SELECT last_hash INTO prior_hash FROM audit.chain_state WHERE singleton FOR UPDATE;

    NEW.prev_hash := prior_hash;
    NEW.hash := public.digest(
        audit.log_hash_payload(NEW.prev_hash, NEW.ts, NEW.actor_id, NEW.action, NEW.target, NEW.detail),
        'sha256'
    );
    UPDATE audit.chain_state SET last_hash = NEW.hash WHERE singleton;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION audit.reject_log_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'audit.log is append-only' USING ERRCODE = '55000';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER tr_audit_log_assign_hash
BEFORE INSERT ON audit.log
FOR EACH ROW EXECUTE FUNCTION audit.assign_log_hash();

CREATE TRIGGER tr_audit_log_reject_mutation
BEFORE UPDATE OR DELETE ON audit.log
FOR EACH ROW EXECUTE FUNCTION audit.reject_log_mutation();

CREATE TRIGGER tr_audit_log_reject_truncate
BEFORE TRUNCATE ON audit.log
FOR EACH STATEMENT EXECUTE FUNCTION audit.reject_log_mutation();

REVOKE ALL ON audit.chain_state FROM audit_writer, report_ro;
REVOKE ALL ON audit.log FROM audit_writer;
GRANT SELECT, INSERT ON audit.log TO audit_writer;
GRANT USAGE, SELECT ON SEQUENCE audit.log_id_seq TO audit_writer;
REVOKE ALL ON FUNCTION audit.log_hash_payload(bytea, timestamptz, bigint, text, text, jsonb) FROM PUBLIC;
REVOKE ALL ON FUNCTION audit.assign_log_hash() FROM PUBLIC;
REVOKE ALL ON FUNCTION audit.reject_log_mutation() FROM PUBLIC;

-- +goose Down
DROP TABLE audit.log;
DROP TABLE audit.chain_state;
DROP FUNCTION audit.reject_log_mutation();
DROP FUNCTION audit.assign_log_hash();
DROP FUNCTION audit.log_hash_payload(bytea, timestamptz, bigint, text, text, jsonb);
-- pgcrypto is intentionally left installed: other migrations or roles may rely on it.
