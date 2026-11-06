package kess_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/leishen8806/Dr.life-/backend/internal/trade/kess"
)

func TestKESSPaymentIntentMigrationAndPersistence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("drlife_kess_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	migrations := migrationDirectory(t)
	if err := goose.Up(database, migrations); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(database, migrations, 52); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := database.QueryRow(`SELECT to_regclass('trade.kess_payment_intent') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("down migration left trade.kess_payment_intent")
	}
	if err := goose.Up(database, migrations); err != nil {
		t.Fatal(err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := kess.NewPgStore(pool)
	intent, err := store.CreateIntent(ctx, kess.ReceivableSnapshot{CompanyID: 9, Amount: 1234, Currency: "USD", SellerCode: "seller-test"})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("outbound timeout sees persisted intent first", func(t *testing.T) {
		client := persistedThenTimeout{pool: pool, intentID: intent.ID}
		err := kess.NewWorker(store, &client).Create(ctx, intent.ID)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Create error = %v, want deadline exceeded", err)
		}
		if !client.seen {
			t.Fatal("outbound client ran before the intent was persisted")
		}
		stored, err := store.Intent(ctx, intent.ID)
		if err != nil || stored.Status != kess.StatusUnknown || stored.TradeNo != intent.TradeNo {
			t.Fatalf("stored intent = %+v, %v; want UNKNOWN with same trade number", stored, err)
		}
	})

	t.Run("company channel trade uniqueness holds under concurrent inserts", func(t *testing.T) {
		const workers = 2
		start := make(chan struct{})
		errs := make(chan error, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				_, err := pool.Exec(ctx, `INSERT INTO trade.kess_payment_intent (public_id, company_id, channel, trade_no, amount_minor, currency, seller_code, status) VALUES ($1, 11, 'kess', 'DRL-01ARZ3NDEKTSV4RRFFQ69G5FAV', 1, 'KHR', 'seller', 'PENDING')`, "01ARZ3NDEKTSV4RRFFQ69G5FA"+string(rune('B'+i)))
				errs <- err
			}(i)
		}
		close(start)
		wg.Wait()
		close(errs)
		var success, duplicate int
		for err := range errs {
			if err == nil {
				success++
				continue
			}
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "ux_kess_payment_intent_company_channel_trade" {
				duplicate++
				continue
			}
			t.Fatalf("concurrent insert error = %v", err)
		}
		if success != 1 || duplicate != 1 {
			t.Fatalf("success=%d duplicate=%d, want 1/1", success, duplicate)
		}
	})

	t.Run("module and report roles have narrowed privileges", func(t *testing.T) {
		var canMutateReceivable, canRecordStatus, canDelete, canTruncate, reportReadsSeller bool
		if err := pool.QueryRow(ctx, `
			SELECT has_column_privilege('trade_rw', 'trade.kess_payment_intent', 'amount_minor', 'UPDATE'),
			       has_column_privilege('trade_rw', 'trade.kess_payment_intent', 'status', 'UPDATE'),
			       has_table_privilege('trade_rw', 'trade.kess_payment_intent', 'DELETE'),
			       has_table_privilege('trade_rw', 'trade.kess_payment_intent', 'TRUNCATE'),
			       has_column_privilege('report_ro', 'trade.kess_payment_intent', 'seller_code', 'SELECT')`).Scan(&canMutateReceivable, &canRecordStatus, &canDelete, &canTruncate, &reportReadsSeller); err != nil {
			t.Fatal(err)
		}
		if canMutateReceivable || !canRecordStatus || canDelete || canTruncate || reportReadsSeller {
			t.Fatalf("privileges amount=%v status=%v delete=%v truncate=%v seller=%v; want false/true/false/false/false", canMutateReceivable, canRecordStatus, canDelete, canTruncate, reportReadsSeller)
		}
		var publicPrivileges int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.table_privileges WHERE table_schema = 'trade' AND table_name = 'kess_payment_intent' AND grantee = 'PUBLIC'`).Scan(&publicPrivileges); err != nil {
			t.Fatal(err)
		}
		if publicPrivileges != 0 {
			t.Fatalf("PUBLIC privileges = %d, want 0", publicPrivileges)
		}
	})

	t.Run("receivable facts and status transitions are guarded", func(t *testing.T) {
		_, err := pool.Exec(ctx, `UPDATE trade.kess_payment_intent SET amount_minor = amount_minor + 1 WHERE id = $1`, intent.ID)
		assertPGError(t, err, "P0001", "kess payment intent receivable facts are immutable")

		const pendingID = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
		if _, err := pool.Exec(ctx, `INSERT INTO trade.kess_payment_intent (public_id, company_id, channel, trade_no, amount_minor, currency, seller_code, status) VALUES ($1, 12, 'kess', $2, 1, 'USD', 'seller', 'PENDING')`, pendingID, "DRL-"+pendingID); err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `UPDATE trade.kess_payment_intent SET status = 'SUCCESS' WHERE public_id = $1`, pendingID)
		assertPGError(t, err, "P0001", "invalid kess payment intent status transition")

		const successID = "01ARZ3NDEKTSV4RRFFQ69G5FAX"
		if _, err := pool.Exec(ctx, `INSERT INTO trade.kess_payment_intent (public_id, company_id, channel, trade_no, amount_minor, currency, seller_code, status) VALUES ($1, 12, 'kess', $2, 1, 'USD', 'seller', 'SUCCESS')`, successID, "DRL-"+successID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE trade.kess_payment_intent SET status = 'REFUND' WHERE public_id = $1`, successID); err != nil {
			t.Fatalf("SUCCESS -> REFUND: %v", err)
		}
	})
}

func assertPGError(t *testing.T, err error, code, message string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code || pgErr.Message != message {
		t.Fatalf("error = %v, want PostgreSQL %s %q", err, code, message)
	}
}

type persistedThenTimeout struct {
	pool     *pgxpool.Pool
	intentID int64
	seen     bool
}

func (c *persistedThenTimeout) Create(ctx context.Context, _ kess.Intent) (kess.ChannelOrder, error) {
	var count int
	if err := c.pool.QueryRow(ctx, `SELECT count(*) FROM trade.kess_payment_intent WHERE id = $1`, c.intentID).Scan(&count); err != nil {
		return kess.ChannelOrder{}, err
	}
	c.seen = count == 1
	return kess.ChannelOrder{}, context.DeadlineExceeded
}
func (*persistedThenTimeout) Query(context.Context, kess.Intent) (kess.ChannelOrder, error) {
	return kess.ChannelOrder{}, pgx.ErrNoRows
}

func migrationDirectory(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate migrations")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations")
}
