package trade

import (
	"context"
	"database/sql"
	"errors"
	"os"
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
	migrations := migrationDirectory(t)
	orderMigration := findMigration(t, migrations, "_trade_orders.sql")
	upgradeMigrations := t.TempDir()
	copyMigrationsExcept(t, migrations, upgradeMigrations, filepath.Base(orderMigration))
	if err := goose.Up(db, upgradeMigrations); err != nil {
		t.Fatal(err)
	}
	baselineVersion, err := goose.GetDBVersion(db)
	if err != nil {
		t.Fatal(err)
	}
	copyMigration(t, orderMigration, filepath.Join(upgradeMigrations, filepath.Base(orderMigration)))
	if err := goose.Up(db, upgradeMigrations); err != nil {
		t.Fatal(err)
	}
	assertTradeTables(t, db, true)
	if err := goose.DownTo(db, upgradeMigrations, baselineVersion); err != nil {
		t.Fatal(err)
	}
	assertTradeTables(t, db, false)
	if err := goose.Up(db, upgradeMigrations); err != nil {
		t.Fatal(err)
	}
	assertTradeTables(t, db, true)
}

func migrationDirectory(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate migrations")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}

func copyMigrationsExcept(t *testing.T, source, destination, excluded string) {
	t.Helper()
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" || entry.Name() == excluded {
			continue
		}
		copyMigration(t, filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name()))
	}
}

func copyMigration(t *testing.T, source, destination string) {
	t.Helper()
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func findMigration(t *testing.T, directory, suffix string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(directory, "*"+suffix))
	if err != nil || len(matches) != 1 {
		t.Fatalf("find migration %q: matches=%v err=%v", suffix, matches, err)
	}
	return matches[0]
}

func assertTradeTables(t *testing.T, db *sql.DB, want bool) {
	t.Helper()
	for _, table := range []string{"buyer_order", "merchant_order", "order_line", "order_event_source", "order_event"} {
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
	if _, err := db.Exec(`INSERT INTO trade.buyer_order (public_id,buyer_account_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAV', 1, 100); INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAW', 1, 9, 100); INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4) VALUES (1,'01ARZ3NDEKTSV4RRFFQ69G5FAX',1,100,100)`); err != nil {
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
	var externalOrderID string
	if err := db.QueryRow(`SELECT external_order_id FROM trade.merchant_order WHERE id=1`).Scan(&externalOrderID); err != nil || externalOrderID != "DRL-01ARZ3NDEKTSV4RRFFQ69G5FAW" {
		t.Fatalf("external_order_id=%q err=%v", externalOrderID, err)
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
	if _, err := db.Exec(`UPDATE trade.merchant_order SET status='awaiting_payment' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if applied, err := store.Advance(ctx, 1, AwaitingPayment, Paid, source); err != nil || applied {
		t.Fatalf("replay after state rollback applied=%v err=%v", applied, err)
	}
	if _, err := db.Exec(`UPDATE trade.merchant_order SET status='paid' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if applied, err := store.Advance(ctx, 1, Shipped, DeliveredVerified, source); !errors.Is(err, ErrInvalidTransition) || applied {
		t.Fatalf("mismatched replay applied=%v err=%v", applied, err)
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
	if _, err := db.Exec(`TRUNCATE trade.order_event`); err == nil {
		t.Fatal("TRUNCATE unexpectedly succeeded")
	}
	var canReferences, canTrigger bool
	if err := db.QueryRow(`SELECT has_table_privilege('trade_rw','trade.order_event','REFERENCES'), has_table_privilege('trade_rw','trade.order_event','TRIGGER')`).Scan(&canReferences, &canTrigger); err != nil {
		t.Fatal(err)
	}
	if canReferences || canTrigger {
		t.Fatalf("trade_rw order_event REFERENCES=%v TRIGGER=%v, want false/false", canReferences, canTrigger)
	}

	if _, err := db.Exec(`INSERT INTO trade.buyer_order (public_id,buyer_account_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAY', 2, 100); INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAZ', 2, 10, 100); INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4) VALUES (2,'01ARZ3NDEKTSV4RRFFQ69G5FB0',1,100,100)`); err != nil {
		t.Fatal(err)
	}
	if applied, err := store.Advance(ctx, 2, AwaitingPayment, Paid, source); !errors.Is(err, ErrInvalidTransition) || applied {
		t.Fatalf("cross-order replay applied=%v err=%v", applied, err)
	}

	assertAmountConstraints(t, ctx, pool)
	assertSharedSourceWithinBuyer(t, ctx, pool, store)
	assertDifferentSourcesSerialize(t, ctx, db, pool, store)
}

func assertAmountConstraints(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO trade.buyer_order (public_id,buyer_account_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FB1', 3, 1); INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FB2', 3, 11, 0)`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("inconsistent buyer/merchant totals committed")
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO trade.buyer_order (public_id,buyer_account_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FB6', 3, 100); INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FB7', 4, 11, 100); INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4) VALUES (4,'01ARZ3NDEKTSV4RRFFQ69G5FB8',1,50,50)`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("inconsistent merchant/line totals committed")
	}
}

func assertDifferentSourcesSerialize(t *testing.T, ctx context.Context, db *sql.DB, pool *pgxpool.Pool, store *PostgresStore) {
	t.Helper()
	var merchantOrderID int64
	if err := db.QueryRow(`WITH buyer AS (INSERT INTO trade.buyer_order (public_id,buyer_account_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FB3', 4, 100) RETURNING id), merchant AS (INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FB4', id, 12, 100 FROM buyer RETURNING id) INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4) SELECT id,'01ARZ3NDEKTSV4RRFFQ69G5FB5',1,100,100 FROM merchant RETURNING merchant_order_id`).Scan(&merchantOrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE FUNCTION trade.test_block_order_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(540054); RETURN NEW; END $$; CREATE TRIGGER trg_test_block_order_event BEFORE INSERT ON trade.order_event FOR EACH ROW EXECUTE FUNCTION trade.test_block_order_event()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TRIGGER IF EXISTS trg_test_block_order_event ON trade.order_event; DROP FUNCTION IF EXISTS trade.test_block_order_event()`)
	})
	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Release()
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_lock(540054)`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		applied bool
		err     error
	}
	results := make(chan result, 2)
	go func() {
		applied, err := store.Advance(ctx, merchantOrderID, AwaitingPayment, Paid, EventSource{Type: SourceKESSResult, Reference: "payment-a"})
		results <- result{applied, err}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := db.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE query LIKE 'INSERT INTO trade.order_event %' AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first transition did not reach blocked event insert")
		}
		time.Sleep(10 * time.Millisecond)
	}
	go func() {
		applied, err := store.Advance(ctx, merchantOrderID, AwaitingPayment, Cancelled, EventSource{Type: SourceSystem, Reference: "cancel-b"})
		results <- result{applied, err}
	}()
	time.Sleep(200 * time.Millisecond)
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_unlock(540054)`); err != nil {
		t.Fatal(err)
	}
	var applied, rejected int
	for range 2 {
		result := <-results
		if result.applied && result.err == nil {
			applied++
		} else if !result.applied && errors.Is(result.err, ErrInvalidTransition) {
			rejected++
		} else {
			t.Fatalf("unexpected concurrent result: %+v", result)
		}
	}
	if applied != 1 || rejected != 1 {
		t.Fatalf("applied=%d rejected=%d, want 1/1", applied, rejected)
	}
}

func assertSharedSourceWithinBuyer(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *PostgresStore) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var buyerOrderID, firstMerchantOrderID, secondMerchantOrderID int64
	if err := tx.QueryRow(ctx, `INSERT INTO trade.buyer_order (public_id,buyer_account_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FB9', 5, 200) RETURNING id`).Scan(&buyerOrderID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FBA',$1,13,100) RETURNING id`, buyerOrderID).Scan(&firstMerchantOrderID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FBB',$1,14,100) RETURNING id`, buyerOrderID).Scan(&secondMerchantOrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4) VALUES ($1,'01ARZ3NDEKTSV4RRFFQ69G5FBC',1,100,100),($2,'01ARZ3NDEKTSV4RRFFQ69G5FBD',1,100,100)`, firstMerchantOrderID, secondMerchantOrderID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	sharedSource := EventSource{Type: SourceKESSResult, Reference: "buyer-payment-shared"}
	for _, merchantOrderID := range []int64{firstMerchantOrderID, secondMerchantOrderID} {
		if applied, err := store.Advance(ctx, merchantOrderID, AwaitingPayment, Paid, sharedSource); err != nil || !applied {
			t.Fatalf("shared buyer source order=%d applied=%v err=%v", merchantOrderID, applied, err)
		}
	}
}
