-- +goose Up
CREATE ROLE iam_rw NOLOGIN;
CREATE ROLE catalog_rw NOLOGIN;
CREATE ROLE trade_rw NOLOGIN;
CREATE ROLE wms_rw NOLOGIN;
CREATE ROLE logistics_rw NOLOGIN;
CREATE ROLE ledger_rw NOLOGIN;
CREATE ROLE comms_rw NOLOGIN;
CREATE ROLE ai_rw NOLOGIN;
CREATE ROLE ops_rw NOLOGIN;
CREATE ROLE audit_writer NOLOGIN;
CREATE ROLE report_ro NOLOGIN;

REVOKE CREATE ON SCHEMA public FROM PUBLIC;

-- +goose StatementBegin
DO $$
DECLARE
    pair text[];
BEGIN
    FOREACH pair SLICE 1 IN ARRAY ARRAY[
        ['iam', 'iam_rw'],
        ['catalog', 'catalog_rw'],
        ['trade', 'trade_rw'],
        ['wms', 'wms_rw'],
        ['logistics', 'logistics_rw'],
        ['ledger', 'ledger_rw'],
        ['comms', 'comms_rw'],
        ['ai', 'ai_rw'],
        ['ops', 'ops_rw']
    ] LOOP
        EXECUTE format('GRANT USAGE ON SCHEMA %I TO %I', pair[1], pair[2]);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON ALL TABLES IN SCHEMA %I TO %I', pair[1], pair[2]);
        EXECUTE format('GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA %I TO %I', pair[1], pair[2]);
        EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA %I GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER ON TABLES TO %I', pair[1], pair[2]);
        EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA %I GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO %I', pair[1], pair[2]);
    END LOOP;
END
$$;
-- +goose StatementEnd

GRANT USAGE ON SCHEMA audit TO audit_writer;
GRANT SELECT, INSERT ON ALL TABLES IN SCHEMA audit TO audit_writer;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA audit TO audit_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA audit GRANT SELECT, INSERT ON TABLES TO audit_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA audit GRANT USAGE, SELECT ON SEQUENCES TO audit_writer;

GRANT USAGE ON SCHEMA iam, catalog, trade, wms, logistics, ledger, comms, ai, ops, audit TO report_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA iam, catalog, trade, wms, logistics, ledger, comms, ai, ops, audit TO report_ro;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA iam, catalog, trade, wms, logistics, ledger, comms, ai, ops, audit TO report_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA iam GRANT SELECT ON TABLES TO report_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA catalog GRANT SELECT ON TABLES TO report_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA trade GRANT SELECT ON TABLES TO report_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA wms GRANT SELECT ON TABLES TO report_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA logistics GRANT SELECT ON TABLES TO report_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA ledger GRANT SELECT ON TABLES TO report_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA comms GRANT SELECT ON TABLES TO report_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA ai GRANT SELECT ON TABLES TO report_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA ops GRANT SELECT ON TABLES TO report_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA audit GRANT SELECT ON TABLES TO report_ro;

-- +goose Down
GRANT CREATE ON SCHEMA public TO PUBLIC;

-- +goose StatementBegin
DO $$
DECLARE
    role_name text;
BEGIN
    FOREACH role_name IN ARRAY ARRAY[
        'iam_rw', 'catalog_rw', 'trade_rw', 'wms_rw', 'logistics_rw',
        'ledger_rw', 'comms_rw', 'ai_rw', 'ops_rw', 'audit_writer', 'report_ro'
    ] LOOP
        EXECUTE format('DROP OWNED BY %I', role_name);
        EXECUTE format('DROP ROLE %I', role_name);
    END LOOP;
END
$$;
-- +goose StatementEnd
