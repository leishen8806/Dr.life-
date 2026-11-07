package trade

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
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
	for _, table := range []string{"buyer_order", "reservation_release_outbox", "merchant_order", "order_line", "order_event_source", "order_event", "payment_transition_guard"} {
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
	if _, err := db.Exec(`INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAV', 1, 'idem-1', repeat('0',64), 100); INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAW', 1, 9, 100); INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4) VALUES (1,'01ARZ3NDEKTSV4RRFFQ69G5FAX',1,100,100)`); err != nil {
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
	if applied, err := store.Advance(ctx, 1, AwaitingPayment, Paid, source); !errors.Is(err, ErrInvalidTransition) || applied {
		t.Fatalf("single-child payment applied=%v err=%v", applied, err)
	}
	start := make(chan struct{})
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV", 100, source)
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
			t.Fatalf("concurrent ConfirmPayment error: %v", err)
		}
	}
	if applied != 1 {
		t.Fatalf("applied=%d, want 1", applied)
	}
	if _, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV", 100, source); err != nil || applied {
		t.Fatalf("replay applied=%v err=%v", applied, err)
	}
	if got, err := store.FindBuyerOrder(ctx, 1, "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil || got.Status != string(Paid) {
		t.Fatalf("parent status=%+v err=%v", got, err)
	}
	if _, err := db.Exec(`INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4) VALUES (1,'01ARZ3NDEKTSV4RRFFQ69G5FBF',1,100,100)`); err == nil {
		t.Fatal("order line insert after payment unexpectedly succeeded")
	}
	if _, err := db.Exec(`UPDATE trade.merchant_order SET status='awaiting_payment' WHERE id=1`); err == nil {
		t.Fatal("direct status rollback unexpectedly succeeded")
	}
	if _, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV", 100, source); err != nil || applied {
		t.Fatalf("replay after state rollback applied=%v err=%v", applied, err)
	}
	if _, err := db.Exec(`UPDATE trade.merchant_order SET status='submitted' WHERE id=1`); err == nil {
		t.Fatal("direct status advance unexpectedly succeeded")
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

	if _, err := db.Exec(`INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAY', 2, 'idem-2', repeat('1',64), 100); INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAZ', 2, 10, 100); INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4) VALUES (2,'01ARZ3NDEKTSV4RRFFQ69G5FB0',1,100,100)`); err != nil {
		t.Fatal(err)
	}
	if _, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAY", 100, source); !errors.Is(err, ErrInvalidTransition) || applied {
		t.Fatalf("cross-order replay applied=%v err=%v", applied, err)
	}

	assertAmountConstraints(t, ctx, pool)
	assertSharedSourceWithinBuyer(t, ctx, pool, store)
	assertPartialCancellationRejected(t, ctx, pool, store)
	assertReservationReleaseCancelsAndQueuesFailures(t, ctx, pool, store)
	assertPaymentSQLStates(t, ctx, pool, store)
	assertCreatedTxidCannotBeForged(t, ctx, pool)
	assertPaymentReleaseGuards(t, ctx, pool, store)
	assertRefundRequiredBlocksSubmission(t, ctx, pool, store)
	assertReservationReleaseOutbox(t, ctx, pool, store)
	assertDuplicatePaymentSignal(t, ctx, pool, store)
	assertNoChildOrderHandling(t, ctx, pool, store)
	assertTradeSecurityDefinerAndPrivileges(t, ctx, db, dsn)
	assertBuyerIsolationAndForeignEvent(t, ctx, pool)
	assertLineAndParentTotalGuards(t, ctx, pool)
	assertDifferentPaymentSourcesSerialize(t, ctx, pool, store)
	assertTransitionMatrix(t, ctx, pool, store)
	assertDifferentSourcesSerialize(t, ctx, db, pool, store)
	assertSiblingTransitionsSerialize(t, ctx, pool, store)
	assertTradeRWCreateIsIdempotent(t, ctx, dsn)
}

func assertTradeRWCreateIsIdempotent(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, `SET ROLE trade_rw`)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewPostgresStore(pool)
	input := CreateOrder{BuyerAccountID: 88, TotalE4: 200, Children: []ChildOrder{{MerchantID: 89, TotalE4: 200, Lines: []OrderItem{{SKUPublicID: "01ARZ3NDEKTSV4RRFFQ69G5FBE", MerchantID: 89, Quantity: 2, UnitPriceE4: 100, LineTotalE4: 200}}}}}
	first, err := store.Create(ctx, "rw-idem", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := store.Create(ctx, "rw-idem", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", input)
	if err != nil || replay.PublicID != first.PublicID || len(replay.Children) != 1 {
		t.Fatalf("replay=%+v first=%+v err=%v", replay, first, err)
	}
	if _, err := store.Create(ctx, "rw-idem", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", input); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting replay error=%v", err)
	}
	var merchantOrderID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM trade.merchant_order WHERE buyer_order_id=(SELECT id FROM trade.buyer_order WHERE public_id=$1)`, first.PublicID).Scan(&merchantOrderID); err != nil {
		t.Fatal(err)
	}
	assertPGCode(t, func() error {
		_, err := pool.Exec(ctx, `UPDATE trade.merchant_order SET status='paid',version=version+1 WHERE id=$1`, merchantOrderID)
		return err
	}(), "42501")
	if key, applied, err := store.ConfirmPayment(ctx, first.PublicID, 200, EventSource{Type: SourceKESSResult, Reference: "rw-payment"}); err != nil || !applied || key != "88:rw-idem" {
		t.Fatalf("trade_rw parent payment key=%q applied=%v err=%v", key, applied, err)
	}
}

func assertAmountConstraints(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FB1', 3, 'idem-3', repeat('2',64), 1); INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FB2', 3, 11, 0)`); err != nil {
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
	if _, err := tx.Exec(ctx, `INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FB6', 3, 'idem-4', repeat('3',64), 100); INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FB7', 4, 11, 100); INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4) VALUES (4,'01ARZ3NDEKTSV4RRFFQ69G5FB8',1,0,0)`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("inconsistent merchant/line totals committed")
	}
}

func assertDifferentSourcesSerialize(t *testing.T, ctx context.Context, db *sql.DB, pool *pgxpool.Pool, store *PostgresStore) {
	t.Helper()
	var merchantOrderID int64
	if err := db.QueryRow(`WITH buyer AS (INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FB3', 4, 'idem-5', repeat('4',64), 100) RETURNING id), merchant AS (INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FB4', id, 12, 100 FROM buyer RETURNING id) INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4) SELECT id,'01ARZ3NDEKTSV4RRFFQ69G5FB5',1,100,100 FROM merchant RETURNING merchant_order_id`).Scan(&merchantOrderID); err != nil {
		t.Fatal(err)
	}
	if _, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FB3", 100, EventSource{Type: SourceKESSResult, Reference: "payment-before-serialize"}); err != nil || !applied {
		t.Fatalf("prepare paid order applied=%v err=%v", applied, err)
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
		applied, err := store.Advance(ctx, merchantOrderID, Paid, Submitted, EventSource{Type: SourceSystem, Reference: "submit-a"})
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
		applied, err := store.Advance(ctx, merchantOrderID, Paid, Submitted, EventSource{Type: SourceSystem, Reference: "submit-b"})
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

func assertSiblingTransitionsSerialize(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *PostgresStore) {
	t.Helper()
	var firstID, secondID int64
	if err := pool.QueryRow(ctx, `WITH buyer AS (
		INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FDP',46,'sibling-lock',repeat('c',64),200) RETURNING id
	), first_child AS (
		INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FDQ',id,546,100 FROM buyer RETURNING id,buyer_order_id
	), second_child AS (
		INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FDR',buyer_order_id,547,100 FROM first_child RETURNING id
	), first_line AS (
		INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
		SELECT id,'01ARZ3NDEKTSV4RRFFQ69G5FDS',1,100,100 FROM first_child
	), second_line AS (
		INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
		SELECT id,'01ARZ3NDEKTSV4RRFFQ69G5FDT',1,100,100 FROM second_child
	) SELECT first_child.id,second_child.id FROM first_child,second_child`).Scan(&firstID, &secondID); err != nil {
		t.Fatal(err)
	}
	if _, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FDP", 200, EventSource{Type: SourceKESSResult, Reference: "sibling-payment"}); err != nil || !applied {
		t.Fatalf("sibling payment applied=%v err=%v", applied, err)
	}
	type outcome struct {
		applied bool
		err     error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for index, id := range []int64{firstID, secondID} {
		go func(index int, id int64) {
			<-start
			applied, err := store.Advance(ctx, id, Paid, Submitted, EventSource{Type: SourceSystem, Reference: fmt.Sprintf("sibling-submit-%d", index)})
			results <- outcome{applied, err}
		}(index, id)
	}
	close(start)
	for range 2 {
		result := <-results
		if result.err != nil || !result.applied {
			t.Fatalf("sibling transition applied=%v err=%v", result.applied, result.err)
		}
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
	if err := tx.QueryRow(ctx, `INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FB9', 5, 'idem-6', repeat('5',64), 200) RETURNING id`).Scan(&buyerOrderID); err != nil {
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
	if key, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FB9", 200, sharedSource); err != nil || !applied || key != "5:idem-6" {
		t.Fatalf("parent payment key=%q applied=%v err=%v", key, applied, err)
	}
	var paidChildren, paymentEvents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM trade.merchant_order WHERE buyer_order_id=$1 AND status='paid'`, buyerOrderID).Scan(&paidChildren); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM trade.order_event WHERE buyer_order_id=$1 AND source_ref=$2`, buyerOrderID, sharedSource.Reference).Scan(&paymentEvents); err != nil {
		t.Fatal(err)
	}
	if paidChildren != 2 || paymentEvents != 2 {
		t.Fatalf("paid children=%d payment events=%d, want 2/2", paidChildren, paymentEvents)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO trade.order_event_source (buyer_order_id,source_type,source_ref) VALUES ($1,'system','stale-submit')`, buyerOrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO trade.order_event (merchant_order_id,buyer_order_id,from_status,to_status,source_type,source_ref)
		VALUES ($1,$2,'paid','submitted','system','stale-submit')`, firstMerchantOrderID, buyerOrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE trade.merchant_order SET status='submitted',version=version+1 WHERE id=$1`, firstMerchantOrderID); err == nil {
		t.Fatal("historical matching event authorized a later transition")
	}
	if key, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FB9", 200, sharedSource); err != nil || applied || key != "5:idem-6" {
		t.Fatalf("parent payment replay key=%q applied=%v err=%v", key, applied, err)
	}
}

func assertPartialCancellationRejected(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *PostgresStore) {
	t.Helper()
	var buyerOrderID, firstChildID int64
	if err := pool.QueryRow(ctx, `WITH buyer AS (
		INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FD0',40,'cancel-cascade',repeat('7',64),200) RETURNING id
	), first_child AS (
		INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FD1',id,401,100 FROM buyer RETURNING id,buyer_order_id
	), second_child AS (
		INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FD2',buyer_order_id,402,100 FROM first_child RETURNING id,merchant_id
	), first_line AS (
		INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
		SELECT id,'01ARZ3NDEKTSV4RRFFQ69G5FD3',1,100,100 FROM first_child
	), second_line AS (
		INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
		SELECT id,'01ARZ3NDEKTSV4RRFFQ69G5FD4',1,100,100 FROM second_child
	) SELECT buyer_order_id,id FROM first_child`).Scan(&buyerOrderID, &firstChildID); err != nil {
		t.Fatal(err)
	}
	if applied, err := store.Advance(ctx, firstChildID, AwaitingPayment, Cancelled, EventSource{Type: SourceSystem, Reference: "cancel-through-advance"}); !errors.Is(err, ErrInvalidTransition) || applied {
		t.Fatalf("cancel through generic advance applied=%v err=%v, want ErrInvalidTransition", applied, err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO trade.order_event_source (buyer_order_id,source_type,source_ref) VALUES ($1,'system','partial-cancel')`, buyerOrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trade.order_event (merchant_order_id,buyer_order_id,from_status,to_status,source_type,source_ref)
		VALUES ($1,$2,'awaiting_payment','cancelled','system','partial-cancel')`, firstChildID, buyerOrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE trade.merchant_order SET status='cancelled',version=version+1 WHERE id=$1`, firstChildID); err != nil {
		t.Fatal(err)
	}
	assertPGCode(t, tx.Commit(ctx), "TR003")
	var awaiting int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM trade.merchant_order WHERE buyer_order_id=$1 AND status='awaiting_payment'`, buyerOrderID).Scan(&awaiting); err != nil || awaiting != 2 {
		t.Fatalf("awaiting children=%d err=%v, want 2 after rejected partial cancel", awaiting, err)
	}
}

func assertReservationReleaseCancelsAndQueuesFailures(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *PostgresStore) {
	t.Helper()
	var buyerOrderID int64
	if err := pool.QueryRow(ctx, `WITH buyer AS (
		INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FD5',41,'release-cascade',repeat('8',64),200) RETURNING id
	), children AS (
		INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FD6',id,411,100 FROM buyer UNION ALL
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FD7',id,412,100 FROM buyer RETURNING id,buyer_order_id,merchant_id
	), lines AS (
		INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
		SELECT id,CASE merchant_id WHEN 411 THEN '01ARZ3NDEKTSV4RRFFQ69G5FD8' ELSE '01ARZ3NDEKTSV4RRFFQ69G5FD9' END,1,100,100 FROM children
	) SELECT id FROM buyer`).Scan(&buyerOrderID); err != nil {
		t.Fatal(err)
	}
	releaseErr := errors.New("catalog release unavailable")
	service := NewService(store, stubAccounts{"active"}, &stubCatalog{releaseErr: releaseErr})
	for attempt := 0; attempt < 2; attempt++ {
		if err := service.ReleaseReservations(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FD5"); !errors.Is(err, releaseErr) {
			t.Fatalf("release attempt %d error=%v, want catalog release unavailable", attempt+1, err)
		}
	}
	var cancelled, queued int
	var released bool
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM trade.merchant_order WHERE buyer_order_id=$1 AND status='cancelled'`, buyerOrderID).Scan(&cancelled); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM trade.reservation_release_outbox WHERE reservation_key='41:release-cascade' AND action='release'`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT reservation_released_at IS NOT NULL FROM trade.buyer_order WHERE id=$1`, buyerOrderID).Scan(&released); err != nil {
		t.Fatal(err)
	}
	if cancelled != 2 || queued != 1 || !released {
		t.Fatalf("cancelled=%d queued=%d released=%v, want 2/1/true", cancelled, queued, released)
	}
	assertPGCode(t, func() error {
		_, err := pool.Exec(ctx, `SELECT * FROM trade.confirm_order_payment('01ARZ3NDEKTSV4RRFFQ69G5FD5','cancelled-payment',200)`)
		return err
	}(), "TR001")
	if _, err := pool.Exec(ctx, `DELETE FROM trade.reservation_release_outbox WHERE reservation_key='41:release-cascade'`); err != nil {
		t.Fatal(err)
	}
}

func assertRefundRequiredBlocksSubmission(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *PostgresStore) {
	t.Helper()
	var merchantOrderID int64
	if err := pool.QueryRow(ctx, `WITH buyer AS (
		INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FDK',45,'refund-required',repeat('b',64),100) RETURNING id
	), merchant AS (
		INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FDM',id,545,100 FROM buyer RETURNING id
	) INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
	SELECT id,'01ARZ3NDEKTSV4RRFFQ69G5FDN',1,100,100 FROM merchant RETURNING merchant_order_id`).Scan(&merchantOrderID); err != nil {
		t.Fatal(err)
	}
	if key, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FDK", 100, EventSource{Type: SourceKESSResult, Reference: "refund-payment"}); err != nil || !applied || key != "45:refund-required" {
		t.Fatalf("confirm payment key=%q applied=%v err=%v", key, applied, err)
	}
	if err := store.RequireRefund(ctx, "45:refund-required"); err != nil {
		t.Fatal(err)
	}
	if applied, err := store.Advance(ctx, merchantOrderID, Paid, Submitted, EventSource{Type: SourceSystem, Reference: "refund-submit"}); !errors.Is(err, ErrReservationReleased) || applied {
		t.Fatalf("refund-required submit applied=%v err=%v, want ErrReservationReleased", applied, err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trade.order_event_source (buyer_order_id,source_type,source_ref)
		SELECT buyer_order_id,'system','refund-submit-direct' FROM trade.merchant_order WHERE id=$1`, merchantOrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trade.order_event (merchant_order_id,buyer_order_id,from_status,to_status,source_type,source_ref)
		SELECT id,buyer_order_id,'paid','submitted','system','refund-submit-direct' FROM trade.merchant_order WHERE id=$1`, merchantOrderID); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `UPDATE trade.merchant_order SET status='submitted',version=version+1 WHERE id=$1`, merchantOrderID)
	assertPGCode(t, err, "TR004")
	_ = tx.Rollback(ctx)
	var marked bool
	var pendingCommit int
	if err := pool.QueryRow(ctx, `SELECT refund_required_at IS NOT NULL FROM trade.buyer_order WHERE public_id='01ARZ3NDEKTSV4RRFFQ69G5FDK'`).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM trade.reservation_release_outbox WHERE reservation_key='45:refund-required' AND action='commit' AND completed_at IS NULL`).Scan(&pendingCommit); err != nil {
		t.Fatal(err)
	}
	if !marked || pendingCommit != 0 {
		t.Fatalf("refund_required=%v pending_commit=%d, want true/0", marked, pendingCommit)
	}
}

func assertPaymentSQLStates(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *PostgresStore) {
	t.Helper()
	create := func(parentID, childID, skuID, key string, buyerID int64) {
		t.Helper()
		if _, err := pool.Exec(ctx, `WITH buyer AS (
			INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4)
			VALUES ($1,$2,$3,repeat('9',64),100) RETURNING id
		), child AS (
			INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
			SELECT $4,id,$2+500,100 FROM buyer RETURNING id
		) INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
		SELECT id,$5,1,100,100 FROM child`, parentID, buyerID, key, childID, skuID); err != nil {
			t.Fatal(err)
		}
	}
	create("01ARZ3NDEKTSV4RRFFQ69G5FDA", "01ARZ3NDEKTSV4RRFFQ69G5FDB", "01ARZ3NDEKTSV4RRFFQ69G5FDC", "sqlstate-amount", 42)
	assertPGCode(t, func() error {
		_, err := pool.Exec(ctx, `SELECT * FROM trade.confirm_order_payment('01ARZ3NDEKTSV4RRFFQ69G5FDA','wrong-amount',99)`)
		return err
	}(), "TR002")
	create("01ARZ3NDEKTSV4RRFFQ69G5FDD", "01ARZ3NDEKTSV4RRFFQ69G5FDE", "01ARZ3NDEKTSV4RRFFQ69G5FDF", "sqlstate-release", 43)
	if _, claimed, err := store.ClaimReservationRelease(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FDD"); err != nil || !claimed {
		t.Fatalf("release claim claimed=%v err=%v", claimed, err)
	}
	assertPGCode(t, func() error {
		_, err := pool.Exec(ctx, `SELECT * FROM trade.confirm_order_payment('01ARZ3NDEKTSV4RRFFQ69G5FDD','released-payment',100)`)
		return err
	}(), "TR001")
}

func assertCreatedTxidCannotBeForged(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var eventTxid, currentTxid int64
	if err := pool.QueryRow(ctx, `WITH buyer AS (
		INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FDG',44,'txid-guard',repeat('a',64),100) RETURNING id
	), child AS (
		INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FDH',id,544,100 FROM buyer RETURNING id,buyer_order_id
	), line AS (
		INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
		SELECT id,'01ARZ3NDEKTSV4RRFFQ69G5FDJ',1,100,100 FROM child
	), source AS (
		INSERT INTO trade.order_event_source (buyer_order_id,source_type,source_ref)
		SELECT buyer_order_id,'system','txid-forgery' FROM child
	), event AS (
		INSERT INTO trade.order_event (merchant_order_id,buyer_order_id,from_status,to_status,source_type,source_ref,created_txid)
		SELECT id,buyer_order_id,'awaiting_payment','cancelled','system','txid-forgery',0 FROM child RETURNING created_txid
	) SELECT created_txid,txid_current() FROM event`).Scan(&eventTxid, &currentTxid); err != nil {
		t.Fatal(err)
	}
	if eventTxid != currentTxid {
		t.Fatalf("created_txid=%d, want server txid %d", eventTxid, currentTxid)
	}
}

func assertPGCode(t *testing.T, err error, want string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != want {
		got := ""
		if pgErr != nil {
			got = pgErr.Code
		}
		t.Fatalf("PostgreSQL error=%v code=%q, want %s", err, got, want)
	}
}

func assertPaymentReleaseGuards(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *PostgresStore) {
	t.Helper()
	createOrder := func(parentID, childID, skuID, key string, buyerID int64) {
		t.Helper()
		if _, err := pool.Exec(ctx, `WITH buyer AS (
			INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4)
			VALUES ($1,$2,$3,repeat('6',64),100) RETURNING id
		), merchant AS (
			INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
			SELECT $4,id,$2+100,100 FROM buyer RETURNING id
		) INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
		SELECT id,$5,1,100,100 FROM merchant`, parentID, buyerID, key, childID, skuID); err != nil {
			t.Fatal(err)
		}
	}

	createOrder("01ARZ3NDEKTSV4RRFFQ69G5FC0", "01ARZ3NDEKTSV4RRFFQ69G5FC1", "01ARZ3NDEKTSV4RRFFQ69G5FC2", "idem-amount", 20)
	if _, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FC0", 99, EventSource{Type: SourceKESSResult, Reference: "wrong-amount"}); !errors.Is(err, ErrPaymentAmount) || applied {
		t.Fatalf("wrong amount applied=%v err=%v", applied, err)
	}

	createOrder("01ARZ3NDEKTSV4RRFFQ69G5FC3", "01ARZ3NDEKTSV4RRFFQ69G5FC4", "01ARZ3NDEKTSV4RRFFQ69G5FC5", "idem-release", 21)
	reservation, claimed, err := store.ClaimReservationRelease(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FC3")
	if err != nil || !claimed || reservation.Key != "21:idem-release" {
		t.Fatalf("claim release=%+v claimed=%v err=%v", reservation, claimed, err)
	}
	if _, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FC3", 100, EventSource{Type: SourceKESSResult, Reference: "released-payment"}); !errors.Is(err, ErrReservationReleased) || applied {
		t.Fatalf("released payment applied=%v err=%v", applied, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE trade.buyer_order SET reservation_released_at=NULL WHERE public_id='01ARZ3NDEKTSV4RRFFQ69G5FC3'`); err == nil {
		t.Fatal("clearing reservation_released_at unexpectedly succeeded")
	}

	createOrder("01ARZ3NDEKTSV4RRFFQ69G5FC6", "01ARZ3NDEKTSV4RRFFQ69G5FC7", "01ARZ3NDEKTSV4RRFFQ69G5FC8", "idem-race", 22)
	start := make(chan struct{})
	type outcome struct {
		kind    string
		applied bool
		claimed bool
		err     error
	}
	outcomes := make(chan outcome, 2)
	go func() {
		<-start
		_, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FC6", 100, EventSource{Type: SourceKESSResult, Reference: "race-payment"})
		outcomes <- outcome{kind: "payment", applied: applied, err: err}
	}()
	go func() {
		<-start
		_, claimed, err := store.ClaimReservationRelease(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FC6")
		outcomes <- outcome{kind: "release", claimed: claimed, err: err}
	}()
	close(start)
	first, second := <-outcomes, <-outcomes
	results := map[string]outcome{first.kind: first, second.kind: second}
	payment, release := results["payment"], results["release"]
	if payment.applied {
		if payment.err != nil || !errors.Is(release.err, ErrInvalidTransition) || release.claimed {
			t.Fatalf("payment won: payment=%+v release=%+v", payment, release)
		}
	} else if release.claimed {
		if release.err != nil || !errors.Is(payment.err, ErrReservationReleased) {
			t.Fatalf("release won: payment=%+v release=%+v", payment, release)
		}
	} else {
		t.Fatalf("neither race path won: payment=%+v release=%+v", payment, release)
	}
}

func assertReservationReleaseOutbox(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *PostgresStore) {
	t.Helper()
	var pendingCommits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM trade.reservation_release_outbox WHERE action='commit'`).Scan(&pendingCommits); err != nil || pendingCommits == 0 {
		t.Fatalf("pending payment commits=%d err=%v", pendingCommits, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM trade.reservation_release_outbox WHERE action='commit'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM trade.reservation_release_outbox WHERE action='release'`); err != nil {
		t.Fatal(err)
	}
	if err := store.QueueReservationRelease(ctx, "31:outbox", "test"); err != nil {
		t.Fatal(err)
	}
	items, err := store.ClaimReservationActions(ctx, 10)
	if err != nil || len(items) != 1 || items[0].Key != "31:outbox" || items[0].Action != "release" {
		t.Fatalf("first claim=%+v err=%v", items, err)
	}
	if again, err := store.ClaimReservationActions(ctx, 10); err != nil || len(again) != 0 {
		t.Fatalf("concurrent claim=%+v err=%v", again, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE trade.reservation_release_outbox SET claimed_at=clock_timestamp()-interval '6 minutes' WHERE id=$1`, items[0].ID); err != nil {
		t.Fatal(err)
	}
	items, err = store.ClaimReservationActions(ctx, 10)
	if err != nil || len(items) != 1 || items[0].Key != "31:outbox" {
		t.Fatalf("expired lease claim=%+v err=%v", items, err)
	}
	if err := store.FailReservationAction(ctx, items[0].ID); err != nil {
		t.Fatal(err)
	}
	if immediate, err := store.ClaimReservationActions(ctx, 10); err != nil || len(immediate) != 0 {
		t.Fatalf("immediate retry=%+v err=%v", immediate, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE trade.reservation_release_outbox SET next_attempt_at=clock_timestamp() WHERE id=$1`, items[0].ID); err != nil {
		t.Fatal(err)
	}
	items, err = store.ClaimReservationActions(ctx, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("retry claim=%+v err=%v", items, err)
	}
	if err := store.CompleteReservationAction(ctx, items[0].ID); err != nil {
		t.Fatal(err)
	}
	var remaining, attempts int
	var reason string
	var completed bool
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE completed_at IS NULL),max(attempts),min(reason),bool_and(completed_at IS NOT NULL) FROM trade.reservation_release_outbox WHERE reservation_key='31:outbox'`).Scan(&remaining, &attempts, &reason, &completed); err != nil || remaining != 0 || attempts != 3 || reason != "test" || !completed {
		t.Fatalf("remaining=%d attempts=%d reason=%q completed=%v err=%v", remaining, attempts, reason, completed, err)
	}
	if err := store.QueueReservationRelease(ctx, "31:outbox", "overwritten"); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT reason FROM trade.reservation_release_outbox WHERE reservation_key='31:outbox' AND action='release'`).Scan(&reason); err != nil || reason != "test" {
		t.Fatalf("requeue reason=%q err=%v, want original reason", reason, err)
	}
}

func assertDuplicatePaymentSignal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *PostgresStore) {
	t.Helper()
	if _, err := pool.Exec(ctx, `WITH buyer AS (
		INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FE0',60,'duplicate-payment',repeat('d',64),100) RETURNING id
	), child AS (
		INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FE1',id,660,100 FROM buyer RETURNING id
	) INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
	SELECT id,'01ARZ3NDEKTSV4RRFFQ69G5FE2',1,100,100 FROM child`); err != nil {
		t.Fatal(err)
	}
	if _, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FE0", 100, EventSource{Type: SourceKESSResult, Reference: "duplicate-payment-a"}); err != nil || !applied {
		t.Fatalf("first payment applied=%v err=%v", applied, err)
	}
	if _, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FE0", 100, EventSource{Type: SourceKESSResult, Reference: "duplicate-payment-b"}); !errors.Is(err, ErrPaymentReconciliation) || applied {
		t.Fatalf("second payment applied=%v err=%v, want ErrPaymentReconciliation", applied, err)
	}
	if _, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FE0", 100, EventSource{Type: SourceKESSResult, Reference: "duplicate-payment-a"}); err != nil || applied {
		t.Fatalf("same-source replay applied=%v err=%v", applied, err)
	}
	var sources int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM trade.order_event_source WHERE source_ref IN ('duplicate-payment-a','duplicate-payment-b')`).Scan(&sources); err != nil || sources != 2 {
		t.Fatalf("payment sources=%d err=%v, want 2 for reconciliation", sources, err)
	}
}

func assertNoChildOrderHandling(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *PostgresStore) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FE3',61,'empty-order',repeat('e',64),0)`); err != nil {
		t.Fatal(err)
	}
	if _, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FE3", 0, EventSource{Type: SourceKESSResult, Reference: "empty-payment"}); !errors.Is(err, ErrInvalidTransition) || applied {
		t.Fatalf("empty payment applied=%v err=%v, want ErrInvalidTransition", applied, err)
	}
	if _, claimed, err := store.ClaimReservationRelease(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FE3"); !errors.Is(err, ErrInvalidTransition) || claimed {
		t.Fatalf("empty release claimed=%v err=%v, want ErrInvalidTransition", claimed, err)
	}
}

func assertTradeSecurityDefinerAndPrivileges(t *testing.T, ctx context.Context, db *sql.DB, dsn string) {
	t.Helper()
	var owner, config string
	var securityDefiner, publicExecute, tradeExecute, outboxDelete bool
	if err := db.QueryRow(`SELECT pg_get_userbyid(p.proowner),p.prosecdef,array_to_string(p.proconfig,',')
		FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
		WHERE n.nspname='trade' AND p.proname='confirm_order_payment'`).Scan(&owner, &securityDefiner, &config); err != nil {
		t.Fatal(err)
	}
	if owner == "trade_rw" || !securityDefiner || config != "search_path=pg_catalog, trade" {
		t.Fatalf("confirm_order_payment owner=%q security_definer=%v config=%q", owner, securityDefiner, config)
	}
	if err := db.QueryRow(`SELECT has_function_privilege('public','trade.confirm_order_payment(text,text,bigint)','EXECUTE'),
		has_function_privilege('trade_rw','trade.confirm_order_payment(text,text,bigint)','EXECUTE'),
		has_table_privilege('trade_rw','trade.reservation_release_outbox','DELETE')`).Scan(&publicExecute, &tradeExecute, &outboxDelete); err != nil {
		t.Fatal(err)
	}
	if publicExecute || !tradeExecute || outboxDelete {
		t.Fatalf("function public_execute=%v trade_execute=%v outbox_delete=%v", publicExecute, tradeExecute, outboxDelete)
	}
	configPool, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	configPool.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, `SET ROLE trade_rw`)
		return err
	}
	rolePool, err := pgxpool.NewWithConfig(ctx, configPool)
	if err != nil {
		t.Fatal(err)
	}
	defer rolePool.Close()
	assertPGCode(t, func() error {
		_, err := rolePool.Exec(ctx, `INSERT INTO trade.payment_transition_guard VALUES (txid_current(),1)`)
		return err
	}(), "42501")
	assertPGCode(t, func() error { _, err := rolePool.Exec(ctx, `DELETE FROM trade.payment_transition_guard`); return err }(), "42501")
	assertPGCode(t, func() error { _, err := rolePool.Exec(ctx, `DELETE FROM trade.reservation_release_outbox`); return err }(), "42501")
	for _, sequence := range []string{"buyer_order_id_seq", "merchant_order_id_seq", "order_line_id_seq", "order_event_id_seq", "order_event_source_id_seq", "reservation_release_outbox_id_seq"} {
		var update bool
		qualified := "trade." + sequence
		if err := db.QueryRow(`SELECT has_sequence_privilege('trade_rw',$1,'UPDATE')`, qualified).Scan(&update); err != nil {
			t.Fatal(err)
		}
		if update {
			t.Fatalf("trade_rw unexpectedly has UPDATE on %s", qualified)
		}
		assertPGCode(t, func() error { _, err := rolePool.Exec(ctx, `SELECT setval($1::regclass,1)`, qualified); return err }(), "42501")
	}
}

func assertBuyerIsolationAndForeignEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4) VALUES
		('01ARZ3NDEKTSV4RRFFQ69G5FE4',62,'shared-key',repeat('1',64),0),
		('01ARZ3NDEKTSV4RRFFQ69G5FE5',63,'shared-key',repeat('2',64),0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO trade.order_event_source (buyer_order_id,source_type,source_ref)
		SELECT id,'system','foreign-event' FROM trade.buyer_order WHERE public_id='01ARZ3NDEKTSV4RRFFQ69G5FAY'`); err != nil {
		t.Fatal(err)
	}
	assertPGConstraint(t, func() error {
		_, err := pool.Exec(ctx, `INSERT INTO trade.order_event (merchant_order_id,buyer_order_id,from_status,to_status,source_type,source_ref)
			SELECT 1,id,'paid','submitted','system','foreign-event' FROM trade.buyer_order WHERE public_id='01ARZ3NDEKTSV4RRFFQ69G5FAY'`)
		return err
	}(), "23503", "fk_trade_order_event_order")
	assertPGCode(t, func() error {
		_, err := pool.Exec(ctx, `UPDATE trade.buyer_order SET buyer_account_id=999 WHERE public_id='01ARZ3NDEKTSV4RRFFQ69G5FE4'`)
		return err
	}(), "55000")
	assertPGCode(t, func() error {
		_, err := pool.Exec(ctx, `UPDATE trade.buyer_order SET reservation_released_at=clock_timestamp()+interval '1 second' WHERE public_id='01ARZ3NDEKTSV4RRFFQ69G5FC3'`)
		return err
	}(), "55000")
	assertPGCode(t, func() error {
		_, err := pool.Exec(ctx, `UPDATE trade.buyer_order SET refund_required_at=clock_timestamp() WHERE public_id='01ARZ3NDEKTSV4RRFFQ69G5FE4'`)
		return err
	}(), "55000")
	assertPGCode(t, func() error {
		_, err := pool.Exec(ctx, `UPDATE trade.buyer_order SET refund_required_at=NULL WHERE public_id='01ARZ3NDEKTSV4RRFFQ69G5FDK'`)
		return err
	}(), "55000")
}

func assertLineAndParentTotalGuards(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var buyerID, firstID, emptyID int64
	if err := pool.QueryRow(ctx, `WITH buyer AS (
		INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FE6',64,'total-guards',repeat('3',64),100) RETURNING id
	), first_child AS (
		INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FE7',id,664,100 FROM buyer RETURNING id,buyer_order_id
	), empty_child AS (
		INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FE8',buyer_order_id,665,0 FROM first_child RETURNING id
	), line AS (
		INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
		SELECT id,'01ARZ3NDEKTSV4RRFFQ69G5FE9',1,100,100 FROM first_child
	) SELECT buyer_order_id,id,(SELECT id FROM empty_child) FROM first_child`).Scan(&buyerID, &firstID, &emptyID); err != nil {
		t.Fatal(err)
	}
	for name, statement := range map[string]string{
		"update": `UPDATE trade.order_line SET unit_price_e4=200,line_total_e4=200 WHERE merchant_order_id=$1`,
		"delete": `DELETE FROM trade.order_line WHERE merchant_order_id=$1`,
		"move":   `UPDATE trade.order_line SET merchant_order_id=$2 WHERE merchant_order_id=$1`,
	} {
		t.Run("line_total_"+name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			args := []any{firstID}
			if name == "move" {
				args = append(args, emptyID)
			}
			if _, err := tx.Exec(ctx, statement, args...); err != nil {
				t.Fatal(err)
			}
			assertPGCode(t, tx.Commit(ctx), "23514")
		})
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `WITH child AS (
		INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FEA',$1,666,100) RETURNING id
	) INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
	SELECT id,'01ARZ3NDEKTSV4RRFFQ69G5FEB',1,100,100 FROM child`, buyerID); err != nil {
		t.Fatal(err)
	}
	assertPGCode(t, tx.Commit(ctx), "23514")

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `WITH buyer AS (
		INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FEC',67,'whole-cent',repeat('4',64),101) RETURNING id
	), child AS (
		INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FED',id,667,101 FROM buyer RETURNING id
	) INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
	SELECT id,'01ARZ3NDEKTSV4RRFFQ69G5FEE',1,101,101 FROM child`)
	assertPGConstraint(t, err, "23514", "order_line_unit_price_e4_check")
	_ = tx.Rollback(ctx)
}

func assertDifferentPaymentSourcesSerialize(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *PostgresStore) {
	t.Helper()
	if _, err := pool.Exec(ctx, `WITH buyer AS (
		INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FEF',68,'payment-lock',repeat('5',64),100) RETURNING id
	), child AS (
		INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FEG',id,668,100 FROM buyer RETURNING id
	) INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
	SELECT id,'01ARZ3NDEKTSV4RRFFQ69G5FEH',1,100,100 FROM child`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		applied bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, ref := range []string{"payment-lock-a", "payment-lock-b"} {
		ref := ref
		go func() {
			<-start
			_, applied, err := store.ConfirmPayment(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FEF", 100, EventSource{Type: SourceKESSResult, Reference: ref})
			results <- result{applied, err}
		}()
	}
	close(start)
	var applied, reconciliation int
	for range 2 {
		result := <-results
		if result.applied && result.err == nil {
			applied++
		} else if !result.applied && errors.Is(result.err, ErrPaymentReconciliation) {
			reconciliation++
		} else {
			t.Fatalf("unexpected payment result: %+v", result)
		}
	}
	if applied != 1 || reconciliation != 1 {
		t.Fatalf("applied=%d reconciliation=%d, want 1/1", applied, reconciliation)
	}
}

func assertTransitionMatrix(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *PostgresStore) {
	t.Helper()
	statuses := []OrderStatus{AwaitingPayment, Paid, Submitted, Accepted, InFulfillment, Shipped, DeliveredVerified, Settled, Rejected, Cancelled, DeliveryFailed, Returning, Returned, Exception}
	allowed := map[[2]OrderStatus]bool{
		{Paid, Submitted}: true, {Submitted, Accepted}: true, {Submitted, Rejected}: true,
		{Accepted, InFulfillment}: true, {InFulfillment, Shipped}: true,
		{Shipped, DeliveredVerified}: true, {DeliveredVerified, Settled}: true,
	}
	nextID := int64(0)
	for _, from := range statuses {
		for _, to := range statuses {
			nextID++
			merchantID := int64(-1)
			if allowed[[2]OrderStatus{from, to}] {
				parentPublicID, err := ids.NewULID()
				if err != nil {
					t.Fatal(err)
				}
				childPublicID, err := ids.NewULID()
				if err != nil {
					t.Fatal(err)
				}
				if err := pool.QueryRow(ctx, `WITH buyer AS (
					INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4)
					VALUES ($1,$2,$3,repeat('f',64),0) RETURNING id
				) INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,status,total_e4)
				SELECT $4,id,$2,$5,0 FROM buyer RETURNING id`, parentPublicID, 1000+nextID, fmt.Sprintf("matrix-%d", nextID), childPublicID, string(from)).Scan(&merchantID); err != nil {
					t.Fatal(err)
				}
			}
			applied, err := store.Advance(ctx, merchantID, from, to, EventSource{Type: SourceSystem, Reference: fmt.Sprintf("matrix-%d", nextID)})
			if allowed[[2]OrderStatus{from, to}] {
				if err != nil || !applied {
					t.Fatalf("matrix %s -> %s applied=%v err=%v", from, to, applied, err)
				}
			} else if !errors.Is(err, ErrInvalidTransition) || applied {
				t.Fatalf("matrix %s -> %s applied=%v err=%v, want ErrInvalidTransition", from, to, applied, err)
			}
		}
	}
}

func assertPGConstraint(t *testing.T, err error, code, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code || pgErr.ConstraintName != constraint {
		t.Fatalf("PostgreSQL error=%v code=%q constraint=%q, want %s/%s", err, pgErrCode(pgErr), pgErrConstraint(pgErr), code, constraint)
	}
}

func pgErrCode(err *pgconn.PgError) string {
	if err == nil {
		return ""
	}
	return err.Code
}
func pgErrConstraint(err *pgconn.PgError) string {
	if err == nil {
		return ""
	}
	return err.ConstraintName
}
