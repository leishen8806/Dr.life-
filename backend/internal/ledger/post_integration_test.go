package ledger_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPostIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("drlife_post_test"), postgres.WithUsername("test_admin"),
		postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate postgres: %v", err)
		}
	})
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
	if err := goose.Up(database, migrationDirectory(t)); err != nil {
		t.Fatal(err)
	}
	adminConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	adminConfig.MaxConns = 64
	admin, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, `CREATE ROLE app_ledger LOGIN PASSWORD 'test-only'; GRANT ledger_rw TO app_ledger`); err != nil {
		t.Fatal(err)
	}
	app := openRolePool(t, ctx, dsn, "app_ledger")
	defer app.Close()
	adminService := ledger.New(admin, ledger.Options{})
	appService := ledger.New(app, ledger.Options{})

	t.Run("migration hardens accounts and table permissions", func(t *testing.T) {
		var bank int64
		if err := admin.QueryRow(ctx, `SELECT id FROM ledger.account WHERE kind='BANK_DEDICATED'`).Scan(&bank); err != nil {
			t.Fatal(err)
		}
		assertState(t, execError(ctx, app, `UPDATE ledger.account SET kind='SUSPENSE' WHERE id=$1`, bank), "42501")
		assertState(t, execError(ctx, admin, `UPDATE ledger.account SET kind='SUSPENSE' WHERE id=$1`, bank), "55000")
		if _, err := app.Exec(ctx, `UPDATE ledger.account SET status='closed' WHERE id=$1`, bank); err != nil {
			t.Fatalf("allowed status update: %v", err)
		}
		if _, err := app.Exec(ctx, `UPDATE ledger.account SET status='open' WHERE id=$1`, bank); err != nil {
			t.Fatalf("restore status: %v", err)
		}
		assertState(t, execError(ctx, app, `DELETE FROM ledger.account WHERE id=$1`, bank), "42501")
		assertState(t, execError(ctx, admin, `DELETE FROM ledger.account WHERE id=$1`, bank), "55000")
		assertState(t, execError(ctx, admin, `TRUNCATE ledger.account CASCADE`), "55000")
		for _, table := range []string{"account", "hold", "withdrawal", "outbox", "bank_statement_line", "snapshot_daily"} {
			for _, privilege := range []string{"DELETE", "TRUNCATE"} {
				var allowed bool
				if err := admin.QueryRow(ctx, `SELECT has_table_privilege('ledger_rw',$1,$2)`, "ledger."+table, privilege).Scan(&allowed); err != nil {
					t.Fatal(err)
				}
				if allowed {
					t.Errorf("ledger_rw unexpectedly has %s on %s", privilege, table)
				}
			}
		}
		for _, privilege := range []string{"REFERENCES", "TRIGGER"} {
			var allowed bool
			if err := admin.QueryRow(ctx, `SELECT has_table_privilege('ledger_rw','ledger.account',$1)`, privilege).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed {
				t.Errorf("ledger_rw unexpectedly has %s on account", privilege)
			}
		}
	})

	t.Run("migration down and up round trip", func(t *testing.T) {
		if err := goose.DownTo(database, migrationDirectory(t), 4); err != nil {
			t.Fatal(err)
		}
		var exists bool
		if err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='ledger' AND p.proname='protect_account')`).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatal("protect_account survived Down")
		}
		if err := goose.Up(database, migrationDirectory(t)); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("post follows normal side and replay is exact", func(t *testing.T) {
		source := createWallet(t, ctx, admin, 101, 10, false)
		destination := createWallet(t, ctx, admin, 102, 0, false)
		request := transfer("settle:order:101", source, destination, 4)
		first, err := adminService.Post(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^L[0-9]{12}$`).MatchString(first.TxnNo) {
			t.Fatalf("txn_no=%q", first.TxnNo)
		}
		replayed, err := adminService.Post(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if replayed.TxnID != first.TxnID || replayed.TxnNo != first.TxnNo || !replayed.Replayed {
			t.Fatalf("replay=%+v first=%+v", replayed, first)
		}
		byID, err := adminService.GetByID(ctx, first.TxnID)
		if err != nil || byID.TxnNo != first.TxnNo {
			t.Fatalf("GetByID=%+v error=%v", byID, err)
		}
		byKey, err := adminService.GetByIdemKey(ctx, request.IdemKey)
		if err != nil || byKey.TxnID != first.TxnID {
			t.Fatalf("GetByIdemKey=%+v error=%v", byKey, err)
		}
		if got := getBalance(t, ctx, admin, source); got != 6 {
			t.Fatalf("source balance=%d, want 6", got)
		}
		changed := request
		changed.Amount = 3
		changed.Postings = []ledger.Posting{{AccountID: source, Side: "DEBIT", Amount: 3}, {AccountID: destination, Side: "CREDIT", Amount: 3}}
		if _, err := adminService.Post(ctx, changed); !errors.Is(err, ledger.ErrIdempotencyConflict) {
			t.Fatalf("content conflict error=%v", err)
		}
		assertTxnCounts(t, ctx, admin, request.IdemKey, 1, 2, 1)
	})

	t.Run("replay survives outbox cleanup", func(t *testing.T) {
		source := createWallet(t, ctx, admin, 104, 10, false)
		destination := createWallet(t, ctx, admin, 105, 0, false)
		request := transfer("settle:outbox-cleanup:104", source, destination, 4)
		first, err := adminService.Post(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `DELETE FROM ledger.outbox WHERE txn_id=$1`, first.TxnID); err != nil {
			t.Fatal(err)
		}
		for name, load := range map[string]func() (ledger.PostResult, error){
			"Post":         func() (ledger.PostResult, error) { return adminService.Post(ctx, request) },
			"GetByID":      func() (ledger.PostResult, error) { return adminService.GetByID(ctx, first.TxnID) },
			"GetByIdemKey": func() (ledger.PostResult, error) { return adminService.GetByIdemKey(ctx, request.IdemKey) },
		} {
			t.Run(name, func(t *testing.T) {
				got, err := load()
				if err != nil {
					t.Fatal(err)
				}
				if got.TxnID != first.TxnID || got.TxnNo != first.TxnNo || !got.Replayed || len(got.Balances) != 0 {
					t.Fatalf("result=%+v", got)
				}
			})
		}
		if getBalance(t, ctx, admin, source) != 6 {
			t.Fatal("replay changed balance")
		}
		assertTxnCounts(t, ctx, admin, request.IdemKey, 1, 2, 0)
	})

	t.Run("debit-normal accounts increase on debit", func(t *testing.T) {
		bank := singletonID(t, ctx, admin, "BANK_DEDICATED")
		wallet := createWallet(t, ctx, admin, 103, 0, false)
		request := ledger.PostRequest{Type: "topup", Amount: 5, IdemKey: "topup:deposit:103", ActorID: 1,
			Postings: []ledger.Posting{{AccountID: bank, Side: "DEBIT", Amount: 5}, {AccountID: wallet, Side: "CREDIT", Amount: 5}}}
		if _, err := adminService.Post(ctx, request); err != nil {
			t.Fatal(err)
		}
		if getBalance(t, ctx, admin, bank) != 5 || getBalance(t, ctx, admin, wallet) != 5 {
			t.Fatalf("bank/wallet balances=%d/%d", getBalance(t, ctx, admin, bank), getBalance(t, ctx, admin, wallet))
		}
	})

	t.Run("one hundred concurrent retries post once", func(t *testing.T) {
		source := createWallet(t, ctx, admin, 201, 10, false)
		destination := createWallet(t, ctx, admin, 202, 0, false)
		request := transfer("settle:order:201", source, destination, 1)
		var fresh atomic.Int64
		var wg sync.WaitGroup
		errs := make(chan error, 100)
		ids := make(chan int64, 100)
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := adminService.Post(ctx, request)
				if err != nil {
					errs <- err
					return
				}
				if !result.Replayed {
					fresh.Add(1)
				}
				ids <- result.TxnID
			}()
		}
		wg.Wait()
		close(errs)
		close(ids)
		for err := range errs {
			t.Error(err)
		}
		var id int64
		for got := range ids {
			if id == 0 {
				id = got
			} else if got != id {
				t.Errorf("replay txn=%d, want %d", got, id)
			}
		}
		if fresh.Load() != 1 || getBalance(t, ctx, admin, source) != 9 {
			t.Fatalf("fresh=%d balance=%d", fresh.Load(), getBalance(t, ctx, admin, source))
		}
		assertTxnCounts(t, ctx, admin, request.IdemKey, 1, 2, 1)
	})

	t.Run("one thousand concurrent debits never overdraw", func(t *testing.T) {
		source := createWallet(t, ctx, admin, 301, 100, false)
		destination := createWallet(t, ctx, admin, 302, 0, false)
		started := time.Now()
		latencies := make([]time.Duration, 1000)
		var successes atomic.Int64
		var insufficient atomic.Int64
		var wg sync.WaitGroup
		errs := make(chan error, 1000)
		for i := range 1000 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				begin := time.Now()
				_, err := adminService.Post(ctx, transfer(fmt.Sprintf("settle:load:%d", i), source, destination, 1))
				latencies[i] = time.Since(begin)
				switch {
				case err == nil:
					successes.Add(1)
				case errors.Is(err, ledger.ErrInsufficientFunds):
					insufficient.Add(1)
				default:
					errs <- err
				}
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		elapsed := time.Since(started)
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		p95 := latencies[949]
		t.Logf("PERF 1000 requests: elapsed=%s throughput=%.1f req/s p95=%s machine=%s/%s cpu=%d", elapsed, 1000/elapsed.Seconds(), p95, runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
		if successes.Load() != 100 || insufficient.Load() != 900 || getBalance(t, ctx, admin, source) != 0 {
			t.Fatalf("success=%d insufficient=%d balance=%d", successes.Load(), insufficient.Load(), getBalance(t, ctx, admin, source))
		}
		assertJournalBalanced(t, ctx, admin)
	})

	t.Run("cross account and three account postings do not deadlock", func(t *testing.T) {
		a := createWallet(t, ctx, admin, 401, 1000, false)
		b := createWallet(t, ctx, admin, 402, 1000, false)
		c := createWallet(t, ctx, admin, 403, 0, false)
		var wg sync.WaitGroup
		errs := make(chan error, 200)
		for i := range 200 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				source, destination := a, b
				if i%2 == 1 {
					source, destination = b, a
				}
				request := ledger.PostRequest{Type: "settle", Amount: 2, IdemKey: fmt.Sprintf("settle:cross:%d", i), ActorID: 1,
					Postings: []ledger.Posting{{AccountID: source, Side: "DEBIT", Amount: 2}, {AccountID: destination, Side: "CREDIT", Amount: 1}, {AccountID: c, Side: "CREDIT", Amount: 1}}}
				if _, err := adminService.Post(ctx, request); err != nil {
					errs <- err
				}
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "40P01" {
				t.Errorf("unhandled deadlock: %v", err)
			} else {
				t.Error(err)
			}
		}
	})

	t.Run("database rejects amount mismatch without residue", func(t *testing.T) {
		a := createWallet(t, ctx, admin, 501, 10, false)
		b := createWallet(t, ctx, admin, 502, 0, false)
		assertRawRejected(t, ctx, admin, "raw-amount-mismatch", 100, 200, a, b)
		assertRawRejected(t, ctx, admin, "raw-unbalanced", 100, 99, a, b)
		request := transfer("settle:mismatch:post", a, b, 2)
		request.Amount = 1
		if _, err := adminService.Post(ctx, request); !errors.Is(err, ledger.ErrUnbalanced) {
			t.Fatalf("Post mismatch error=%v", err)
		}
	})

	t.Run("negative policy and invalid accounts", func(t *testing.T) {
		suspense := singletonID(t, ctx, admin, "SUSPENSE")
		destination := createWallet(t, ctx, admin, 601, 0, false)
		if _, err := adminService.Post(ctx, transfer("bank_diff:item:601", suspense, destination, 5)); err != nil {
			t.Fatal(err)
		}
		if getBalance(t, ctx, admin, suspense) != -5 {
			t.Fatal("negative-enabled SUSPENSE did not go negative")
		}
		request := transfer("settle:missing:601", destination, 999999999, 1)
		if _, err := adminService.Post(ctx, request); !errors.Is(err, ledger.ErrAccountNotFound) {
			t.Fatalf("missing account error=%v", err)
		}
	})

	t.Run("closed category and overflow checks roll back", func(t *testing.T) {
		source := createWallet(t, ctx, admin, 602, 10, false)
		destination := createWallet(t, ctx, admin, 603, 0, false)
		if _, err := admin.Exec(ctx, `UPDATE ledger.account SET status='closed' WHERE id=$1`, source); err != nil {
			t.Fatal(err)
		}
		if _, err := adminService.Post(ctx, transfer("settle:closed:602", source, destination, 1)); !errors.Is(err, ledger.ErrAccountClosed) {
			t.Fatalf("closed account error=%v", err)
		}
		if _, err := admin.Exec(ctx, `UPDATE ledger.account SET status='open' WHERE id=$1`, source); err != nil {
			t.Fatal(err)
		}
		categoryRequest := transfer("settle:category:602", source, destination, 1)
		categoryRequest.Postings[1].Category = "storage"
		if _, err := adminService.Post(ctx, categoryRequest); !errors.Is(err, ledger.ErrInvalidRequest) {
			t.Fatalf("category error=%v", err)
		}
		if _, err := admin.Exec(ctx, `UPDATE ledger.balance SET balance=$2 WHERE account_id=$1`, destination, int64(^uint64(0)>>1)); err != nil {
			t.Fatal(err)
		}
		if _, err := adminService.Post(ctx, transfer("settle:overflow:602", source, destination, 1)); !errors.Is(err, ledger.ErrInvalidAmount) {
			t.Fatalf("overflow error=%v", err)
		}
		assertTxnCounts(t, ctx, admin, "settle:overflow:602", 0, 0, 0)
	})

	t.Run("outbox failure rolls back app transaction", func(t *testing.T) {
		source := createWallet(t, ctx, admin, 701, 10, false)
		destination := createWallet(t, ctx, admin, 702, 0, false)
		if _, err := admin.Exec(ctx, `REVOKE INSERT ON ledger.outbox FROM ledger_rw`); err != nil {
			t.Fatal(err)
		}
		_, postErr := appService.Post(ctx, transfer("settle:rollback:701", source, destination, 1))
		if _, err := admin.Exec(ctx, `GRANT INSERT ON ledger.outbox TO ledger_rw`); err != nil {
			t.Fatal(err)
		}
		if postErr == nil {
			t.Fatal("Post succeeded without outbox INSERT permission")
		}
		if getBalance(t, ctx, admin, source) != 10 {
			t.Fatal("balance changed after outbox failure")
		}
		assertTxnCounts(t, ctx, admin, "settle:rollback:701", 0, 0, 0)
	})

	t.Run("app role posts and outbox workers skip locked", func(t *testing.T) {
		if _, err := admin.Exec(ctx, `UPDATE ledger.outbox SET sent_at=now() WHERE sent_at IS NULL`); err != nil {
			t.Fatal(err)
		}
		source := createWallet(t, ctx, admin, 801, 10, false)
		destination := createWallet(t, ctx, admin, 802, 0, false)
		for i := range 2 {
			if _, err := appService.Post(ctx, transfer(fmt.Sprintf("settle:outbox:%d", i), source, destination, 1)); err != nil {
				t.Fatal(err)
			}
		}
		first, err := appService.ClaimOutbox(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		defer first.Release(context.Background())
		second, err := appService.ClaimOutbox(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		defer second.Release(context.Background())
		if len(first.Events()) != 1 || len(second.Events()) != 1 || first.Events()[0].ID == second.Events()[0].ID {
			t.Fatalf("claimed first=%v second=%v", first.Events(), second.Events())
		}
		if err := first.MarkSent(ctx); err != nil {
			t.Fatal(err)
		}
		if err := second.MarkSent(ctx); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("journal remains immutable for app role", func(t *testing.T) {
		assertState(t, execError(ctx, app, `UPDATE ledger.txn SET note='blocked'`), "42501")
		assertState(t, execError(ctx, app, `DELETE FROM ledger.posting`), "42501")
	})
}

func openRolePool(t *testing.T, ctx context.Context, dsn, login string) *pgxpool.Pool {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword(login, "test-only")
	config, err := pgxpool.ParseConfig(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 16
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func createWallet(t *testing.T, ctx context.Context, admin *pgxpool.Pool, owner, balance int64, allowNegative bool) int64 {
	t.Helper()
	var id int64
	if err := admin.QueryRow(ctx, `INSERT INTO ledger.account(kind,normal_side,owner_account,allow_negative) VALUES('WALLET_AVAIL','CREDIT',$1,$2) RETURNING id`, owner, allowNegative).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `UPDATE ledger.balance SET balance=$2 WHERE account_id=$1`, id, balance); err != nil {
		t.Fatal(err)
	}
	return id
}

func singletonID(t *testing.T, ctx context.Context, admin *pgxpool.Pool, kind string) int64 {
	t.Helper()
	var id int64
	if err := admin.QueryRow(ctx, `SELECT id FROM ledger.account WHERE kind=$1`, kind).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func transfer(idem string, source, destination, amount int64) ledger.PostRequest {
	return ledger.PostRequest{Type: "settle", Amount: amount, IdemKey: idem, ActorID: 1,
		Postings: []ledger.Posting{{AccountID: source, Side: "DEBIT", Amount: amount}, {AccountID: destination, Side: "CREDIT", Amount: amount}}}
}

func getBalance(t *testing.T, ctx context.Context, db *pgxpool.Pool, account int64) int64 {
	t.Helper()
	var balance int64
	if err := db.QueryRow(ctx, `SELECT balance FROM ledger.balance WHERE account_id=$1`, account).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	return balance
}

func assertTxnCounts(t *testing.T, ctx context.Context, db *pgxpool.Pool, idem string, txns, postings, outbox int) {
	t.Helper()
	var gotTxn, gotPosting, gotOutbox int
	if err := db.QueryRow(ctx, `SELECT count(*),COALESCE(sum((SELECT count(*) FROM ledger.posting p WHERE p.txn_id=t.id)),0),COALESCE(sum((SELECT count(*) FROM ledger.outbox o WHERE o.txn_id=t.id)),0) FROM ledger.txn t WHERE idem_key=$1`, idem).Scan(&gotTxn, &gotPosting, &gotOutbox); err != nil {
		t.Fatal(err)
	}
	if gotTxn != txns || gotPosting != postings || gotOutbox != outbox {
		t.Fatalf("counts txn/posting/outbox=%d/%d/%d, want %d/%d/%d", gotTxn, gotPosting, gotOutbox, txns, postings, outbox)
	}
}

func assertRawRejected(t *testing.T, ctx context.Context, db *pgxpool.Pool, key string, txnAmount, postingAmount, debit, credit int64) {
	t.Helper()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var txnID int64
	if err := tx.QueryRow(ctx, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,actor_id) VALUES($1,'settle',$2,$1,1) RETURNING id`, key, txnAmount).Scan(&txnID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger.posting(txn_id,account_id,side,amount) VALUES($1,$2,'DEBIT',$4),($1,$3,'CREDIT',$4)`, txnID, debit, credit, postingAmount); err != nil {
		t.Fatal(err)
	}
	err = tx.Commit(ctx)
	assertState(t, err, "23514")
	assertTxnCounts(t, ctx, db, key, 0, 0, 0)
}

func assertJournalBalanced(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	var bad int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM (SELECT txn_id FROM ledger.posting GROUP BY txn_id HAVING sum(amount) FILTER(WHERE side='DEBIT') <> sum(amount) FILTER(WHERE side='CREDIT')) q`).Scan(&bad); err != nil {
		t.Fatal(err)
	}
	if bad != 0 {
		t.Fatalf("unbalanced transactions=%d", bad)
	}
}

type sqlExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func execError(ctx context.Context, db sqlExecer, statement string, args ...any) error {
	_, err := db.Exec(ctx, statement, args...)
	return err
}

func assertState(t *testing.T, err error, state string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != state {
		t.Fatalf("error=%v, want SQLSTATE %s", err, state)
	}
}
