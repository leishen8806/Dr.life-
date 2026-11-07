-- +goose Up
-- Administrators use the existing session rotation and replay-protection chain.
-- A session is owned by exactly one mobile account or one administrator.
ALTER TABLE iam.session
    ADD COLUMN admin_user_id bigint REFERENCES iam.admin_user(id);

ALTER TABLE iam.admin_user
    ADD COLUMN password_change_required boolean NOT NULL DEFAULT false;

ALTER TABLE iam.session
    ALTER COLUMN account_id DROP NOT NULL;

ALTER TABLE iam.session
    ADD CONSTRAINT ck_session_owner CHECK (
        (account_id IS NOT NULL)::integer + (admin_user_id IS NOT NULL)::integer = 1
    );

CREATE INDEX ix_session_admin_user_id ON iam.session (admin_user_id) WHERE admin_user_id IS NOT NULL;

CREATE TABLE iam.admin_audit_outbox (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    occurred_at timestamptz NOT NULL,
    actor_id bigint NOT NULL,
    actor_role text NOT NULL,
    action text NOT NULL,
    target text NOT NULL,
    detail jsonb NOT NULL,
    ip inet,
    request_id text NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    last_error text,
    delivered_at timestamptz
);

CREATE INDEX ix_admin_audit_outbox_pending ON iam.admin_audit_outbox (id) WHERE delivered_at IS NULL;
CREATE INDEX ix_admin_audit_outbox_delivered ON iam.admin_audit_outbox (delivered_at) WHERE delivered_at IS NOT NULL;

-- +goose Down
DROP TABLE iam.admin_audit_outbox;
DROP INDEX iam.ix_session_admin_user_id;
ALTER TABLE iam.session DROP CONSTRAINT ck_session_owner;
DELETE FROM iam.session WHERE admin_user_id IS NOT NULL;
ALTER TABLE iam.session ALTER COLUMN account_id SET NOT NULL;
ALTER TABLE iam.session DROP COLUMN admin_user_id;
ALTER TABLE iam.admin_user DROP COLUMN password_change_required;
