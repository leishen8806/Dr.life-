-- +goose Up
-- cmd/server owns the public catalog/trade/comms paths and the file lifecycle.
-- Keep this role separate from iam/ledger and from the broad ops_rw role.
CREATE ROLE server_rw NOLOGIN;
GRANT catalog_rw, trade_rw, comms_rw TO server_rw;

GRANT USAGE ON SCHEMA ops TO server_rw;
GRANT SELECT, INSERT ON ops.file TO server_rw;
GRANT USAGE, SELECT ON SEQUENCE ops.file_id_seq TO server_rw;
GRANT UPDATE (status, object_key, size, sha256, retention_until, cleanup_started_at, cleanup_failures, cleanup_next_attempt_at, updated_at) ON ops.file TO server_rw;
GRANT SELECT, INSERT ON ops.file_reference TO server_rw;
GRANT UPDATE (released_at, updated_at) ON ops.file_reference TO server_rw;
GRANT USAGE, SELECT ON SEQUENCE ops.file_reference_id_seq TO server_rw;

-- +goose Down
REVOKE USAGE, SELECT ON SEQUENCE ops.file_reference_id_seq FROM server_rw;
REVOKE UPDATE (released_at, updated_at) ON ops.file_reference FROM server_rw;
REVOKE SELECT, INSERT ON ops.file_reference FROM server_rw;
REVOKE UPDATE (status, object_key, size, sha256, retention_until, cleanup_started_at, cleanup_failures, cleanup_next_attempt_at, updated_at) ON ops.file FROM server_rw;
REVOKE USAGE, SELECT ON SEQUENCE ops.file_id_seq FROM server_rw;
REVOKE SELECT, INSERT ON ops.file FROM server_rw;
REVOKE USAGE ON SCHEMA ops FROM server_rw;
REVOKE catalog_rw, trade_rw, comms_rw FROM server_rw;
DROP ROLE server_rw;
