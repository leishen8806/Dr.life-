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
		var canMutateReceivable, canRecordStatus, reportReadsSeller bool
		if err := pool.QueryRow(ctx, `
			SELECT has_column_privilege('trade_rw', 'trade.kess_payment_intent', 'amount_minor', 'UPDATE'),
			       has_column_privilege('trade_rw', 'trade.kess_payment_intent', 'status', 'UPDATE'),
			       has_column_privilege('report_ro', 'trade.kess_payment_intent', 'seller_code', 'SELECT')`).Scan(&canMutateReceivable, &canRecordStatus, &reportReadsSeller); err != nil {
			t.Fatal(err)
		}
		if canMutateReceivable || !canRecordStatus || reportReadsSeller {
			t.Fatalf("privileges amount=%v status=%v seller=%v; want false/true/false", canMutateReceivable, canRecordStatus, reportReadsSeller)
		}
	})
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
