package logistics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/logistics/routefit"
)

type fulfillmentOutboxRow struct {
	ID              int64
	ExternalOrderID string
	Payload         []byte
	Attempts        int
}

// FulfillmentOutboxWorker drains the durable command table without holding a
// database transaction across the external RouteFit request.
type FulfillmentOutboxWorker struct {
	DB          *pgxpool.Pool
	Client      *routefit.Client
	BatchSize   int
	Lease       time.Duration
	MaxAttempts int
	RetryDelay  time.Duration
	Now         func() time.Time
}

func (w FulfillmentOutboxWorker) RunOnce(ctx context.Context) error {
	if w.DB == nil || w.Client == nil {
		return errors.New("RouteFit fulfillment worker is not configured")
	}
	batchSize := w.BatchSize
	if batchSize <= 0 {
		batchSize = 20
	}
	lease := w.Lease
	if lease <= 0 {
		lease = 15 * time.Minute
	}
	maxAttempts := w.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	now := time.Now().UTC()
	if w.Now != nil {
		now = w.Now().UTC()
	}
	tx, err := w.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE logistics.fulfillment_outbox
		SET status='retry', next_attempt_at=$2, updated_at=clock_timestamp()
		WHERE status='processing' AND updated_at < clock_timestamp() - ($1 * interval '1 second')`, lease.Seconds(), now); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE logistics.fulfillment_outbox
		SET status='exception', last_error_code='MAX_ATTEMPTS_EXCEEDED', updated_at=clock_timestamp()
		WHERE status IN ('pending','retry') AND attempts >= $1`, maxAttempts); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	rows, err := tx.Query(ctx, `
		SELECT id, external_order_id, payload, attempts
		FROM logistics.fulfillment_outbox
		WHERE status IN ('pending', 'retry') AND next_attempt_at <= $1
		ORDER BY id
		FOR UPDATE SKIP LOCKED
		LIMIT $2`, now, batchSize)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	var batch []fulfillmentOutboxRow
	for rows.Next() {
		var row fulfillmentOutboxRow
		if err := rows.Scan(&row.ID, &row.ExternalOrderID, &row.Payload, &row.Attempts); err != nil {
			rows.Close()
			_ = tx.Rollback(ctx)
			return err
		}
		batch = append(batch, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		_ = tx.Rollback(ctx)
		return err
	}
	rows.Close()
	for _, row := range batch {
		if _, err := tx.Exec(ctx, `UPDATE logistics.fulfillment_outbox SET status='processing', attempts=attempts+1, updated_at=clock_timestamp() WHERE id=$1`, row.ID); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	for _, row := range batch {
		var command routefit.CreateShipmentCmd
		if err := json.Unmarshal(row.Payload, &command); err != nil {
			if updateErr := w.finish(ctx, row.ID, DeliveryException, "INVALID_PAYLOAD", ""); updateErr != nil {
				return updateErr
			}
			continue
		}
		result, deliveryErr := w.Client.Deliver(ctx, command)
		action := ClassifyDeliveryError(deliveryErr)
		code := ""
		if deliveryErr != nil {
			var upstream *routefit.Error
			if errors.As(deliveryErr, &upstream) {
				code = upstream.Code
			} else {
				code = "DELIVERY_FAILED"
			}
		}
		if deliveryErr != nil && row.Attempts+1 >= maxAttempts {
			action = DeliveryException
			code = "MAX_ATTEMPTS_EXCEEDED"
		}
		if err := w.finish(ctx, row.ID, action, code, result.FulfillmentID); err != nil {
			return err
		}
	}
	return nil
}

func (w FulfillmentOutboxWorker) finish(ctx context.Context, id int64, action DeliveryAction, errorCode, fulfillmentID string) error {
	status := string(action)
	if action == DeliveryRetry {
		status = "retry"
	}
	if action == DeliveryException {
		status = "exception"
	}
	if action == DeliveryDelivered {
		status = "delivered"
	}
	if strings.TrimSpace(status) == "" {
		return fmt.Errorf("invalid RouteFit delivery action for outbox %d", id)
	}
	next := time.Now().UTC()
	if w.Now != nil {
		next = w.Now().UTC()
	}
	if action == DeliveryRetry {
		delay := w.RetryDelay
		if delay <= 0 {
			delay = 30 * time.Second
		}
		next = next.Add(delay)
	}
	_, err := w.DB.Exec(ctx, `UPDATE logistics.fulfillment_outbox SET status=$1, last_error_code=NULLIF($2,''), fulfillment_id=NULLIF($3,''), next_attempt_at=$4, updated_at=clock_timestamp() WHERE id=$5`, status, errorCode, fulfillmentID, next, id)
	return err
}
