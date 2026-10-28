-- +goose Up
-- Administrators use the existing session rotation and replay-protection chain.
-- A session is owned by exactly one mobile account or one administrator.
ALTER TABLE iam.session
    ADD COLUMN admin_user_id bigint REFERENCES iam.admin_user(id);

ALTER TABLE iam.session
    ALTER COLUMN account_id DROP NOT NULL;

ALTER TABLE iam.session
    ADD CONSTRAINT ck_session_owner CHECK (
        (account_id IS NOT NULL)::integer + (admin_user_id IS NOT NULL)::integer = 1
    );

CREATE INDEX ix_session_admin_user_id ON iam.session (admin_user_id) WHERE admin_user_id IS NOT NULL;

-- +goose Down
DROP INDEX iam.ix_session_admin_user_id;
ALTER TABLE iam.session DROP CONSTRAINT ck_session_owner;
DELETE FROM iam.session WHERE admin_user_id IS NOT NULL;
ALTER TABLE iam.session ALTER COLUMN account_id SET NOT NULL;
ALTER TABLE iam.session DROP COLUMN admin_user_id;
