-- +goose Up
-- Notification records are created from a domain event transaction. The outbox
-- is the durable hand-off to the worker; Telegram delivery is never part of
-- the business transaction.
CREATE TABLE comms.notification (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    event_id text NOT NULL,
    account_id bigint NOT NULL REFERENCES iam.account(id),
    event_type text NOT NULL,
    title jsonb NOT NULL,
    body jsonb NOT NULL,
    read_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (account_id, event_id)
);

CREATE INDEX ix_notification_account_created ON comms.notification (account_id, id DESC);
CREATE INDEX ix_notification_account_unread ON comms.notification (account_id, id DESC) WHERE read_at IS NULL;

CREATE TABLE comms.notification_setting (
    account_id bigint NOT NULL REFERENCES iam.account(id),
    event_type text NOT NULL,
    in_app_enabled boolean NOT NULL DEFAULT true,
    telegram_enabled boolean NOT NULL DEFAULT true,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (account_id, event_type)
);

CREATE TABLE comms.notification_outbox (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id text NOT NULL UNIQUE,
    account_id bigint NOT NULL REFERENCES iam.account(id),
    end_name text NOT NULL CHECK (end_name IN ('buyer', 'merchant')),
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    last_error text,
    dead_lettered_at timestamptz,
    delivered_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX ix_notification_outbox_pending ON comms.notification_outbox (next_attempt_at, id)
    WHERE delivered_at IS NULL AND dead_lettered_at IS NULL;

-- +goose Down
DROP TABLE comms.notification_outbox;
DROP TABLE comms.notification_setting;
DROP TABLE comms.notification;
