package kess

import (
	"context"
	"errors"
	"time"
)

type Client interface {
	Create(context.Context, Intent) (ChannelOrder, error)
	Query(context.Context, Intent) (ChannelOrder, error)
}

type Worker struct {
	store          Store
	client         Client
	now            func() time.Time
	recoveryWindow time.Duration
}

func NewWorker(store Store, client Client, recoveryWindow time.Duration) *Worker {
	return &Worker{store: store, client: client, now: time.Now, recoveryWindow: recoveryWindow}
}

// Create runs outside the intent persistence transaction. A timeout deliberately
// keeps the original trade number and defers recovery to Query.
func (w *Worker) Create(ctx context.Context, id int64) error {
	intent, claimed, err := w.store.ClaimPending(ctx, id)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	order, err := w.client.Create(ctx, intent)
	if err != nil {
		if definitelyNotSent(err) {
			if releaseErr := w.store.ReleasePendingClaim(ctx, id); releaseErr != nil {
				return releaseErr
			}
			return err
		}
		if recordErr := w.store.RecordChannelResult(ctx, id, StatusPending, ChannelResult{Status: StatusUnknown}); recordErr != nil {
			return recordErr
		}
		return err
	}
	if order.Status != "WAITING" {
		return w.store.RecordChannelResult(ctx, id, StatusPending, ChannelResult{Status: StatusUnknown, ChannelStatus: order.Status, TransactionID: order.TransactionID})
	}
	return w.recordValidated(ctx, intent, order, true)
}

// RecoverUnknown queries exactly the persisted trade number; it never creates a
// replacement payment order.
func (w *Worker) RecoverUnknown(ctx context.Context, id int64) error {
	intent, err := w.store.Intent(ctx, id)
	if err != nil {
		return err
	}
	if intent.Status != StatusUnknown {
		return nil
	}
	order, err := w.client.Query(ctx, intent)
	if err != nil {
		if errors.Is(err, ErrChannelMissing) && !intent.CreatedAt.IsZero() && w.now().Sub(intent.CreatedAt) >= w.recoveryWindow {
			return w.store.RecordChannelResult(ctx, id, StatusUnknown, ChannelResult{Status: StatusClosed, ChannelStatus: "NOT_FOUND"})
		}
		return err
	}
	return w.recordValidated(ctx, intent, order, false)
}

func (w *Worker) recordValidated(ctx context.Context, intent Intent, order ChannelOrder, requireURL bool) error {
	status, err := validateOrder(intent, order, requireURL)
	if err != nil {
		return err
	}
	return w.store.RecordChannelResult(ctx, intent.ID, intent.Status, ChannelResult{
		Status: status, ChannelStatus: order.Status, TransactionID: order.TransactionID, PaymentURL: order.PaymentURL,
	})
}

func validateOrder(intent Intent, order ChannelOrder, requireURL bool) (Status, error) {
	if order.TradeNo != intent.TradeNo || order.SellerCode != intent.ExpectedSellerCode || order.AmountE4 != intent.AmountE4 || order.Currency != intent.Currency {
		return "", ErrInvalidChannel
	}
	switch order.Status {
	case "WAITING":
		if requireURL && !validSandboxPayURL(order.PaymentURL) {
			return "", ErrInvalidChannel
		}
		return StatusWaiting, nil
	case "SUCCESS":
		if order.TransactionID == "" {
			return "", ErrInvalidChannel
		}
		return StatusSuccess, nil
	case "CLOSED":
		return StatusClosed, nil
	default:
		return "", ErrInvalidChannel
	}
}
