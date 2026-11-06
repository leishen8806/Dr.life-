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
		err := kess.NewWorker(store, &client, 24*time.Hour).Create(ctx, intent.ID)
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

	t.Run("module and report roles have narrowed privileges", func(t *testing.T) {
		var canMutateReceivable, canRecordStatus, canDelete, canTruncate, reportReadsSeller, canUpdateSequence bool
		if err := pool.QueryRow(ctx, `
			SELECT has_column_privilege('trade_rw', 'trade.kess_payment_intent', 'amount_e4', 'UPDATE'),
			       has_column_privilege('trade_rw', 'trade.kess_payment_intent', 'status', 'UPDATE'),
			       has_table_privilege('trade_rw', 'trade.kess_payment_intent', 'DELETE'),
			       has_table_privilege('trade_rw', 'trade.kess_payment_intent', 'TRUNCATE'),
			       has_column_privilege('report_ro', 'trade.kess_payment_intent', 'seller_code', 'SELECT'),
			       has_sequence_privilege('trade_rw', 'trade.kess_payment_intent_id_seq', 'UPDATE')`).Scan(&canMutateReceivable, &canRecordStatus, &canDelete, &canTruncate, &reportReadsSeller, &canUpdateSequence); err != nil {
			t.Fatal(err)
		}
		if canMutateReceivable || !canRecordStatus || canDelete || canTruncate || reportReadsSeller || canUpdateSequence {
			t.Fatalf("privileges amount=%v status=%v delete=%v truncate=%v seller=%v sequence_update=%v", canMutateReceivable, canRecordStatus, canDelete, canTruncate, reportReadsSeller, canUpdateSequence)
		}
		var publicPrivileges int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.table_privileges WHERE table_schema = 'trade' AND table_name = 'kess_payment_intent' AND grantee = 'PUBLIC'`).Scan(&publicPrivileges); err != nil {
			t.Fatal(err)
		}
		if publicPrivileges != 0 {
			t.Fatalf("PUBLIC privileges = %d, want 0", publicPrivileges)
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
		_, err = pool.Exec(ctx, `UPDATE trade.kess_payment_intent SET status = 'SUCCESS' WHERE public_id = $1`, pendingID)
		assertPGError(t, err, "P0001", "invalid kess payment intent status transition")

		const successID = "01ARZ3NDEKTSV4RRFFQ69G5FAX"
		_, err = pool.Exec(ctx, `INSERT INTO trade.kess_payment_intent (public_id, company_id, channel, trade_no, source_ref, attempt_no, amount_e4, currency, seller_code, status) VALUES ($1, 12, 'kess', $2, $1, 1, 100, 'USD', 'seller', 'SUCCESS')`, successID, "DRL-"+successID+"-1")
		assertPGError(t, err, "P0001", "invalid initial kess payment intent state")
		if _, err := pool.Exec(ctx, `INSERT INTO trade.kess_payment_intent (public_id, company_id, channel, trade_no, source_ref, attempt_no, amount_e4, currency, seller_code, status) VALUES ($1, 12, 'kess', $2, $1, 1, 100, 'USD', 'seller', 'PENDING')`, successID, "DRL-"+successID+"-1"); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE trade.kess_payment_intent SET status = 'WAITING' WHERE public_id = $1`, successID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE trade.kess_payment_intent SET status = 'SUCCESS' WHERE public_id = $1`, successID); err != nil {
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
