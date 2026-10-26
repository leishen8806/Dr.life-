-- +goose Up
CREATE TABLE ops.processed_event (
    consumer text NOT NULL,
    event_id text NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (consumer, event_id)
);

CREATE INDEX processed_event_processed_at_idx ON ops.processed_event (processed_at);

GRANT SELECT, INSERT, DELETE ON ops.processed_event TO ops_rw;
GRANT SELECT ON ops.processed_event TO report_ro;

-- +goose Down
REVOKE SELECT ON ops.processed_event FROM report_ro;
REVOKE SELECT, INSERT, DELETE ON ops.processed_event FROM ops_rw;
DROP TABLE ops.processed_event;
