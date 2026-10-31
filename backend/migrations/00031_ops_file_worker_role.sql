-- +goose Up
-- File verification is the only operation allowed to replace a pending upload
-- key with a private ready key. Keep it separate from the API's ops_rw role.
CREATE ROLE ops_file_worker NOLOGIN;
GRANT USAGE ON SCHEMA ops TO ops_file_worker;
GRANT SELECT ON ops.file TO ops_file_worker;
GRANT UPDATE (status, object_key, updated_at) ON ops.file TO ops_file_worker;

-- +goose Down
DROP OWNED BY ops_file_worker;
DROP ROLE ops_file_worker;
