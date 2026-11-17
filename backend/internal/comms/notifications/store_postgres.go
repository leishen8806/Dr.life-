package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	platformids "github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

// PostgresDB is the small database surface needed by the notification
// repository. A pgx pool and a transaction both satisfy it where applicable.
type PostgresDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type postgresPool interface {
	PostgresDB
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// BotResolver is deliberately injected: the comms schema stores recipient
// settings, while Bot credentials/configuration belong to the integration
// boundary and must not be copied into notification rows.
type BotResolver func(context.Context, int64, End) ([]Bot, error)

// PostgresRepository persists notification outbox rows and in-app records.
// It is safe for the worker to process buyer and merchant rows for one event
// independently because every mutation is keyed by the outbox row id.
type PostgresRepository struct {
	DB          postgresPool
	ResolveBots BotResolver
}

func NewPostgresRepository(db postgresPool, resolveBots BotResolver) *PostgresRepository {
	return &PostgresRepository{DB: db, ResolveBots: resolveBots}
}

// EnqueueTx is the business-transaction writer. Callers pass their existing
// transaction so a committed domain change and its notification hand-off are
// atomic; Telegram is never called from this method.
func EnqueueTx(ctx context.Context, tx pgx.Tx, event Event) error {
	if err := event.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("notifications: encode event: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO comms.notification_outbox
			(event_id, account_id, end_name, event_type, payload)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (event_id, account_id, end_name) DO NOTHING`,
		event.ID, event.AccountID, string(event.End), string(event.Type), payload)
	return err
}

func (r *PostgresRepository) PutOnce(ctx context.Context, event Event) error {
	if r == nil || r.DB == nil {
		return errors.New("notifications: database is required")
	}
	if err := event.Validate(); err != nil {
		return err
	}
	id, err := platformids.NewULID()
	if err != nil {
		return err
	}
	title, err := json.Marshal(event.Title)
	if err != nil {
		return fmt.Errorf("notifications: encode title: %w", err)
	}
	body, err := json.Marshal(event.Body)
	if err != nil {
		return fmt.Errorf("notifications: encode body: %w", err)
	}
	_, err = r.DB.Exec(ctx, `
		INSERT INTO comms.notification (public_id, event_id, account_id, event_type, title, body)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (account_id, event_id) DO NOTHING`,
		id, event.ID, event.AccountID, string(event.Type), title, body)
	return err
}

func (r *PostgresRepository) ClaimPending(ctx context.Context) (OutboxItem, bool, error) {
	if r == nil || r.DB == nil {
		return OutboxItem{}, false, errors.New("notifications: database is required")
	}
	tx, err := r.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return OutboxItem{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var (
		item       OutboxItem
		payload    []byte
		endName    string
		telegramOn bool
	)
	err = tx.QueryRow(ctx, `
		SELECT o.id, o.event_id, o.account_id, o.end_name, o.payload, o.attempts,
		       COALESCE(s.telegram_enabled, true)
		FROM comms.notification_outbox o
		LEFT JOIN comms.notification_setting s
		  ON s.account_id = o.account_id AND s.event_type = o.event_type
		WHERE o.delivered_at IS NULL AND o.dead_lettered_at IS NULL
		  AND o.next_attempt_at <= clock_timestamp()
		ORDER BY o.next_attempt_at, o.id
		FOR UPDATE OF o SKIP LOCKED
		LIMIT 1`).Scan(&item.ID, &item.Event.ID, &item.Event.AccountID, &endName, &payload, &item.Attempts, &telegramOn)
	if errors.Is(err, pgx.ErrNoRows) {
		return OutboxItem{}, false, nil
	}
	if err != nil {
		return OutboxItem{}, false, err
	}
	if err := json.Unmarshal(payload, &item.Event); err != nil {
		return OutboxItem{}, false, fmt.Errorf("notifications: decode event: %w", err)
	}
	item.Event.End = End(endName)
	item.TelegramEnabled = telegramOn
	if r.ResolveBots != nil {
		item.Bots, err = r.ResolveBots(ctx, item.Event.AccountID, item.Event.End)
		if err != nil {
			return OutboxItem{}, false, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE comms.notification_outbox SET attempts = attempts + 1 WHERE id = $1`, item.ID); err != nil {
		return OutboxItem{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OutboxItem{}, false, err
	}
	return item, true, nil
}

func (r *PostgresRepository) MarkDelivered(ctx context.Context, item OutboxItem) error {
	if r == nil || r.DB == nil {
		return errors.New("notifications: database is required")
	}
	if item.ID <= 0 {
		return errors.New("notifications: outbox id is required")
	}
	_, err := r.DB.Exec(ctx, `UPDATE comms.notification_outbox SET delivered_at = clock_timestamp() WHERE id = $1 AND delivered_at IS NULL`, item.ID)
	return err
}

func (r *PostgresRepository) MarkRetry(ctx context.Context, item OutboxItem, next time.Time, lastError string, deadLetter bool) error {
	if r == nil || r.DB == nil {
		return errors.New("notifications: database is required")
	}
	if item.ID <= 0 {
		return errors.New("notifications: outbox id is required")
	}
	var deadAt any
	if deadLetter {
		deadAt = time.Now().UTC()
	}
	_, err := r.DB.Exec(ctx, `
		UPDATE comms.notification_outbox
		SET next_attempt_at = $2, last_error = NULLIF($3, ''), dead_lettered_at = $4
		WHERE id = $1 AND delivered_at IS NULL`, item.ID, next, lastError, deadAt)
	return err
}
