package logistics

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
)

type routeFitTransactionStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}

// RouteFitEventHandler applies a supported event type inside the same
// transaction as the inbox append and idempotency marker.
type RouteFitEventHandler func(context.Context, pgx.Tx, RouteFitEvent) error

// RouteFitEventRecorder makes the receipt durable before a webhook receives a
// 2xx response. Unknown event types intentionally remain inbox-only.
type RouteFitEventRecorder struct {
	DB       routeFitTransactionStarter
	Handlers map[string]RouteFitEventHandler
}

func (r RouteFitEventRecorder) Record(ctx context.Context, event RouteFitEvent, rawBody []byte) error {
	return queue.Idempotent("routefit", queue.TransactionalHandler{
		DB: r.DB,
		Handle: func(ctx context.Context, tx pgx.Tx, _ string) error {
			accepted, err := RecordRouteFitEvent(ctx, tx, event, rawBody)
			if err != nil || !accepted {
				return err
			}
			if handler := r.Handlers[event.EventType]; handler != nil {
				return handler(ctx, tx, event)
			}
			return nil
		},
	})(ctx, event.EventID)
}

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
