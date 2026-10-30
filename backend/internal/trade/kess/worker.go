package kess

import (
	"context"
	"errors"
	"net"
)

type Client interface {
	Create(context.Context, Intent) (ChannelOrder, error)
	Query(context.Context, Intent) (ChannelOrder, error)
}

type Worker struct {
	store  Store
	client Client
}

func NewWorker(store Store, client Client) *Worker { return &Worker{store: store, client: client} }

// Create runs outside the intent persistence transaction. A timeout deliberately
// keeps the original trade number and defers recovery to Query.
func (w *Worker) Create(ctx context.Context, id int64) error {
	intent, err := w.store.Intent(ctx, id)
	if err != nil {
		return err
	}
	if intent.Status != StatusPending {
		return nil
	}
	if err := w.store.RecordCreateAttempt(ctx, id); err != nil {
		return err
	}
	order, err := w.client.Create(ctx, intent)
	if err != nil {
		if isTimeout(err) {
			if recordErr := w.store.RecordChannelResult(ctx, id, ChannelResult{Status: StatusUnknown}); recordErr != nil {
				return recordErr
			}
		}
		return err
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
		return err
	}
	return w.recordValidated(ctx, intent, order, false)
}

func (w *Worker) recordValidated(ctx context.Context, intent Intent, order ChannelOrder, requireURL bool) error {
	status, err := validateOrder(intent, order, requireURL)
	if err != nil {
		return err
	}
	return w.store.RecordChannelResult(ctx, intent.ID, ChannelResult{
		Status: status, ChannelStatus: order.Status, TransactionID: order.TransactionID, PaymentURL: order.PaymentURL,
	})
}

func validateOrder(intent Intent, order ChannelOrder, requireURL bool) (Status, error) {
	if order.TradeNo != intent.TradeNo || order.SellerCode != intent.ExpectedSellerCode || order.Amount != intent.Amount || order.Currency != intent.Currency {
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
	case "FAILED":
		return StatusFailed, nil
	case "CLOSED":
		return StatusClosed, nil
	case "REFUND", "REFUNDING", "REFUNDED", "PENDING_REFUNDED", "PARTIAL_REFUNDED":
		return StatusRefund, nil
	default:
		return "", ErrInvalidChannel
	}
}

func isTimeout(err error) bool {
	var networkErr net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkErr) && networkErr.Timeout()
}
