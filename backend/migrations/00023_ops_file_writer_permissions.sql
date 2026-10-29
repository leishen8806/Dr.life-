-- +goose Up
-- ops_rw inherits broad default table privileges from 00002. File metadata must
-- remain recoverable and cannot be deleted or truncated by the application role.
REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER ON ops.file FROM ops_rw;
REVOKE UPDATE ON ops.file FROM ops_rw;
GRANT UPDATE (status, updated_at) ON ops.file TO ops_rw;

-- +goose Down
REVOKE UPDATE (status, updated_at) ON ops.file FROM ops_rw;
GRANT DELETE, TRUNCATE, REFERENCES, TRIGGER, UPDATE ON ops.file TO ops_rw;
