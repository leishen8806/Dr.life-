package logistics

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/jackc/pgx/v5"
)

// RecordRouteFitEvent appends every first-delivery event. An event whose
// aggregate version is not newer is retained as stale and never dispatched.
func RecordRouteFitEvent(ctx context.Context, tx pgx.Tx, event RouteFitEvent, rawBody []byte) (bool, error) {
	if tx == nil {
		return false, errors.New("routefit transaction is required")
	}
	var current int64
	err := tx.QueryRow(ctx, `SELECT aggregate_version FROM logistics.shipment_event WHERE aggregate_id = $1 ORDER BY aggregate_version DESC LIMIT 1 FOR UPDATE`, event.Aggregate.ID).Scan(&current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	state := "accepted"
	if err == nil && event.Aggregate.Version <= current {
		state = "stale"
	}
	hash := sha256.Sum256(rawBody)
	_, err = tx.Exec(ctx, `INSERT INTO logistics.shipment_event (event_id,event_type,aggregate_type,aggregate_id,aggregate_version,schema_version,occurred_at,correlation_id,body_sha256,payload,processing_state) VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10,$11)`, event.EventID, event.EventType, event.Aggregate.Type, event.Aggregate.ID, event.Aggregate.Version, event.SchemaVersion, event.OccurredAt, event.CorrelationID, hash[:], event.Payload, state)
	if err != nil {
		return false, err
	}
	return state == "accepted", nil
}
