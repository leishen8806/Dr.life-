-- +goose Up
-- Q-01：不可变审计日志、串行化链头、事件幂等、检查结果与每日锚点。
-- main 当前最大为 00012；并行 A-03 已占用 00013，因此本任务使用 00014。

CREATE TABLE audit.log (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ts timestamptz NOT NULL,
    actor_id bigint NOT NULL CHECK (actor_id > 0),
    actor_role text NOT NULL CHECK (btrim(actor_role) <> ''),
    action text NOT NULL CHECK (btrim(action) <> ''),
    target text NOT NULL CHECK (btrim(target) <> ''),
    detail jsonb NOT NULL,
    ip inet,
    request_id text NOT NULL,
    prev_hash bytea NOT NULL CHECK (octet_length(prev_hash) = 32),
    hash bytea NOT NULL CHECK (octet_length(hash) = 32)
);
CREATE INDEX ix_audit_log_ts_id ON audit.log (ts DESC, id DESC);
CREATE INDEX ix_audit_log_actor ON audit.log (actor_id, id DESC);
CREATE INDEX ix_audit_log_action ON audit.log (action, id DESC);
CREATE INDEX ix_audit_log_target ON audit.log (target, id DESC);

CREATE TABLE audit.chain_head (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    last_id bigint,
    last_hash bytea NOT NULL CHECK (octet_length(last_hash) = 32),
    CHECK ((last_id IS NULL AND last_hash = decode(repeat('00', 32), 'hex')) OR last_id IS NOT NULL)
);
INSERT INTO audit.chain_head (singleton, last_id, last_hash)
VALUES (true, NULL, decode(repeat('00', 32), 'hex'));

CREATE TABLE audit.event_seen (
    event_id text PRIMARY KEY CHECK (btrim(event_id) <> ''),
    log_id bigint NOT NULL UNIQUE,
    seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit.check_result (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    day date NOT NULL,
    check_id text NOT NULL CHECK (btrim(check_id) <> ''),
    severity text NOT NULL CHECK (severity IN ('normal', 'attention', 'abnormal')),
    detail jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ix_audit_check_result_day ON audit.check_result (day DESC, check_id);

CREATE TABLE audit.anchor (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    day date NOT NULL UNIQUE,
    last_id bigint NOT NULL,
    last_hash bytea NOT NULL CHECK (octet_length(last_hash) = 32),
    anchored_at timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE FUNCTION audit.reject_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit records are append-only' USING ERRCODE = '55000';
END
$$;

CREATE FUNCTION audit.guard_chain_head() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    -- 唯一允许的 UPDATE 来自下面的 SECURITY DEFINER 函数。
    IF TG_OP = 'UPDATE' AND current_user <> session_user THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'audit chain head is managed internally' USING ERRCODE = '55000';
END
$$;

CREATE FUNCTION audit.advance_chain_head(expected_hash bytea, new_id bigint, new_hash bytea)
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
    UPDATE audit.chain_head
       SET last_id = new_id, last_hash = new_hash
     WHERE singleton AND last_hash = expected_hash;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'audit chain head changed' USING ERRCODE = '40001';
    END IF;
END
$$;

CREATE FUNCTION audit.lock_chain_head(OUT last_id bigint, OUT last_hash bytea)
RETURNS record
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, audit
AS 'SELECT last_id, last_hash FROM audit.chain_head WHERE singleton FOR UPDATE';
-- +goose StatementEnd

CREATE TRIGGER audit_log_no_update BEFORE UPDATE OR DELETE ON audit.log FOR EACH ROW EXECUTE FUNCTION audit.reject_mutation();
CREATE TRIGGER audit_log_no_truncate BEFORE TRUNCATE ON audit.log FOR EACH STATEMENT EXECUTE FUNCTION audit.reject_mutation();
CREATE TRIGGER audit_event_seen_no_update BEFORE UPDATE OR DELETE ON audit.event_seen FOR EACH ROW EXECUTE FUNCTION audit.reject_mutation();
CREATE TRIGGER audit_event_seen_no_truncate BEFORE TRUNCATE ON audit.event_seen FOR EACH STATEMENT EXECUTE FUNCTION audit.reject_mutation();
CREATE TRIGGER audit_check_result_no_update BEFORE UPDATE OR DELETE ON audit.check_result FOR EACH ROW EXECUTE FUNCTION audit.reject_mutation();
CREATE TRIGGER audit_check_result_no_truncate BEFORE TRUNCATE ON audit.check_result FOR EACH STATEMENT EXECUTE FUNCTION audit.reject_mutation();
CREATE TRIGGER audit_anchor_no_update BEFORE UPDATE OR DELETE ON audit.anchor FOR EACH ROW EXECUTE FUNCTION audit.reject_mutation();
CREATE TRIGGER audit_anchor_no_truncate BEFORE TRUNCATE ON audit.anchor FOR EACH STATEMENT EXECUTE FUNCTION audit.reject_mutation();
CREATE TRIGGER audit_chain_head_guard BEFORE INSERT OR UPDATE OR DELETE ON audit.chain_head FOR EACH ROW EXECUTE FUNCTION audit.guard_chain_head();
CREATE TRIGGER audit_chain_head_no_truncate BEFORE TRUNCATE ON audit.chain_head FOR EACH STATEMENT EXECUTE FUNCTION audit.reject_mutation();

REVOKE ALL ON FUNCTION audit.advance_chain_head(bytea, bigint, bytea) FROM PUBLIC;
REVOKE ALL ON FUNCTION audit.lock_chain_head() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION audit.advance_chain_head(bytea, bigint, bytea) TO audit_writer;
GRANT EXECUTE ON FUNCTION audit.lock_chain_head() TO audit_writer;

-- 00002 的默认权限已保证 audit_writer 只有 SELECT/INSERT、report_ro 只有 SELECT。

-- +goose Down
DROP FUNCTION audit.lock_chain_head();
DROP FUNCTION audit.advance_chain_head(bytea, bigint, bytea);
DROP TABLE audit.anchor;
DROP TABLE audit.check_result;
DROP TABLE audit.event_seen;
DROP TABLE audit.chain_head;
DROP TABLE audit.log;
DROP FUNCTION audit.guard_chain_head();
DROP FUNCTION audit.reject_mutation();
