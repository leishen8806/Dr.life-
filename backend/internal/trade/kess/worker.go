package kess

import (
	"context"
	"errors"
	"fmt"
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
	paymentURLHost string
}

func NewWorker(store Store, client Client, recoveryWindow time.Duration, paymentURLHost string) (*Worker, error) {
	if recoveryWindow <= 0 || !validPaymentHost(paymentURLHost) {
		return nil, fmt.Errorf("invalid KESS worker configuration: %w", ErrConfig)
	}
	return &Worker{store: store, client: client, now: time.Now, recoveryWindow: recoveryWindow, paymentURLHost: paymentURLHost}, nil
}

// Create runs outside the intent persistence transaction. A timeout deliberately
// keeps the original trade number and defers recovery to Query.
func (w *Worker) Create(ctx context.Context, id int64) error {
	intent, claimed, err := w.store.ClaimPending(ctx, id)
	if err != nil {
		return err
	}
	if !claimed {
		if intent.ID != 0 && intent.Status == StatusUnknown {
			return w.RecoverUnknown(ctx, id)
		}
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
		if recordErr := w.store.RecordChannelResult(ctx, id, StatusPending, ChannelResult{Status: StatusUnknown, CreateSent: true}); recordErr != nil {
			return recordErr
		}
		return err
	}
	if order.Status != "WAITING" {
		return w.store.RecordChannelResult(ctx, id, StatusPending, ChannelResult{Status: StatusUnknown, ChannelStatus: order.Status, TransactionID: order.TransactionID, CreateSent: true})
	}
	return w.recordValidated(ctx, intent, order, true, true)
}

// RecoverUnknown queries exactly the persisted trade number; it never creates a
// replacement payment order.
func (w *Worker) RecoverUnknown(ctx context.Context, id int64) error {
	intent, err := w.store.Intent(ctx, id)
	if err != nil {
		return err
	}
	if intent.Status != StatusUnknown && intent.Status != StatusWaiting {
		return nil
	}
	order, err := w.client.Query(ctx, intent)
	if err != nil {
		if errors.Is(err, ErrChannelMissing) && intent.LastCreateSentAt != nil && w.now().Sub(*intent.LastCreateSentAt) >= w.recoveryWindow {
			return w.store.RecordChannelResult(ctx, id, intent.Status, ChannelResult{Status: StatusClosed, ChannelStatus: "NOT_FOUND"})
		}
		return err
	}
	return w.recordValidated(ctx, intent, order, false, false)
}

func (w *Worker) recordValidated(ctx context.Context, intent Intent, order ChannelOrder, requireURL, createSent bool) error {
	status, err := validateOrder(intent, order, requireURL, w.paymentURLHost)
	if err != nil {
		return err
	}
	return w.store.RecordChannelResult(ctx, intent.ID, intent.Status, ChannelResult{
		Status: status, ChannelStatus: order.Status, TransactionID: order.TransactionID, PaymentURL: order.PaymentURL, CreateSent: createSent,
	})
}

func validateOrder(intent Intent, order ChannelOrder, requireURL bool, paymentURLHost string) (Status, error) {
	if order.TradeNo != intent.TradeNo || order.SellerCode != intent.ExpectedSellerCode || order.AmountE4 != intent.AmountE4 || order.Currency != intent.Currency {
		return "", ErrInvalidChannel
	}
	switch order.Status {
	case "WAITING":
		if requireURL && !validPaymentURL(order.PaymentURL, paymentURLHost) {
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
