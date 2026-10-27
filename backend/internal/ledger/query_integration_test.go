package ledger_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// L-09a 的读模型必须在真实 PostgreSQL 上验证：钱包查询只能返回当前账户的分录，
// 且查询服务使用 report 只读连接池而非可写 ledger 连接。
func TestReadServiceWalletTransactionsDoNotLeakOtherOwners(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("drlife_query_test"),
		postgres.WithUsername("test_admin"),
		postgres.WithPassword("test-only"),
		postgres.BasicWaitStrategies(),
	)
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
	t.Cleanup(func() { _ = database.Close() })
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(database, queryMigrationDirectory(t)); err != nil {
		t.Fatal(err)
	}

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	ownerWallet := insertQueryWallet(t, ctx, admin, 101)
	otherWallet := insertQueryWallet(t, ctx, admin, 202)
	bank := queryAccountID(t, ctx, admin, "BANK_DEDICATED")
	insertQueryTxn(t, ctx, admin, "TXN-OWNER", ownerWallet, bank, 1200)
	insertQueryTxn(t, ctx, admin, "TXN-OTHER", otherWallet, bank, 3400)

	report, err := pgxpool.New(ctx, dsn+"&default_transaction_read_only=on")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(report.Close)

	reader := ledger.NewReadService(report)
	page, err := reader.ListWalletTransactions(ctx, ledger.WalletTransactionQuery{OwnerAccountID: 101, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].TxnNo != "TXN-OWNER" {
		t.Fatalf("wallet rows = %+v, want only the owner's transaction", page.Items)
	}
	if page.Items[0].AmountE4 != 1200 {
		t.Fatalf("amount = %d, want integer e4 amount", page.Items[0].AmountE4)
	}
}

func TestReadServiceWalletTransactionsUsesKeysetCursor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("drlife_query_cursor_test"),
		postgres.WithUsername("test_admin"),
		postgres.WithPassword("test-only"),
		postgres.BasicWaitStrategies(),
	)
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
	t.Cleanup(func() { _ = database.Close() })
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(database, queryMigrationDirectory(t)); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	wallet := insertQueryWallet(t, ctx, admin, 303)
	bank := queryAccountID(t, ctx, admin, "BANK_DEDICATED")
	insertQueryTxn(t, ctx, admin, "TXN-CURSOR-OLD", wallet, bank, 100)
	insertQueryTxn(t, ctx, admin, "TXN-CURSOR-NEW", wallet, bank, 200)

	reader := ledger.NewReadService(admin)
	first, err := reader.ListWalletTransactions(ctx, ledger.WalletTransactionQuery{OwnerAccountID: 303, PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first page = %+v, want one row and a cursor", first)
	}
	second, err := reader.ListWalletTransactions(ctx, ledger.WalletTransactionQuery{OwnerAccountID: 303, PageSize: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].TxnNo == first.Items[0].TxnNo || second.NextCursor != "" {
		t.Fatalf("second page = %+v, want the remaining transaction", second)
	}
}

func TestReadServiceAdminTransactionIncludesAllPostings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("drlife_query_detail_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db, queryMigrationDirectory(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	wallet := insertQueryWallet(t, ctx, pool, 404)
	bank := queryAccountID(t, ctx, pool, "BANK_DEDICATED")
	insertQueryTxn(t, ctx, pool, "TXN-DETAIL", wallet, bank, 900)
	item, found, err := ledger.NewReadService(pool).GetAdminTransaction(ctx, "TXN-DETAIL")
	if err != nil || !found {
		t.Fatalf("detail found=%v err=%v", found, err)
	}
	if len(item.Postings) != 2 {
		t.Fatalf("postings=%+v, want both entries", item.Postings)
	}
}

func queryMigrationDirectory(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate migrations")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}

func queryAccountID(t *testing.T, ctx context.Context, database *pgxpool.Pool, kind string) int64 {
	t.Helper()
	var id int64
	if err := database.QueryRow(ctx, `SELECT id FROM ledger.account WHERE kind=$1`, kind).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertQueryWallet(t *testing.T, ctx context.Context, database *pgxpool.Pool, owner int64) int64 {
	t.Helper()
	var id int64
	if err := database.QueryRow(ctx, `INSERT INTO ledger.account(kind,normal_side,owner_account) VALUES('WALLET_AVAIL','CREDIT',$1) RETURNING id`, owner).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertQueryTxn(t *testing.T, ctx context.Context, database *pgxpool.Pool, txnNo string, wallet, bank, amount int64) {
	t.Helper()
	tx, err := database.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var txnID int64
	if err := tx.QueryRow(ctx, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,actor_id) VALUES($1,'topup',$2,$3,1) RETURNING id`, txnNo, amount, "query:"+txnNo+":1").Scan(&txnID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger.posting(txn_id,account_id,side,amount) VALUES($1,$2,'CREDIT',$3),($1,$4,'DEBIT',$3)`, txnID, wallet, amount, bank); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
