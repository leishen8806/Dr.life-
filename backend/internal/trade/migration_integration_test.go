package trade

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestTradeOrderMigration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	c, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("trade_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil { t.Fatal(err) }
	db, err := sql.Open("pgx", dsn)
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := goose.SetDialect("postgres"); err != nil { t.Fatal(err) }
	_, file, _, _ := runtime.Caller(0)
	migrations := filepath.Join(filepath.Dir(file), "..", "..", "migrations")
	if err := goose.Up(db, migrations); err != nil { t.Fatal(err) }
	assertTradeTables(t, db, true)
	if err := goose.DownTo(db, migrations, 52); err != nil { t.Fatal(err) }
	assertTradeTables(t, db, false)
	if err := goose.Up(db, migrations); err != nil { t.Fatal(err) }
	assertTradeTables(t, db, true)
}

func assertTradeTables(t *testing.T, db *sql.DB, want bool) {
	t.Helper()
	for _, table := range []string{"buyer_order", "merchant_order", "order_line", "order_event"} {
		var exists bool
		if err := db.QueryRow(`SELECT to_regclass('trade.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil { t.Fatal(err) }
		if exists != want { t.Fatalf("trade.%s exists=%v, want %v", table, exists, want) }
	}
}
