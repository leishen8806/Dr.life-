package kess

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/trade"
)

type dueStore interface {
	ListDue(context.Context, time.Time, int) ([]Intent, error)
	Intent(context.Context, int64) (Intent, error)
}

type paymentConfirmer interface {
	ConfirmPayment(context.Context, string, int64, trade.EventSource) (bool, error)
}

// Maintenance scans persisted intents, confirms KESS facts, then applies a
// confirmed payment through the trade service's idempotent payment path.
type Maintenance struct {
	store     dueStore
	worker    *Worker
	confirmer paymentConfirmer
	now       func() time.Time
}

func NewMaintenance(store dueStore, worker *Worker, confirmer paymentConfirmer) (*Maintenance, error) {
	if store == nil || worker == nil || confirmer == nil {
		return nil, errors.New("kess maintenance dependencies are required")
	}
	return &Maintenance{store: store, worker: worker, confirmer: confirmer, now: time.Now}, nil
}

func (m *Maintenance) ProcessDue(ctx context.Context, limit int) error {
	intents, err := m.store.ListDue(ctx, m.now(), limit)
	if err != nil {
		return err
	}
	var result error
	for _, due := range intents {
		if due.Status == StatusPending {
			err = m.worker.Create(ctx, due.ID)
		} else if due.Status != StatusSuccess {
			err = m.worker.RecoverUnknown(ctx, due.ID)
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("payment intent %d: %w", due.ID, err))
			continue
		}
		intent, loadErr := m.store.Intent(ctx, due.ID)
		if loadErr != nil {
			result = errors.Join(result, loadErr)
			continue
		}
		if intent.Status != StatusSuccess {
			continue
		}
		if intent.ChannelTransactionID == "" {
			result = errors.Join(result, fmt.Errorf("payment intent %d: missing KESS transaction id", intent.ID))
			continue
		}
		_, confirmErr := m.confirmer.ConfirmPayment(ctx, intent.SourceRef, intent.AmountE4, trade.EventSource{Type: trade.SourceKESSResult, Reference: intent.ChannelTransactionID})
		if errors.Is(confirmErr, trade.ErrReservationReleased) || errors.Is(confirmErr, trade.ErrPaymentReconciliation) {
			slog.WarnContext(ctx, "KESS payment requires manual reconciliation", "payment_intent_id", intent.ID)
			continue
		}
		result = errors.Join(result, confirmErr)
	}
	return result
}
