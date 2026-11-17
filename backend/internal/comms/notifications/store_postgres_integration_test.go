package notifications

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPostgresRepositoryKeepsBuyerAndMerchantRecipientsIndependent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("notifications_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	_, err = db.Exec(ctx, `
		CREATE SCHEMA iam;
		CREATE SCHEMA comms;
		CREATE TABLE iam.account (id bigint PRIMARY KEY);
		INSERT INTO iam.account(id) VALUES (7), (8);
		CREATE TABLE comms.notification (
			id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			public_id text NOT NULL UNIQUE,
			event_id text NOT NULL,
			account_id bigint NOT NULL REFERENCES iam.account(id),
			event_type text NOT NULL,
			title jsonb NOT NULL,
			body jsonb NOT NULL,
			read_at timestamptz,
			created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
			UNIQUE (account_id, event_id)
		);
		CREATE TABLE comms.notification_setting (
			account_id bigint NOT NULL REFERENCES iam.account(id),
			event_type text NOT NULL,
			telegram_enabled boolean NOT NULL DEFAULT true,
			PRIMARY KEY (account_id, event_type)
		);
		CREATE TABLE comms.notification_outbox (
			id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			event_id text NOT NULL,
			account_id bigint NOT NULL REFERENCES iam.account(id),
			end_name text NOT NULL,
			event_type text NOT NULL,
			payload jsonb NOT NULL,
			attempts integer NOT NULL DEFAULT 0,
			next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
			last_error text,
			dead_lettered_at timestamptz,
			delivered_at timestamptz,
			created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
			UNIQUE (event_id, account_id, end_name)
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresRepository(db, func(_ context.Context, accountID int64, end End) ([]Bot, error) {
		return []Bot{{ID: accountID + 100, End: end, Active: true}}, nil
	})
	event := Event{ID: "order-1", AccountID: 7, End: Buyer, Type: OrderCreated, Title: map[string]string{"en": "Order"}, Body: map[string]string{"en": "Created"}}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnqueueTx(ctx, tx, event); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	merchantEvent := event
	merchantEvent.AccountID = 8
	merchantEvent.End = Merchant
	if err := EnqueueTx(ctx, tx, merchantEvent); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.PutOnce(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := repo.PutOnce(ctx, event); err != nil {
		t.Fatal(err)
	}
	var notifications int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM comms.notification`).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 {
		t.Fatalf("in-app notifications=%d, want idempotent 1", notifications)
	}
	first, ok, err := repo.ClaimPending(ctx)
	if err != nil || !ok || first.Event.End != Buyer || first.Bots[0].End != Buyer {
		t.Fatalf("first claim=%+v ok=%v err=%v", first, ok, err)
	}
	if err := repo.MarkDelivered(ctx, first); err != nil {
		t.Fatal(err)
	}
	second, ok, err := repo.ClaimPending(ctx)
	if err != nil || !ok || second.Event.End != Merchant || second.Bots[0].End != Merchant {
		t.Fatalf("second claim=%+v ok=%v err=%v", second, ok, err)
	}
	if first.ID == second.ID {
		t.Fatalf("buyer and merchant claims shared outbox id %d", first.ID)
	}
}
