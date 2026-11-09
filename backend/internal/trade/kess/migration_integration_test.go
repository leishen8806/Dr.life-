package kess_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
	"github.com/leishen8806/Dr.life-/backend/internal/trade"
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
	for i, receivable := range []struct {
		publicID string
		amountE4 int64
	}{
		{"01ARZ3NDEKTSV4RRFFQ69G5FAT", 123400},
		{"01ARZ3NDEKTSV4RRFFQ69G5FAV", 100},
		{"01ARZ3NDEKTSV4RRFFQ69G5FAW", 100},
		{"01ARZ3NDEKTSV4RRFFQ69G5FAX", 100},
		{"01ARZ3NDEKTSV4RRFFQ69G5FAY", 10000},
		{"01ARZ3NDEKTSV4RRFFQ69G5FAZ", 10000},
		{"01ARZ3NDEKTSV4RRFFQ69G5FB0", 10000},
		{"01ARZ3NDEKTSV4RRFFQ69G5FB1", 10000},
	} {
		seedKESSReceivableOrder(t, database, receivable.publicID, receivable.amountE4, int64(i+1))
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tradeConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = tradeConn.Exec(ctx, `RESET ROLE`)
		tradeConn.Release()
	}()
	if _, err := tradeConn.Exec(ctx, `SET ROLE trade_rw`); err != nil {
		t.Fatal(err)
	}
	store := kess.NewPgStore(tradeConn)
	const sourceRef = "01ARZ3NDEKTSV4RRFFQ69G5FAT"
	intent, err := store.CreateIntent(ctx, kess.ReceivableSnapshot{CompanyID: 9, SourceRef: sourceRef, AttemptNo: 1, AmountE4: 123400, Currency: "USD", SellerCode: "seller-test"})
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := store.CreateIntent(ctx, kess.ReceivableSnapshot{CompanyID: 9, SourceRef: sourceRef, AttemptNo: 1, AmountE4: 123400, Currency: "USD", SellerCode: "seller-test"})
	if err != nil || repeated.ID != intent.ID || repeated.TradeNo != intent.TradeNo {
		t.Fatalf("idempotent CreateIntent = %+v, %v; want id=%d trade=%s", repeated, err, intent.ID, intent.TradeNo)
	}
	if _, err := store.CreateIntent(ctx, kess.ReceivableSnapshot{CompanyID: 9, SourceRef: sourceRef, AttemptNo: 2, AmountE4: 123401, Currency: "USD", SellerCode: "seller-test"}); !errors.Is(err, kess.ErrInvalidAmount) {
		t.Fatalf("sub-cent CreateIntent error = %v, want ErrInvalidAmount", err)
	}
	if _, err := store.CreateIntent(ctx, kess.ReceivableSnapshot{CompanyID: 9, SourceRef: sourceRef, AttemptNo: 2, AmountE4: 10000, Currency: "KHR", SellerCode: "seller-test"}); !errors.Is(err, kess.ErrInvalidAmount) {
		t.Fatalf("KHR CreateIntent error = %v, want ErrInvalidAmount", err)
	}

	t.Run("outbound timeout sees persisted intent first", func(t *testing.T) {
		client := persistedThenTimeout{pool: pool, intentID: intent.ID}
		worker, err := kess.NewWorker(store, &client, 24*time.Hour, "devwebpayment.kesspay.io")
		if err != nil {
			t.Fatal(err)
		}
		err = worker.Create(ctx, intent.ID)
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
				publicID := "01ARZ3NDEKTSV4RRFFQ69G5FA" + string(rune('B'+i))
				_, err := pool.Exec(ctx, `INSERT INTO trade.kess_payment_intent (public_id, company_id, channel, trade_no, source_ref, attempt_no, amount_e4, currency, seller_code, status) VALUES ($1, 11, 'kess', $2, '01ARZ3NDEKTSV4RRFFQ69G5FAV', 1, 100, 'USD', 'seller', 'PENDING')`, publicID, "DRL-"+publicID+"-1")
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
			if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "ux_kess_payment_intent_company_source_attempt" {
				duplicate++
				continue
			}
			t.Fatalf("concurrent insert error = %v", err)
		}
		if success != 1 || duplicate != 1 {
			t.Fatalf("success=%d duplicate=%d, want 1/1", success, duplicate)
		}
	})

	t.Run("due scan leases one intent across concurrent trade workers", func(t *testing.T) {
		due, err := store.CreateIntent(ctx, kess.ReceivableSnapshot{CompanyID: 9, SourceRef: "01ARZ3NDEKTSV4RRFFQ69G5FB1", AttemptNo: 1, AmountE4: 10000, Currency: "USD", SellerCode: "seller-test"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE trade.kess_payment_intent SET last_checked_at=now()-interval '2 minutes' WHERE id=$1`, due.ID); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		results := make(chan []kess.Intent, 2)
		errs := make(chan error, 2)
		scanAt := time.Now()
		for range 2 {
			go func() {
				conn, err := pool.Acquire(ctx)
				if err != nil {
					errs <- err
					return
				}
				defer conn.Release()
				if _, err := conn.Exec(ctx, `SET ROLE trade_rw`); err != nil {
					errs <- err
					return
				}
				defer func() { _, _ = conn.Exec(ctx, `RESET ROLE`) }()
				<-start
				items, err := kess.NewPgStore(conn).ListDue(ctx, scanAt, 100)
				results <- items
				errs <- err
			}()
		}
		close(start)
		matches := 0
		for range 2 {
			for _, item := range <-results {
				if item.ID == due.ID {
					matches++
				}
			}
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
		if matches != 1 {
			t.Fatalf("due intent claimed %d times, want 1", matches)
		}
	})

	t.Run("invalidated late success creates one manual reconciliation task", func(t *testing.T) {
		const orderID = "01ARZ3NDEKTSV4RRFFQ69G5FB0"
		intent, err := store.CreateIntent(ctx, kess.ReceivableSnapshot{CompanyID: 9, SourceRef: orderID, AttemptNo: 1, AmountE4: 10000, Currency: "USD", SellerCode: "seller-test"})
		if err != nil {
			t.Fatal(err)
		}
		tradeStore := trade.NewPostgresStore(pool)
		if _, claimed, err := tradeStore.ClaimBuyerReservationRelease(ctx, 7, orderID); err != nil || !claimed {
			t.Fatalf("cancel claimed=%v err=%v", claimed, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE trade.kess_payment_intent SET last_checked_at=now()+interval '1 hour'`); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE trade.kess_payment_intent SET last_checked_at=now()-interval '2 minutes' WHERE id=$1`, intent.ID); err != nil {
			t.Fatal(err)
		}
		client := &lateSuccessClient{transactionID: "late-kess-transaction"}
		worker, err := kess.NewWorker(kess.NewPgStore(pool), client, 24*time.Hour, "devwebpayment.kesspay.io")
		if err != nil {
			t.Fatal(err)
		}
		service := trade.NewService(tradeStore, kessAccounts{}, kessCatalog{})
		maintenance, err := kess.NewMaintenance(kess.NewPgStore(pool), worker, service)
		if err != nil {
			t.Fatal(err)
		}
		if err := maintenance.ProcessDue(ctx, 100); err != nil {
			t.Fatal(err)
		}
		if err := maintenance.ProcessDue(ctx, 100); err != nil {
			t.Fatal(err)
		}
		var status string
		var tasks, paid int
		if err := pool.QueryRow(ctx, `SELECT status FROM trade.kess_payment_intent WHERE id=$1`, intent.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM trade.payment_reconciliation_task WHERE source_ref=$1`, client.transactionID).Scan(&tasks); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM trade.merchant_order child JOIN trade.buyer_order buyer ON buyer.id=child.buyer_order_id WHERE buyer.public_id=$1 AND child.status='paid'`, orderID).Scan(&paid); err != nil {
			t.Fatal(err)
		}
		if status != "SUCCESS" || tasks != 1 || paid != 0 || client.queries != 1 {
			t.Fatalf("status=%s tasks=%d paid=%d queries=%d, want SUCCESS/1/0/1", status, tasks, paid, client.queries)
		}
	})

	t.Run("module and report roles have narrowed privileges", func(t *testing.T) {
		var canMutateReceivable, canRecordStatus, canRecordCreateSent, canDelete, canTruncate, reportReadsSeller, canUpdateSequence bool
		if err := pool.QueryRow(ctx, `
			SELECT has_column_privilege('trade_rw', 'trade.kess_payment_intent', 'amount_e4', 'UPDATE'),
			       has_column_privilege('trade_rw', 'trade.kess_payment_intent', 'status', 'UPDATE'),
			       has_column_privilege('trade_rw', 'trade.kess_payment_intent', 'create_sent_at', 'UPDATE'),
			       has_table_privilege('trade_rw', 'trade.kess_payment_intent', 'DELETE'),
			       has_table_privilege('trade_rw', 'trade.kess_payment_intent', 'TRUNCATE'),
			       has_column_privilege('report_ro', 'trade.kess_payment_intent', 'seller_code', 'SELECT'),
			       has_sequence_privilege('trade_rw', 'trade.kess_payment_intent_id_seq', 'UPDATE')`).Scan(&canMutateReceivable, &canRecordStatus, &canRecordCreateSent, &canDelete, &canTruncate, &reportReadsSeller, &canUpdateSequence); err != nil {
			t.Fatal(err)
		}
		if canMutateReceivable || !canRecordStatus || !canRecordCreateSent || canDelete || canTruncate || reportReadsSeller || canUpdateSequence {
			t.Fatalf("privileges amount=%v status=%v create_sent=%v delete=%v truncate=%v seller=%v sequence_update=%v", canMutateReceivable, canRecordStatus, canRecordCreateSent, canDelete, canTruncate, reportReadsSeller, canUpdateSequence)
		}
		var publicPrivileges int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.table_privileges WHERE table_schema = 'trade' AND table_name = 'kess_payment_intent' AND grantee = 'PUBLIC'`).Scan(&publicPrivileges); err != nil {
			t.Fatal(err)
		}
		if publicPrivileges != 0 {
			t.Fatalf("PUBLIC privileges = %d, want 0", publicPrivileges)
		}
	})

	t.Run("expired create lease becomes unknown without a second create claim", func(t *testing.T) {
		stale, err := store.CreateIntent(ctx, kess.ReceivableSnapshot{CompanyID: 9, SourceRef: "01ARZ3NDEKTSV4RRFFQ69G5FAZ", AttemptNo: 1, AmountE4: 10000, Currency: "USD", SellerCode: "seller-test"})
		if err != nil {
			t.Fatal(err)
		}
		if _, claimed, err := store.ClaimPending(ctx, stale.ID); err != nil || !claimed {
			t.Fatalf("initial claim claimed=%v error=%v", claimed, err)
		}
		var claimedAt time.Time
		if err := tradeConn.QueryRow(ctx, `UPDATE trade.kess_payment_intent SET create_claimed_at = now() - interval '6 minutes' WHERE id = $1 RETURNING create_claimed_at`, stale.ID).Scan(&claimedAt); err != nil {
			t.Fatal(err)
		}
		got, claimed, err := store.ClaimPending(ctx, stale.ID)
		if err != nil || claimed || got.Status != kess.StatusUnknown || got.CreateAttempts != 1 || got.LastCreateSentAt == nil || got.LastCreateSentAt.Sub(claimedAt).Abs() > time.Second {
			t.Fatalf("expired claim = %+v claimed=%v error=%v; want UNKNOWN, unclaimed, one attempt and sent time", got, claimed, err)
		}
	})

	t.Run("claim is exclusive and channel update is conditional", func(t *testing.T) {
		claim, err := store.CreateIntent(ctx, kess.ReceivableSnapshot{CompanyID: 9, SourceRef: "01ARZ3NDEKTSV4RRFFQ69G5FAY", AttemptNo: 1, AmountE4: 10000, Currency: "USD", SellerCode: "seller-test"})
		if err != nil {
			t.Fatal(err)
		}
		first, claimed, err := store.ClaimPending(ctx, claim.ID)
		if err != nil || !claimed {
			t.Fatalf("first claim = %+v, %v, %v", first, claimed, err)
		}
		if _, claimed, err := store.ClaimPending(ctx, claim.ID); err != nil || claimed {
			t.Fatalf("second claim = %v, %v; want unclaimed", claimed, err)
		}
		if err := store.RecordChannelResult(ctx, claim.ID, kess.StatusPending, kess.ChannelResult{Status: kess.StatusWaiting, PaymentURL: "https://devwebpayment.kesspay.io/pay/keep"}); err != nil {
			t.Fatal(err)
		}
		if err := store.RecordChannelResult(ctx, claim.ID, kess.StatusPending, kess.ChannelResult{Status: kess.StatusUnknown}); !errors.Is(err, kess.ErrConflict) {
			t.Fatalf("stale conditional update error = %v, want ErrConflict", err)
		}
		if err := store.RecordChannelResult(ctx, claim.ID, kess.StatusWaiting, kess.ChannelResult{Status: kess.StatusUnknown}); err != nil {
			t.Fatal(err)
		}
		var paymentURL string
		if err := pool.QueryRow(ctx, `SELECT payment_url FROM trade.kess_payment_intent WHERE id = $1`, claim.ID).Scan(&paymentURL); err != nil || paymentURL != "https://devwebpayment.kesspay.io/pay/keep" {
			t.Fatalf("payment_url = %q, %v; want preserved", paymentURL, err)
		}
	})

	t.Run("receivable facts and status transitions are guarded", func(t *testing.T) {
		_, err := pool.Exec(ctx, `UPDATE trade.kess_payment_intent SET amount_e4 = amount_e4 + 100 WHERE id = $1`, intent.ID)
		assertPGError(t, err, "P0001", "kess payment intent receivable facts are immutable")

		const pendingID = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
		if _, err := pool.Exec(ctx, `INSERT INTO trade.kess_payment_intent (public_id, company_id, channel, trade_no, source_ref, attempt_no, amount_e4, currency, seller_code, status) VALUES ($1, 12, 'kess', $2, $1, 1, 100, 'USD', 'seller', 'PENDING')`, pendingID, "DRL-"+pendingID+"-1"); err != nil {
			t.Fatal(err)
		}
		_, err = tradeConn.Exec(ctx, `UPDATE trade.kess_payment_intent SET status = 'SUCCESS' WHERE public_id = $1`, pendingID)
		assertPGError(t, err, "P0001", "invalid kess payment intent status transition")

		const successID = "01ARZ3NDEKTSV4RRFFQ69G5FAX"
		_, err = tradeConn.Exec(ctx, `INSERT INTO trade.kess_payment_intent (public_id, company_id, channel, trade_no, source_ref, attempt_no, amount_e4, currency, seller_code, status) VALUES ($1, 12, 'kess', $2, $1, 1, 100, 'USD', 'seller', 'SUCCESS')`, successID, "DRL-"+successID+"-1")
		assertPGError(t, err, "P0001", "invalid initial kess payment intent state")
		if _, err := pool.Exec(ctx, `INSERT INTO trade.kess_payment_intent (public_id, company_id, channel, trade_no, source_ref, attempt_no, amount_e4, currency, seller_code, status) VALUES ($1, 12, 'kess', $2, $1, 1, 100, 'USD', 'seller', 'PENDING')`, successID, "DRL-"+successID+"-1"); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE trade.kess_payment_intent SET status = 'WAITING' WHERE public_id = $1`, successID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE trade.kess_payment_intent SET status = 'REVIEW' WHERE public_id = $1`, successID); err != nil {
			t.Fatal(err)
		}
		_, err = tradeConn.Exec(ctx, `UPDATE trade.kess_payment_intent SET status = 'CLOSED' WHERE public_id = $1`, successID)
		assertPGError(t, err, "P0001", "invalid kess payment intent status transition")
		if _, err := pool.Exec(ctx, `UPDATE trade.kess_payment_intent SET status = 'SUCCESS' WHERE public_id = $1`, successID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE trade.kess_payment_intent SET status = 'REFUND' WHERE public_id = $1`, successID); err != nil {
			t.Fatalf("SUCCESS -> REFUND: %v", err)
		}
	})
}

func seedKESSReceivableOrder(t *testing.T, db *sql.DB, publicID string, amountE4, sequence int64) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var buyerOrderID int64
	if err := tx.QueryRow(`INSERT INTO trade.buyer_order
		(public_id,buyer_account_id,idempotency_key,request_hash,total_e4,goods_amount_e4,shipping_amount_e4,payable_amount_e4,shipping_quote_expires_at)
		VALUES($1,$2,$3,$4,$5,$5,0,$5,now()+interval '1 hour') RETURNING id`,
		publicID, sequence, "kess-fixture-"+publicID, fmt.Sprintf("%064x", sequence), amountE4).Scan(&buyerOrderID); err != nil {
		t.Fatal(err)
	}
	var merchantOrderID int64
	if err := tx.QueryRow(`INSERT INTO trade.merchant_order
		(public_id,buyer_order_id,merchant_id,merchant_public_id,total_e4,goods_amount_e4,shipping_amount_e4,payable_amount_e4,shipping_quote_expires_at)
		VALUES($1,$2,$3,$4,$5,$5,0,$5,now()+interval '1 hour') RETURNING id`,
		newKESSFixtureULID(t), buyerOrderID, sequence, "merchant-fixture", amountE4).Scan(&merchantOrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO trade.order_line
		(merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4) VALUES($1,$2,1,$3,$3)`, merchantOrderID, newKESSFixtureULID(t), amountE4); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func assertPGError(t *testing.T, err error, code, message string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code || pgErr.Message != message {
		t.Fatalf("error = %v, want PostgreSQL %s %q", err, code, message)
	}
}

func newKESSFixtureULID(t *testing.T) string {
	t.Helper()
	value, err := ids.NewULID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type persistedThenTimeout struct {
	pool     *pgxpool.Pool
	intentID int64
	seen     bool
}

type lateSuccessClient struct {
	transactionID string
	queries       int
}

func (*lateSuccessClient) Create(context.Context, kess.Intent) (kess.ChannelOrder, error) {
	return kess.ChannelOrder{}, errors.New("unexpected KESS create")
}

func (c *lateSuccessClient) Query(_ context.Context, intent kess.Intent) (kess.ChannelOrder, error) {
	c.queries++
	return kess.ChannelOrder{TradeNo: intent.TradeNo, SellerCode: intent.ExpectedSellerCode, AmountE4: intent.AmountE4, Currency: intent.Currency, Status: "SUCCESS", TransactionID: c.transactionID}, nil
}

type kessAccounts struct{}

func (kessAccounts) Status(context.Context, int64) (string, error) { return "active", nil }

type kessCatalog struct{}

func (kessCatalog) ResolveAndReserve(context.Context, string, []trade.OrderLineInput) ([]trade.CatalogLine, error) {
	return nil, errors.New("unexpected catalog reservation")
}
func (kessCatalog) Release(context.Context, string) error { return nil }
func (kessCatalog) Commit(context.Context, string) error  { return nil }

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
