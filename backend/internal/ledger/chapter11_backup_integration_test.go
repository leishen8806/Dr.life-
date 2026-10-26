package ledger_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestChapter11BackupRestoreIntegration(t *testing.T) {
	e := newBusinessEnv(t)
	wallet := mustWallet(t, e, 98_001, nil)
	if _, err := e.appService.Topup(e.ctx, ledger.TopupInput{
		BankStatementLineID: 98_001, BankConfirmed: true, WalletAccountID: wallet.Available, Amount: 1_000, ActorID: 1,
	}); err != nil {
		t.Fatal(err)
	}
	before := readBackupState(t, e.admin, e.ctx, wallet.Available)

	requireContainerCommand(t, e.ctx, e.container, []string{"pg_dump", "-U", "test_admin", "-d", "drlife_business_test", "-Fc", "-f", "/tmp/ledger.dump"})
	dumpFile, err := e.container.CopyFileFromContainer(e.ctx, "/tmp/ledger.dump")
	if err != nil {
		t.Fatal(err)
	}
	dump, err := io.ReadAll(dumpFile)
	_ = dumpFile.Close()
	if err != nil {
		t.Fatal(err)
	}

	restored, err := postgres.Run(e.ctx, "postgres:16-alpine",
		postgres.WithDatabase("drlife_restore_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Terminate(e.ctx) })
	createRestoreRoles(t, e.ctx, restored)
	if err := restored.CopyToContainer(e.ctx, dump, "/tmp/ledger.dump", 0o600); err != nil {
		t.Fatal(err)
	}
	requireContainerCommand(t, e.ctx, restored, []string{"pg_restore", "-U", "test_admin", "-d", "drlife_restore_test", "--exit-on-error", "/tmp/ledger.dump"})

	dsn, err := restored.ConnectionString(e.ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(e.ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	run, err := ledger.New(pool, ledger.Options{}).RunDailyReconciliation(e.ctx, time.Now(), ledger.ReconciliationOptions{})
	if err != nil || run.Status != "ok" || len(run.Issues) != 0 {
		t.Fatalf("reconciliation after restore: run=%+v err=%v", run, err)
	}
	if after := readBackupState(t, pool, e.ctx, wallet.Available); after != before {
		t.Fatalf("restored state=%+v want=%+v", after, before)
	}
	assertAccountingInvariant(t, businessEnv{ctx: e.ctx, admin: pool})
}

type backupState struct {
	bankBalance   int64
	walletBalance int64
	transactions  int64
	postings      int64
}

func readBackupState(t *testing.T, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, ctx context.Context, walletID int64) backupState {
	t.Helper()
	var state backupState
	err := db.QueryRow(ctx, `
		SELECT
			(SELECT b.balance FROM ledger.balance b JOIN ledger.account a ON a.id=b.account_id WHERE a.kind='BANK_DEDICATED'),
			(SELECT balance FROM ledger.balance WHERE account_id=$1),
			(SELECT count(*) FROM ledger.txn),
			(SELECT count(*) FROM ledger.posting)`, walletID).
		Scan(&state.bankBalance, &state.walletBalance, &state.transactions, &state.postings)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func createRestoreRoles(t *testing.T, ctx context.Context, container testcontainers.Container) {
	t.Helper()
	roles := []string{"iam_rw", "catalog_rw", "trade_rw", "wms_rw", "logistics_rw", "ledger_rw", "comms_rw", "ai_rw", "ops_rw", "audit_writer", "report_ro"}
	statements := make([]string, len(roles))
	for i, role := range roles {
		statements[i] = "CREATE ROLE " + role
	}
	requireContainerCommand(t, ctx, container, []string{"psql", "-U", "test_admin", "-d", "drlife_restore_test", "-v", "ON_ERROR_STOP=1", "-c", strings.Join(statements, ";")})
}

func requireContainerCommand(t *testing.T, ctx context.Context, container testcontainers.Container, command []string) {
	t.Helper()
	code, output, err := container.Exec(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	result, err := io.ReadAll(output)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("%v exited %d: %s", command, code, result)
	}
}
