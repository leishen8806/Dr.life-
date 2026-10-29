-- +goose Up
CREATE TABLE ops.file (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE,
    owner_account bigint NOT NULL,
    purpose text NOT NULL,
    object_key text NOT NULL UNIQUE,
    content_type text NOT NULL,
    size bigint NOT NULL CHECK (size > 0),
    sha256 bytea NOT NULL CHECK (octet_length(sha256) = 32),
    status text NOT NULL,
    retention_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_ops_file_purpose CHECK (purpose IN ('merchant_kyc', 'product_image', 'inbound_photo', 'delivery_proof')),
    CONSTRAINT ck_ops_file_status CHECK (status IN ('pending', 'verifying', 'ready', 'rejected', 'expired', 'deleted'))
);

CREATE INDEX idx_ops_file_owner_created_at ON ops.file(owner_account, created_at DESC);
CREATE INDEX idx_ops_file_status_created_at ON ops.file(status, created_at DESC);

-- The opaque owner id is resolved through IAM's public interface; do not add a cross-schema foreign key.
GRANT SELECT, INSERT, UPDATE ON ops.file TO ops_rw;
GRANT USAGE, SELECT ON SEQUENCE ops.file_id_seq TO ops_rw;

-- +goose Down
REVOKE USAGE, SELECT ON SEQUENCE ops.file_id_seq FROM ops_rw;
REVOKE SELECT, INSERT, UPDATE ON ops.file FROM ops_rw;
DROP TABLE ops.file;
