-- +goose Up
-- ops_rw inherits broad default table privileges from 00002. File metadata must
-- remain recoverable and cannot be deleted or truncated by the application role.
REVOKE DELETE, TRUNCATE ON ops.file FROM ops_rw;

-- +goose Down
GRANT DELETE, TRUNCATE ON ops.file TO ops_rw;
