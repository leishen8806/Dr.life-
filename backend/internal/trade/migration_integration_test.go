package trade

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestTradeOrderMigration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	c, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("trade_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	_, file, _, _ := runtime.Caller(0)
	migrations := filepath.Join(filepath.Dir(file), "..", "..", "migrations")
	if err := goose.Up(db, migrations); err != nil {
		t.Fatal(err)
	}
	assertTradeTables(t, db, true)
	if err := goose.DownTo(db, migrations, 52); err != nil {
		t.Fatal(err)
	}
	assertTradeTables(t, db, false)
	if err := goose.Up(db, migrations); err != nil {
		t.Fatal(err)
	}
	assertTradeTables(t, db, true)
}

func assertTradeTables(t *testing.T, db *sql.DB, want bool) {
	t.Helper()
	for _, table := range []string{"buyer_order", "merchant_order", "order_line", "order_event"} {
		var exists bool
		if err := db.QueryRow(`SELECT to_regclass('trade.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != want {
			t.Fatalf("trade.%s exists=%v, want %v", table, exists, want)
		}
	}
}

func TestOrderEventsAreAppendOnlyAndIdempotentUnderConcurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	c, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("trade_events"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	_, file, _, _ := runtime.Caller(0)
	migrations := filepath.Join(filepath.Dir(file), "..", "..", "migrations")
	if err := goose.Up(db, migrations); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO trade.buyer_order (public_id,buyer_account_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAV', 1, 100); INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAW', 1, 9, 100)`); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewPostgresStore(pool)
	if got, err := store.FindBuyerOrder(ctx, 1, "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil || got.TotalE4 != 100 {
		t.Fatalf("owner buyer read = %+v, %v", got, err)
	}
	if _, err := store.FindBuyerOrder(ctx, 2, "01ARZ3NDEKTSV4RRFFQ69G5FAV"); !errors.Is(err, ErrOrderNotVisible) {
		t.Fatalf("foreign buyer read error = %v", err)
	}
	if got, err := store.FindMerchantOrder(ctx, 9, "01ARZ3NDEKTSV4RRFFQ69G5FAW"); err != nil || got.TotalE4 != 100 {
		t.Fatalf("owner merchant read = %+v, %v", got, err)
	}
	if _, err := store.FindMerchantOrder(ctx, 10, "01ARZ3NDEKTSV4RRFFQ69G5FAW"); !errors.Is(err, ErrOrderNotVisible) {
		t.Fatalf("foreign merchant read error = %v", err)
	}
	source := EventSource{Type: SourceKESSResult, Reference: "payment-1"}
	start := make(chan struct{})
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			applied, err := store.Advance(ctx, 1, AwaitingPayment, Paid, source)
			results <- applied
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	applied := 0
	for result := range results {
		if result {
			applied++
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Advance error: %v", err)
		}
	}
	if applied != 1 {
		t.Fatalf("applied=%d, want 1", applied)
	}
	if applied, err := store.Advance(ctx, 1, AwaitingPayment, Paid, source); err != nil || applied {
		t.Fatalf("replay applied=%v err=%v", applied, err)
	}
	var events int
	if err := db.QueryRow(`SELECT count(*) FROM trade.order_event`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("events=%d, want 1", events)
	}
	if _, err := db.Exec(`UPDATE trade.order_event SET source_ref='altered'`); err == nil {
		t.Fatal("UPDATE unexpectedly succeeded")
	}
	if _, err := db.Exec(`DELETE FROM trade.order_event`); err == nil {
		t.Fatal("DELETE unexpectedly succeeded")
	}
}
