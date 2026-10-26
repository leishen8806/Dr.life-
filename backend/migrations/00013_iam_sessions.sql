-- +goose Up
ALTER TABLE iam.account
    ADD COLUMN token_epoch bigint NOT NULL DEFAULT 0 CHECK (token_epoch >= 0);

ALTER TABLE iam.session
    ADD COLUMN family_id text,
    ADD COLUMN previous_hash bytea,
    ADD COLUMN rotated_at timestamptz,
    ADD COLUMN last_used_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN rotation bigint NOT NULL DEFAULT 0 CHECK (rotation >= 0),
    ADD COLUMN refresh_kid text;

UPDATE iam.session SET family_id = public_id WHERE family_id IS NULL;
ALTER TABLE iam.session ALTER COLUMN family_id SET NOT NULL;

CREATE INDEX ix_session_family_id ON iam.session (family_id);
CREATE INDEX ix_session_previous_hash ON iam.session (previous_hash) WHERE previous_hash IS NOT NULL;
CREATE INDEX ix_session_active_account ON iam.session (account_id, created_at) WHERE revoked_at IS NULL;

CREATE TABLE iam.security_event (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    account_id bigint NOT NULL REFERENCES iam.account(id),
    session_family_id text,
    event text NOT NULL CHECK (event IN ('refresh_reuse')),
    reason text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE iam.security_event;
DROP INDEX iam.ix_session_active_account;
DROP INDEX iam.ix_session_previous_hash;
DROP INDEX iam.ix_session_family_id;
ALTER TABLE iam.session
    DROP COLUMN refresh_kid,
    DROP COLUMN rotation,
    DROP COLUMN last_used_at,
    DROP COLUMN rotated_at,
    DROP COLUMN previous_hash,
    DROP COLUMN family_id;
ALTER TABLE iam.account DROP COLUMN token_epoch;
