package notifications

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/comms/telegram"
	iamtelegram "github.com/leishen8806/Dr.life-/backend/internal/iam/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestTelegramWorkerLoadsEncryptedPoolAndSendsThroughLocalBot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("telegram_notifications_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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

	key := make([]byte, secrets.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.NewKeyring(map[string][]byte{"test-key": key}, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := secrets.NewEnvelope(ring)
	if err != nil {
		t.Fatal(err)
	}
	const botID int64 = 101
	const token = "buyer-test-token"
	encrypted, err := envelope.Encrypt(ctx, []byte(token), "bot_token:101")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(ctx, `
		CREATE SCHEMA iam;
		CREATE SCHEMA comms;
		CREATE TABLE iam.bot (id bigint PRIMARY KEY, "end" text NOT NULL, role_in_pool text NOT NULL, priority integer NOT NULL, token_enc bytea NOT NULL);
		CREATE TABLE comms.notification (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, public_id text NOT NULL UNIQUE, event_id text NOT NULL, account_id bigint NOT NULL, event_type text NOT NULL, title jsonb NOT NULL, body jsonb NOT NULL, read_at timestamptz, created_at timestamptz NOT NULL DEFAULT clock_timestamp(), UNIQUE (account_id, event_id));
		CREATE TABLE comms.notification_setting (account_id bigint NOT NULL, event_type text NOT NULL, telegram_enabled boolean NOT NULL DEFAULT true, PRIMARY KEY (account_id, event_type));
		CREATE TABLE comms.notification_outbox (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, event_id text NOT NULL, account_id bigint NOT NULL, end_name text NOT NULL, event_type text NOT NULL, payload jsonb NOT NULL, attempts integer NOT NULL DEFAULT 0, next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(), last_error text, dead_lettered_at timestamptz, delivered_at timestamptz, created_at timestamptz NOT NULL DEFAULT clock_timestamp(), UNIQUE (event_id, account_id, end_name));
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(ctx, `INSERT INTO iam.bot(id, "end", role_in_pool, priority, token_enc) VALUES (101, 'buyer', 'primary', 0, $1), (202, 'merchant', 'primary', 0, $2)`, encrypted, encrypted)
	if err != nil {
		t.Fatal(err)
	}

	var gotPath, gotText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body struct {
			ChatID int64  `json:"chat_id"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode Telegram request: %v", err)
		}
		if body.ChatID != 7001 {
			t.Errorf("chat id=%d, want 7001", body.ChatID)
		}
		gotText = body.Text
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer server.Close()

	source := &iamtelegram.DBPoolSource{DB: db, Envelope: envelope, TTL: time.Minute}
	provider := iamtelegram.PoolTokenProvider{Source: source, End: iamtelegram.Buyer}
	sender := telegram.NewHTTPSender(server.URL, provider, telegram.SenderOptions{Timeout: time.Second})
	repo := NewPostgresRepository(db, func(ctx context.Context, _ int64, end End) ([]Bot, error) {
		bots, err := source.Bots(ctx, iamtelegram.End(end))
		if err != nil {
			return nil, err
		}
		result := make([]Bot, 0, len(bots))
		for _, bot := range bots {
			result = append(result, Bot{ID: bot.ID, End: End(end), Active: true})
		}
		return result, nil
	})
	event := Event{ID: "order-telegram-1", AccountID: 7, End: Buyer, Type: OrderCreated, Title: map[string]string{"en": "Order"}, Body: map[string]string{"en": "Created"}}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnqueueTx(ctx, tx, event); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	worker := Worker{Repo: repo, Inbox: repo, Telegram: TelegramDeliveryWriter{Sender: sender, ChatID: func(context.Context, int64) (int64, error) { return 7001, nil }, DeepLink: func(Event) string { return "https://t.me/buyer_bot?startapp=order-telegram-1" }}}
	processed, err := worker.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("worker processed=%v err=%v", processed, err)
	}
	if gotPath != "/bot"+token+"/sendMessage" || !strings.Contains(gotText, "https://t.me/buyer_bot?startapp=order-telegram-1") {
		t.Fatalf("Telegram request path=%q text=%q", gotPath, gotText)
	}
	var delivered bool
	if err := db.QueryRow(ctx, `SELECT delivered_at IS NOT NULL FROM comms.notification_outbox WHERE event_id=$1`, event.ID).Scan(&delivered); err != nil || !delivered {
		t.Fatalf("delivered=%v err=%v", delivered, err)
	}
}
