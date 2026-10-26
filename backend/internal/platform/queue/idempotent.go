package queue

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrInvalidIdempotentEvent = errors.New("invalid idempotent consumer or event id")

type transactionStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}

// TransactionalHandler keeps business changes in the same transaction as the
// processed-event marker. Ledger consumers use ledger idempotency keys instead.
type TransactionalHandler struct {
	DB     transactionStarter
	Handle func(context.Context, pgx.Tx, string) error
}

// Idempotent returns a consumer which records an event before applying its
// business effect. A duplicate event is a successful no-op.
func Idempotent(consumer string, handler TransactionalHandler) func(context.Context, string) error {
	return func(ctx context.Context, eventID string) error {
		if strings.TrimSpace(consumer) == "" || strings.TrimSpace(eventID) == "" || handler.DB == nil || handler.Handle == nil {
			return ErrInvalidIdempotentEvent
		}
		tx, err := handler.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var inserted string
		err = tx.QueryRow(ctx, `INSERT INTO ops.processed_event(consumer,event_id) VALUES($1,$2) ON CONFLICT DO NOTHING RETURNING event_id`, consumer, eventID).Scan(&inserted)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := handler.Handle(ctx, tx, eventID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
}

type processedEventCleaner interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// CleanupProcessedEvents retains only the required thirty-day idempotency window.
func CleanupProcessedEvents(ctx context.Context, db processedEventCleaner, now time.Time) error {
	if db == nil {
		return errors.New("processed event database is required")
	}
	_, err := db.Exec(ctx, `DELETE FROM ops.processed_event WHERE processed_at < $1`, now.AddDate(0, 0, -30))
	return err
}
