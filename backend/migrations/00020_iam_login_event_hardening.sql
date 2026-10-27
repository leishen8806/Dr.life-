-- +goose Up
ALTER TABLE iam.login_event
    DROP CONSTRAINT ck_login_event_result,
    ADD CONSTRAINT ck_login_event_result CHECK (result IN (
        'ok', 'invalid_signature', 'expired', 'malformed',
        'account_blocked', 'rate_limited', 'internal_error', 'ambiguous_account'
    ));
REVOKE UPDATE, DELETE, TRUNCATE, TRIGGER, REFERENCES ON iam.login_event FROM iam_rw;

-- +goose Down
GRANT UPDATE, DELETE, TRUNCATE, TRIGGER, REFERENCES ON iam.login_event TO iam_rw;
UPDATE iam.login_event SET result='internal_error' WHERE result='ambiguous_account';
ALTER TABLE iam.login_event
    DROP CONSTRAINT ck_login_event_result,
    ADD CONSTRAINT ck_login_event_result CHECK (result IN (
        'ok', 'invalid_signature', 'expired', 'malformed',
        'account_blocked', 'rate_limited', 'internal_error'
    ));
