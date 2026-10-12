package ledger

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

type OutboxEvent struct {
	ID      int64
	TxnID   int64
	Topic   string
	Payload json.RawMessage
}

type OutboxBatch struct {
	tx     pgx.Tx
	events []OutboxEvent
	done   bool
}

func (s *Service) ClaimOutbox(ctx context.Context, limit int) (*OutboxBatch, error) {
	if limit <= 0 {
		return nil, ErrInvalidRequest
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT id,txn_id,topic,payload
		FROM ledger.outbox
		WHERE sent_at IS NULL
		ORDER BY id
		FOR UPDATE SKIP LOCKED
		LIMIT $1`, limit)
	if err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	events := make([]OutboxEvent, 0, limit)
	for rows.Next() {
		var event OutboxEvent
		if err := rows.Scan(&event.ID, &event.TxnID, &event.Topic, &event.Payload); err != nil {
			rows.Close()
			tx.Rollback(ctx)
			return nil, err
		}
		events = append(events, event)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	return &OutboxBatch{tx: tx, events: events}, nil
}

func (b *OutboxBatch) Events() []OutboxEvent {
	return append([]OutboxEvent(nil), b.events...)
}

func (b *OutboxBatch) MarkSent(ctx context.Context) error {
	if b.done {
		return ErrInvalidRequest
	}
	b.done = true
	ids := make([]int64, len(b.events))
	for i, event := range b.events {
		ids[i] = event.ID
	}
	if len(ids) > 0 {
		if _, err := b.tx.Exec(ctx, `UPDATE ledger.outbox SET sent_at=now() WHERE id=ANY($1)`, ids); err != nil {
			b.tx.Rollback(ctx)
			return err
		}
	}
	return b.tx.Commit(ctx)
}

func (b *OutboxBatch) Release(ctx context.Context) error {
	if b.done {
		return ErrInvalidRequest
	}
	b.done = true
	err := b.tx.Rollback(ctx)
	if errors.Is(err, pgx.ErrTxClosed) {
		return nil
	}
	return err
}
