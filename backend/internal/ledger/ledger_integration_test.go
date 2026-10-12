package ledger_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestLedgerMigrationAndConstraints(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("drlife_ledger_test"),
		postgres.WithUsername("test_admin"),
		postgres.WithPassword("test-only"),
		postgres.BasicWaitStrategies(),
	)
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
	migrations := migrationDirectory(t)
	if err := goose.Up(database, migrations); err != nil {
		t.Fatal(err)
	}

	t.Run("down removes ledger objects and up restores them", func(t *testing.T) {
		if err := goose.DownTo(database, migrations, 3); err != nil {
			t.Fatal(err)
		}
		assertLedgerObjectCount(t, database, 0, 0)
		if err := goose.Up(database, migrations); err != nil {
			t.Fatal(err)
		}
		assertLedgerObjectCount(t, database, 9, 7)
	})

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)

	debitAccount := accountID(t, ctx, admin, "BANK_DEDICATED")
	creditAccount := accountID(t, ctx, admin, "PLATFORM_HELD")

	t.Run("schema amounts and seed state", func(t *testing.T) {
		assertAccountKinds(t, ctx, admin)
		assertAmountColumnsAreBigint(t, ctx, admin)
		var accounts, balances, nonzero, txns, postings int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM ledger.account WHERE owner_account IS NULL`).Scan(&accounts); err != nil {
			t.Fatal(err)
		}
		if err := admin.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE balance <> 0) FROM ledger.balance`).Scan(&balances, &nonzero); err != nil {
			t.Fatal(err)
		}
		if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM ledger.txn), (SELECT count(*) FROM ledger.posting)`).Scan(&txns, &postings); err != nil {
			t.Fatal(err)
		}
		if accounts != 6 || balances != 6 || nonzero != 0 || txns != 0 || postings != 0 {
			t.Fatalf("seed state accounts=%d balances=%d nonzero=%d txns=%d postings=%d", accounts, balances, nonzero, txns, postings)
		}
		seedSingletons(t, ctx, admin)
		var after int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM ledger.account WHERE owner_account IS NULL`).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != 6 {
			t.Fatalf("repeated seed count = %d, want 6", after)
		}
	})

	t.Run("account constraints and automatic balance", func(t *testing.T) {
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.account(kind, normal_side) VALUES ('OTHER', 'CREDIT')`), "23514")
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.account(kind, normal_side) VALUES ('BANK_DEDICATED', 'CREDIT')`), "23514")
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.account(kind, normal_side, owner_account) VALUES ('SUSPENSE', 'CREDIT', 99)`), "23514")
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.account(kind, normal_side, currency) VALUES ('SUSPENSE', 'CREDIT', 'KHR')`), "23514")
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.account(kind, normal_side, status) VALUES ('SUSPENSE', 'CREDIT', 'bad')`), "23514")
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.account(kind, normal_side) VALUES ('SUSPENSE', 'CREDIT')`), "23505")

		wallet := insertOwnedAccount(t, ctx, admin, "WALLET_AVAIL", 1001, false)
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.account(kind, normal_side, owner_account, allow_negative) VALUES ('WALLET_AVAIL', 'CREDIT', 1002, true)`), "23514")
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.account(kind, normal_side, owner_account) VALUES ('WALLET_AVAIL', 'CREDIT', 1001)`), "23505")
		_ = insertOwnedAccount(t, ctx, admin, "AD_BUDGET", 1001, false)
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.account(kind, normal_side, owner_account) VALUES ('AD_BUDGET', 'CREDIT', 1001)`), "23505")
		var balance int64
		if err := admin.QueryRow(ctx, `SELECT balance FROM ledger.balance WHERE account_id=$1`, wallet).Scan(&balance); err != nil || balance != 0 {
			t.Fatalf("automatic balance = %d, err=%v", balance, err)
		}
	})

	t.Run("balanced transaction commits", func(t *testing.T) {
		insertBalanced(t, ctx, admin, "balanced", "balanced", 100, debitAccount, creditAccount, 0)
	})

	t.Run("deferred balance checks reject invalid transactions", func(t *testing.T) {
		cases := []struct {
			name     string
			amount   int64
			postings []posting
		}{
			{"no postings", 100, nil},
			{"one posting", 100, []posting{{debitAccount, "DEBIT", 100, ""}}},
			{"debit and credit differ", 100, []posting{{debitAccount, "DEBIT", 100, ""}, {creditAccount, "CREDIT", 90, ""}}},
			{"totals differ from txn", 110, []posting{{debitAccount, "DEBIT", 100, ""}, {creditAccount, "CREDIT", 100, ""}}},
		}
		for i, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				tx, err := admin.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				var txnID int64
				err = tx.QueryRow(ctx, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,actor_id) VALUES($1,'topup',$2,$3,1) RETURNING id`, fmt.Sprintf("bad-%d", i), tc.amount, fmt.Sprintf("bad-%d", i)).Scan(&txnID)
				if err != nil {
					t.Fatal(err)
				}
				for _, p := range tc.postings {
					if _, err := tx.Exec(ctx, `INSERT INTO ledger.posting(txn_id,account_id,side,amount) VALUES($1,$2,$3,$4)`, txnID, p.accountID, p.side, p.amount); err != nil {
						t.Fatal(err)
					}
				}
				assertSQLState(t, tx.Commit(ctx), "23514")
			})
		}
	})

	t.Run("transaction and posting checks", func(t *testing.T) {
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,actor_id) VALUES('bad-type','unknown',1,'bad-type',1)`), "23514")
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,actor_id) VALUES('bad-amount','topup',0,'bad-amount',1)`), "23514")
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,actor_id) VALUES('dup-idem','topup',1,'balanced',1)`), "23505")

		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var txnID int64
		if err := tx.QueryRow(ctx, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,actor_id) VALUES('posting-check','topup',10,'posting-check',1) RETURNING id`).Scan(&txnID); err != nil {
			t.Fatal(err)
		}
		assertSQLState(t, execErr(ctx, tx, `INSERT INTO ledger.posting(txn_id,account_id,side,amount) VALUES($1,$2,'OTHER',10)`, txnID, debitAccount), "23514")
		_ = tx.Rollback(ctx)
	})

	t.Run("category only applies to platform held", func(t *testing.T) {
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var txnID int64
		if err := tx.QueryRow(ctx, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,actor_id) VALUES('bad-category','topup',10,'bad-category',1) RETURNING id`).Scan(&txnID); err != nil {
			t.Fatal(err)
		}
		assertSQLState(t, execErr(ctx, tx, `INSERT INTO ledger.posting(txn_id,account_id,side,amount,category) VALUES($1,$2,'DEBIT',10,'ads')`, txnID, debitAccount), "23514")
		_ = tx.Rollback(ctx)
		insertWithCategory(t, ctx, admin, debitAccount, creditAccount)
	})

	t.Run("balance permits negative only when configured", func(t *testing.T) {
		wallet := insertOwnedAccount(t, ctx, admin, "WALLET_AVAIL", 2001, false)
		assertSQLState(t, execErr(ctx, admin, `UPDATE ledger.balance SET balance=-1 WHERE account_id=$1`, wallet), "23514")
		assertSQLState(t, execErr(ctx, admin, `UPDATE ledger.balance SET allow_negative=true WHERE account_id=$1`, wallet), "23514")
		suspense := accountID(t, ctx, admin, "SUSPENSE")
		if _, err := admin.Exec(ctx, `UPDATE ledger.balance SET balance=-1 WHERE account_id=$1`, suspense); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("reversal constraints", func(t *testing.T) {
		original := insertBalanced(t, ctx, admin, "original", "original", 70, debitAccount, creditAccount, 0)
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,reverses_txn_id,actor_id) VALUES('mismatch','reversal',71,'mismatch',$1,1)`, original), "23514")
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.txn(id,txn_no,type,amount,idem_key,reverses_txn_id,actor_id) OVERRIDING SYSTEM VALUE VALUES(900000,'self','reversal',70,'self',900000,1)`), "23514")
		insertBalanced(t, ctx, admin, "reversal-one", "reversal-one", 70, creditAccount, debitAccount, original)
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,reverses_txn_id,actor_id) VALUES('reversal-two','reversal',70,'reversal-two',$1,1)`, original), "23505")
	})

	t.Run("supporting table checks", func(t *testing.T) {
		baseTxn := insertBalanced(t, ctx, admin, "support", "support", 50, debitAccount, creditAccount, 0)
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.hold(account_id,ref_type,ref_id,amount,status,freeze_txn_id) VALUES($1,'batch',1,0,'held',$2)`, creditAccount, baseTxn), "23514")
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.hold(account_id,ref_type,ref_id,amount,status,freeze_txn_id) VALUES($1,'batch',1,1,'bad',$2)`, creditAccount, baseTxn), "23514")
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.hold(account_id,ref_type,ref_id,amount,status,freeze_txn_id) VALUES(999999,'batch',1,1,'held',$1)`, baseTxn), "23503")
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.withdrawal(public_id,account_id,amount,payee_account_text,status,maker_actor,checker_actor) VALUES('wd1',1,1,'cipher','review',10,10)`), "23514")
		assertSQLState(t, execErr(ctx, admin, `INSERT INTO ledger.bank_statement_line(bank_ref,booked_on,amount,direction,imported_batch) VALUES('b1',CURRENT_DATE,1,'SIDE','x')`), "23514")
	})

	immutableTxn := insertBalanced(t, ctx, admin, "immutable", "immutable", 25, debitAccount, creditAccount, 0)
	var immutablePosting int64
	if err := admin.QueryRow(ctx, `SELECT id FROM ledger.posting WHERE txn_id=$1 ORDER BY id LIMIT 1`, immutableTxn).Scan(&immutablePosting); err != nil {
		t.Fatal(err)
	}

	t.Run("administrator cannot mutate or truncate journal", func(t *testing.T) {
		for _, statement := range []string{
			fmt.Sprintf(`UPDATE ledger.txn SET note='x' WHERE id=%d`, immutableTxn),
			fmt.Sprintf(`DELETE FROM ledger.txn WHERE id=%d`, immutableTxn),
			fmt.Sprintf(`UPDATE ledger.posting SET role='x' WHERE id=%d`, immutablePosting),
			fmt.Sprintf(`DELETE FROM ledger.posting WHERE id=%d`, immutablePosting),
			`TRUNCATE ledger.posting`,
			fmt.Sprintf(`DELETE FROM ledger.balance WHERE account_id=%d`, debitAccount),
			`TRUNCATE ledger.balance`,
		} {
			assertSQLState(t, execErr(ctx, admin, statement), "55000")
		}
		// PostgreSQL validates an omitted FK-dependent table before firing TRUNCATE triggers.
		assertSQLState(t, execErr(ctx, admin, `TRUNCATE ledger.txn`), "0A000")
		assertSQLState(t, execErr(ctx, admin, `TRUNCATE ledger.txn CASCADE`), "55000")
	})

	ledgerLogin := createLogin(t, ctx, admin, dsn, "test_app_ledger", "ledger_rw")
	defer ledgerLogin.Close(ctx)
	iamLogin := createLogin(t, ctx, admin, dsn, "test_app_iam", "iam_rw")
	defer iamLogin.Close(ctx)
	reportLogin := createLogin(t, ctx, admin, dsn, "test_app_report", "report_ro")
	defer reportLogin.Close(ctx)

	t.Run("application and report permissions", func(t *testing.T) {
		for _, table := range []string{"txn", "posting"} {
			for _, privilege := range []string{"UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
				assertPrivilege(t, ctx, admin, "ledger_rw", "ledger."+table, privilege, false)
			}
			for _, privilege := range []string{"SELECT", "INSERT"} {
				assertPrivilege(t, ctx, admin, "ledger_rw", "ledger."+table, privilege, true)
			}
		}
		for _, statement := range []string{
			fmt.Sprintf(`UPDATE ledger.txn SET note='x' WHERE id=%d`, immutableTxn),
			fmt.Sprintf(`DELETE FROM ledger.txn WHERE id=%d`, immutableTxn),
			fmt.Sprintf(`UPDATE ledger.posting SET role='x' WHERE id=%d`, immutablePosting),
			fmt.Sprintf(`DELETE FROM ledger.posting WHERE id=%d`, immutablePosting),
			`TRUNCATE ledger.txn`,
			`TRUNCATE ledger.posting`,
		} {
			assertSQLState(t, execErr(ctx, ledgerLogin, statement), "42501")
		}
		var appAccount int64
		if err := ledgerLogin.QueryRow(ctx, `INSERT INTO ledger.account(kind,normal_side,owner_account) VALUES('WALLET_AVAIL','CREDIT',4001) RETURNING id`).Scan(&appAccount); err != nil {
			t.Fatalf("app_ledger own-schema insert failed: %v", err)
		}
		if _, err := ledgerLogin.Exec(ctx, `SELECT * FROM ledger.balance WHERE account_id=$1`, appAccount); err != nil {
			t.Fatalf("app_ledger own-schema select failed: %v", err)
		}
		assertSQLState(t, execErr(ctx, ledgerLogin, `DELETE FROM ledger.balance WHERE account_id=$1`, appAccount), "42501")
		assertSQLState(t, execErr(ctx, iamLogin, `SELECT * FROM ledger.account`), "42501")
		for _, table := range []string{"account", "txn", "posting", "balance", "hold", "withdrawal", "outbox", "bank_statement_line", "snapshot_daily"} {
			if _, err := reportLogin.Exec(ctx, `SELECT * FROM ledger.`+table+` LIMIT 0`); err != nil {
				t.Fatalf("report SELECT from %s failed: %v", table, err)
			}
		}
		assertSQLState(t, execErr(ctx, reportLogin, `INSERT INTO ledger.account(kind,normal_side) VALUES('SUSPENSE','CREDIT')`), "42501")
		assertSQLState(t, execErr(ctx, reportLogin, `UPDATE ledger.balance SET balance=0`), "42501")
	})

	t.Run("concurrent balance updates remain nonnegative", func(t *testing.T) {
		wallet := insertOwnedAccount(t, ctx, admin, "WALLET_AVAIL", 3001, false)
		if _, err := admin.Exec(ctx, `UPDATE ledger.balance SET balance=100 WHERE account_id=$1`, wallet); err != nil {
			t.Fatal(err)
		}
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		var successes atomic.Int32
		errCh := make(chan error, 20)
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				tx, err := pool.Begin(ctx)
				if err != nil {
					errCh <- err
					return
				}
				defer tx.Rollback(ctx)
				ids := []int64{debitAccount, wallet}
				sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
				rows, err := tx.Query(ctx, `SELECT account_id FROM ledger.balance WHERE account_id=ANY($1) ORDER BY account_id FOR UPDATE`, ids)
				if err != nil {
					errCh <- err
					return
				}
				rows.Close()
				var txnID int64
				err = tx.QueryRow(ctx, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,actor_id) VALUES($1,'withdraw_req',10,$2,1) RETURNING id`, fmt.Sprintf("concurrent-%d", i), fmt.Sprintf("concurrent-%d", i)).Scan(&txnID)
				if err == nil {
					_, err = tx.Exec(ctx, `INSERT INTO ledger.posting(txn_id,account_id,side,amount) VALUES($1,$2,'DEBIT',10),($1,$3,'CREDIT',10)`, txnID, wallet, debitAccount)
				}
				if err == nil {
					_, err = tx.Exec(ctx, `UPDATE ledger.balance SET balance=balance-10,version=version+1 WHERE account_id=$1`, wallet)
				}
				if err == nil {
					_, err = tx.Exec(ctx, `UPDATE ledger.balance SET balance=balance+10,version=version+1 WHERE account_id=$1`, debitAccount)
				}
				if err == nil {
					err = tx.Commit(ctx)
				}
				if err == nil {
					successes.Add(1)
					return
				}
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
					errCh <- err
				}
			}(i)
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			t.Errorf("concurrent operation: %v", err)
		}
		var balance int64
		if err := admin.QueryRow(ctx, `SELECT balance FROM ledger.balance WHERE account_id=$1`, wallet).Scan(&balance); err != nil {
			t.Fatal(err)
		}
		if successes.Load() != 10 || balance != 0 {
			t.Fatalf("successes=%d balance=%d, want 10 and 0", successes.Load(), balance)
		}
	})
}

type posting struct {
	accountID int64
	side      string
	amount    int64
	category  string
}

type execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func execErr(ctx context.Context, db execer, statement string, args ...any) error {
	_, err := db.Exec(ctx, statement, args...)
	return err
}

func migrationDirectory(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate migrations")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}

func assertLedgerObjectCount(t *testing.T, database *sql.DB, tables, functions int) {
	t.Helper()
	var tableCount, functionCount int
	if err := database.QueryRow(`SELECT count(*) FROM pg_tables WHERE schemaname='ledger'`).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='ledger'`).Scan(&functionCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != tables || functionCount != functions {
		t.Fatalf("ledger tables=%d functions=%d, want %d/%d", tableCount, functionCount, tables, functions)
	}
}

func accountID(t *testing.T, ctx context.Context, conn *pgx.Conn, kind string) int64 {
	t.Helper()
	var id int64
	if err := conn.QueryRow(ctx, `SELECT id FROM ledger.account WHERE kind=$1`, kind).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertOwnedAccount(t *testing.T, ctx context.Context, conn *pgx.Conn, kind string, owner int64, allowNegative bool) int64 {
	t.Helper()
	var id int64
	if err := conn.QueryRow(ctx, `INSERT INTO ledger.account(kind,normal_side,owner_account,allow_negative) VALUES($1,'CREDIT',$2,$3) RETURNING id`, kind, owner, allowNegative).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertBalanced(t *testing.T, ctx context.Context, conn *pgx.Conn, txnNo, idem string, amount, debit, credit, reverses int64) int64 {
	t.Helper()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var id int64
	txnType := "topup"
	var reversal any
	if reverses != 0 {
		txnType = "reversal"
		reversal = reverses
	}
	if err := tx.QueryRow(ctx, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,reverses_txn_id,actor_id) VALUES($1,$2,$3,$4,$5,1) RETURNING id`, txnNo, txnType, amount, idem, reversal).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger.posting(txn_id,account_id,side,amount) VALUES($1,$2,'DEBIT',$4),($1,$3,'CREDIT',$4)`, id, debit, credit, amount); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertWithCategory(t *testing.T, ctx context.Context, conn *pgx.Conn, debit, platform int64) {
	t.Helper()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var txnID int64
	if err := tx.QueryRow(ctx, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,actor_id) VALUES('platform-category','storage_fee',10,'platform-category',1) RETURNING id`).Scan(&txnID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger.posting(txn_id,account_id,side,amount,category) VALUES($1,$2,'DEBIT',10,NULL),($1,$3,'CREDIT',10,'storage')`, txnID, debit, platform); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func assertSQLState(t *testing.T, err error, want string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != want {
		t.Fatalf("error=%v, want SQLSTATE %s", err, want)
	}
}

func assertAccountKinds(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	rows, err := conn.Query(ctx, `SELECT kind,normal_side FROM ledger.account WHERE owner_account IS NULL ORDER BY kind`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var kind, side string
		if err := rows.Scan(&kind, &side); err != nil {
			t.Fatal(err)
		}
		got[kind] = side
	}
	want := map[string]string{"BANK_DEDICATED": "DEBIT", "PENDING_PAYOUT": "CREDIT", "DELIVERY_ESCROW": "CREDIT", "PLATFORM_HELD": "CREDIT", "PAYABLE_CARRIER": "CREDIT", "SUSPENSE": "CREDIT"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("seed kinds=%v, want %v", got, want)
	}
}

func assertAmountColumnsAreBigint(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	rows, err := conn.Query(ctx, `SELECT table_name,column_name,data_type FROM information_schema.columns WHERE table_schema='ledger' AND (column_name LIKE '%amount%' OR column_name='balance')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var table, column, dataType string
		if err := rows.Scan(&table, &column, &dataType); err != nil {
			t.Fatal(err)
		}
		count++
		if dataType != "bigint" {
			t.Errorf("%s.%s type=%s, want bigint", table, column, dataType)
		}
	}
	if count == 0 {
		t.Fatal("no amount columns found")
	}
}

func seedSingletons(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	_, err := conn.Exec(ctx, `INSERT INTO ledger.account(kind,normal_side,allow_negative) VALUES
		('BANK_DEDICATED','DEBIT',false),('PENDING_PAYOUT','CREDIT',false),('DELIVERY_ESCROW','CREDIT',false),
		('PLATFORM_HELD','CREDIT',false),('PAYABLE_CARRIER','CREDIT',false),('SUSPENSE','CREDIT',true)
		ON CONFLICT ON CONSTRAINT uq_account_kind_owner DO NOTHING`)
	if err != nil {
		t.Fatal(err)
	}
}

func createLogin(t *testing.T, ctx context.Context, admin *pgx.Conn, dsn, login, permission string) *pgx.Conn {
	t.Helper()
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE ROLE %s LOGIN PASSWORD 'test-only'`, pgx.Identifier{login}.Sanitize())); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf(`GRANT %s TO %s`, pgx.Identifier{permission}.Sanitize(), pgx.Identifier{login}.Sanitize())); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword(login, "test-only")
	conn, err := pgx.Connect(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func assertPrivilege(t *testing.T, ctx context.Context, conn *pgx.Conn, role, table, privilege string, want bool) {
	t.Helper()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT has_table_privilege($1,$2,$3)`, role, table, privilege).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("has_table_privilege(%s,%s,%s)=%v, want %v", role, table, privilege, got, want)
	}
}
