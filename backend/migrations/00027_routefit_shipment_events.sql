-- +goose Up
CREATE TABLE logistics.shipment_event (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id text NOT NULL UNIQUE,
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    aggregate_version bigint NOT NULL CHECK (aggregate_version >= 0),
    schema_version integer NOT NULL CHECK (schema_version > 0),
    occurred_at timestamptz NOT NULL,
    correlation_id text,
    body_sha256 bytea NOT NULL CHECK (octet_length(body_sha256) = 32),
    payload jsonb NOT NULL,
    processing_state text NOT NULL CHECK (processing_state IN ('accepted', 'stale')),
    received_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX shipment_event_aggregate_version_idx ON logistics.shipment_event (aggregate_id, aggregate_version DESC);
CREATE OR REPLACE FUNCTION logistics.no_shipment_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'logistics.shipment_event is append-only'; END; $$;
CREATE TRIGGER shipment_event_append_only BEFORE UPDATE OR DELETE ON logistics.shipment_event FOR EACH ROW EXECUTE FUNCTION logistics.no_shipment_event_mutation();
GRANT SELECT, INSERT ON logistics.shipment_event TO logistics_rw;
GRANT SELECT ON logistics.shipment_event TO report_ro;

-- +goose Down
REVOKE SELECT ON logistics.shipment_event FROM report_ro;
REVOKE SELECT, INSERT ON logistics.shipment_event FROM logistics_rw;
DROP TRIGGER shipment_event_append_only ON logistics.shipment_event;
DROP FUNCTION logistics.no_shipment_event_mutation();
DROP TABLE logistics.shipment_event;
