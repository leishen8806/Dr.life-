-- +goose Up
ALTER TABLE ops.file ADD COLUMN cleanup_started_at timestamptz;

-- Existing ready files predate reference tracking. Fail closed until their
-- owning modules attach or release a reference explicitly.
UPDATE ops.file SET retention_until = NULL WHERE status = 'ready';

CREATE TABLE ops.file_reference (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    file_id bigint NOT NULL REFERENCES ops.file(id),
    reference_public_id text NOT NULL,
    released_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (file_id, reference_public_id)
);

CREATE INDEX idx_ops_file_retention_cleanup
    ON ops.file(retention_until, created_at)
    WHERE status IN ('pending', 'verifying', 'ready', 'rejected');
CREATE INDEX idx_ops_file_reference_active
    ON ops.file_reference(file_id) WHERE released_at IS NULL;

GRANT SELECT, INSERT ON ops.file_reference TO ops_rw;
GRANT UPDATE (released_at, updated_at) ON ops.file_reference TO ops_rw;
GRANT USAGE, SELECT ON SEQUENCE ops.file_reference_id_seq TO ops_rw;
GRANT UPDATE (retention_until, cleanup_started_at, updated_at) ON ops.file TO ops_rw;
GRANT SELECT ON ops.file_reference TO ops_file_worker;
GRANT UPDATE (cleanup_started_at) ON ops.file TO ops_file_worker;

-- +goose Down
REVOKE UPDATE (cleanup_started_at) ON ops.file FROM ops_file_worker;
REVOKE SELECT ON ops.file_reference FROM ops_file_worker;
REVOKE UPDATE (retention_until, cleanup_started_at, updated_at) ON ops.file FROM ops_rw;
REVOKE USAGE, SELECT ON SEQUENCE ops.file_reference_id_seq FROM ops_rw;
REVOKE UPDATE (released_at, updated_at) ON ops.file_reference FROM ops_rw;
REVOKE SELECT, INSERT ON ops.file_reference FROM ops_rw;
DROP INDEX ops.idx_ops_file_reference_active;
DROP INDEX ops.idx_ops_file_retention_cleanup;
DROP TABLE ops.file_reference;
ALTER TABLE ops.file DROP COLUMN cleanup_started_at;
