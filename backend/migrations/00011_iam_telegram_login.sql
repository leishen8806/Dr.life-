-- +goose Up
ALTER TABLE iam.login_event
    ADD COLUMN start_param text,
	DROP CONSTRAINT login_event_tried_count_check,
	ADD CONSTRAINT ck_login_event_tried_count CHECK (tried_count >= 0),
    ADD CONSTRAINT ck_login_event_result CHECK (result IN (
        'ok', 'invalid_signature', 'expired', 'malformed',
        'account_blocked', 'rate_limited', 'internal_error'
    )),
    ADD CONSTRAINT ck_login_event_start_param CHECK (
        start_param IS NULL OR (char_length(start_param) <= 64 AND start_param ~ '^[A-Za-z0-9_-]+$')
    );

CREATE UNIQUE INDEX ux_session_refresh_token_hash ON iam.session (refresh_token_hash);
CREATE INDEX ix_login_event_ip_created_at ON iam.login_event (ip, created_at DESC)
    WHERE result IN ('invalid_signature', 'expired', 'malformed', 'rate_limited');

-- +goose Down
DROP INDEX iam.ux_session_refresh_token_hash;
DROP INDEX iam.ix_login_event_ip_created_at;
ALTER TABLE iam.login_event
    DROP CONSTRAINT ck_login_event_start_param,
    DROP CONSTRAINT ck_login_event_result,
	DROP CONSTRAINT ck_login_event_tried_count,
	ADD CONSTRAINT login_event_tried_count_check CHECK (tried_count > 0),
    DROP COLUMN start_param;
