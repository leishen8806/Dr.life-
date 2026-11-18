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

// FulfillmentEventApplier is implemented by the trade module. Keeping the
// transaction callback here makes the inbox receipt atomic without letting
// logistics query or mutate trade-owned tables.
type FulfillmentEventApplier func(context.Context, pgx.Tx, FulfillmentUpdate, string) error

// ApplyFulfillmentUpdateTx advances the merchant order through the trade
// adapter from a verified RouteFit event.
func ApplyFulfillmentUpdateTx(ctx context.Context, tx pgx.Tx, update FulfillmentUpdate, sourceRef string, applier FulfillmentEventApplier) error {
	if tx == nil || strings.TrimSpace(update.ExternalOrderID) == "" || strings.TrimSpace(sourceRef) == "" || applier == nil {
		return errors.New("routefit fulfillment update requires transaction, source, and trade adapter")
	}
	return applier(ctx, tx, update, sourceRef)
}
