package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Event struct {
	EventID   string          `json:"event_id"`
	Time      time.Time       `json:"time"`
	ActorID   int64           `json:"actor_id"`
	ActorRole string          `json:"actor_role"`
	Action    Action          `json:"action"`
	Target    string          `json:"target"`
	Before    json.RawMessage `json:"before,omitempty"`
	After     json.RawMessage `json:"after,omitempty"`
	IP        string          `json:"ip,omitempty"`
	RequestID string          `json:"request_id"`
}

type ConsumeResult struct {
	Record   Record
	Replayed bool
}

type Consumer struct{ Store *Store }

func (c Consumer) Consume(ctx context.Context, event Event) (ConsumeResult, error) {
	if c.Store == nil || c.Store.DB == nil || event.EventID == "" {
		return ConsumeResult{}, errors.New("audit consumer is not configured or event ID is empty")
	}
	tx, err := c.Store.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ConsumeResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if record, found, err := consumedRecord(ctx, tx, event.EventID); err != nil {
		return ConsumeResult{}, err
	} else if found {
		return ConsumeResult{Record: record, Replayed: true}, tx.Commit(ctx)
	}
	detail, err := json.Marshal(map[string]json.RawMessage{"before": defaultJSON(event.Before), "after": defaultJSON(event.After)})
	if err != nil {
		return ConsumeResult{}, err
	}
	record, err := c.Store.appendTx(ctx, tx, Entry{Time: event.Time, ActorID: event.ActorID, ActorRole: event.ActorRole, Action: event.Action, Target: event.Target, Detail: detail, IP: event.IP, RequestID: event.RequestID})
	if err != nil {
		return ConsumeResult{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit.event_seen (event_id, log_id) VALUES ($1,$2)`, event.EventID, record.ID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			_ = tx.Rollback(ctx)
			return c.replay(ctx, event.EventID)
		}
		return ConsumeResult{}, fmt.Errorf("mark audit event consumed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ConsumeResult{}, err
	}
	c.Store.recordAppend(true)
	return ConsumeResult{Record: record}, nil
}

func (c Consumer) replay(ctx context.Context, eventID string) (ConsumeResult, error) {
	tx, err := c.Store.DB.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return ConsumeResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, found, err := consumedRecord(ctx, tx, eventID)
	if err != nil || !found {
		if err == nil {
			err = errors.New("concurrent audit event was not found")
		}
		return ConsumeResult{}, err
	}
	return ConsumeResult{Record: record, Replayed: true}, tx.Commit(ctx)
}

func consumedRecord(ctx context.Context, tx pgx.Tx, eventID string) (Record, bool, error) {
	var record Record
	err := tx.QueryRow(ctx, `SELECT l.id,l.ts,l.actor_id,l.actor_role,l.action,l.target,l.detail,coalesce(host(l.ip),''),l.request_id,l.prev_hash,l.hash FROM audit.event_seen e JOIN audit.log l ON l.id=e.log_id WHERE e.event_id=$1`, eventID).
		Scan(&record.ID, &record.Time, &record.ActorID, &record.ActorRole, &record.Action, &record.Target, &record.Detail, &record.IP, &record.RequestID, &record.PrevHash, &record.Hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, false, nil
	}
	return record, err == nil, err
}

func defaultJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}
