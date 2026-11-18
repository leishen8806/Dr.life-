package logistics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/logistics/routefit"
)

// FulfillmentOutboxEntry is the durable command handed to the RouteFit worker.
// The payload is the exact command used by routefit.Client.Deliver.
type FulfillmentOutboxEntry struct {
	ID              int64
	ExternalOrderID string
	IdempotencyKey  string
	Payload         routefit.CreateShipmentCmd
	Attempts        int
	Status          string
}

// EnqueueFulfillmentTx records the delivery command in the caller's order
// transaction. A retry of the same order is a no-op through the stable key.
func EnqueueFulfillmentTx(ctx context.Context, tx pgx.Tx, command routefit.CreateShipmentCmd) error {
	if tx == nil {
		return errors.New("routefit fulfillment transaction is required")
	}
	if strings.TrimSpace(command.ExternalOrderID) == "" {
		return errors.New("routefit external order id is required")
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return fmt.Errorf("encode RouteFit fulfillment outbox: %w", err)
	}
	key := fulfillmentIdempotencyKey(command.ExternalOrderID)
	_, err = tx.Exec(ctx, `
		INSERT INTO logistics.fulfillment_outbox
			(external_order_id, idempotency_key, payload)
		VALUES ($1, $2, $3)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		command.ExternalOrderID, key, payload)
	return err
}

func fulfillmentIdempotencyKey(externalOrderID string) string {
	sum := sha256.Sum256([]byte("routefit.fulfillment:v1:" + externalOrderID))
	return hex.EncodeToString(sum[:])
}

// DeliveryAction tells the outbox worker whether an error should be retried,
// turned into an order exception, or treated as a successful delivery.
type DeliveryAction string

const (
	DeliveryRetry     DeliveryAction = "retry"
	DeliveryException DeliveryAction = "exception"
	DeliveryDelivered DeliveryAction = "delivered"
)

func ClassifyDeliveryError(err error) DeliveryAction {
	if err == nil {
		return DeliveryDelivered
	}
	var upstream *routefit.Error
	if !errors.As(err, &upstream) {
		if errors.Is(err, context.DeadlineExceeded) {
			return DeliveryRetry
		}
		return DeliveryException
	}
	if upstream.Status == http.StatusLocked || upstream.Code == "OWNER_OUTBOUND_HELD" || upstream.Code == "INSUFFICIENT_STOCK" || upstream.Code == "SKU_UNAVAILABLE" {
		return DeliveryException
	}
	if upstream.Status == http.StatusTooManyRequests || upstream.Status >= 500 {
		return DeliveryRetry
	}
	return DeliveryException
}

// ApplyFulfillmentUpdateTx advances the merchant order from a verified
// RouteFit event. The source row and transition event are written before the
// status update so the existing order trigger can enforce allowed transitions.
func ApplyFulfillmentUpdateTx(ctx context.Context, tx pgx.Tx, update FulfillmentUpdate, sourceRef string) error {
	if tx == nil || strings.TrimSpace(update.ExternalOrderID) == "" || strings.TrimSpace(sourceRef) == "" {
		return errors.New("routefit fulfillment update requires transaction and source")
	}
	var merchantOrderID, buyerOrderID int64
	var current string
	if err := tx.QueryRow(ctx, `SELECT id, buyer_order_id, status FROM trade.merchant_order WHERE external_order_id=$1 FOR UPDATE`, update.ExternalOrderID).Scan(&merchantOrderID, &buyerOrderID, &current); err != nil {
		return err
	}
	if current == update.State {
		return nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trade.order_event_source (buyer_order_id, source_type, source_ref) VALUES ($1, 'routefit_event', $2) ON CONFLICT DO NOTHING`, buyerOrderID, sourceRef); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trade.order_event (merchant_order_id, buyer_order_id, from_status, to_status, source_type, source_ref) VALUES ($1, $2, $3, $4, 'routefit_event', $5) ON CONFLICT DO NOTHING`, merchantOrderID, buyerOrderID, current, update.State, sourceRef); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE trade.merchant_order SET status=$1, version=version+1, updated_at=clock_timestamp() WHERE id=$2 AND status=$3`, update.State, merchantOrderID, current)
	return err
}
