package db_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
	platformdb "github.com/leishen8806/Dr.life-/backend/internal/platform/db"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/observability"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPoolsApplySettingsAndKeepLedgerTimeoutLocal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("drlife_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		DatabaseURL: dsn, ReportDatabaseURL: dsn,
		DBMaxConns: 2, DBReportMaxConns: 3, DBWorkerMaxConns: 4,
		DBConnectTimeout: time.Second, DBAcquireTimeout: time.Second,
	}
	pools := []struct {
		name string
		max  int32
		open func(context.Context, config.Config) (*pgxpool.Pool, error)
		want string
	}{
		{
			"oltp", 2,
			func(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
				return platformdb.NewPool(ctx, cfg)
			},
			"5s",
		},
		{
			"report", 3,
			func(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
				return platformdb.NewReadOnlyPool(ctx, cfg)
			},
			"30s",
		},
		{
			"worker", 4,
			func(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
				return platformdb.NewWorkerPool(ctx, cfg)
			},
			"1min",
		},
	}
	for _, test := range pools {
		t.Run(test.name, func(t *testing.T) {
			pool, err := test.open(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			if got := pool.Config().MaxConns; got != test.max {
				t.Fatalf("MaxConns = %d, want %d", got, test.max)
			}
			conn, err := platformdb.Acquire(ctx, pool, cfg.DBAcquireTimeout)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Release()
			assertSetting(t, ctx, conn, "statement_timeout", test.want)
			assertSetting(t, ctx, conn, "lock_timeout", "3s")
			assertSetting(t, ctx, conn, "idle_in_transaction_session_timeout", "10s")
		})
	}

	oltp, err := platformdb.NewPool(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer oltp.Close()
	tx, err := oltp.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '15s'"); err != nil {
		t.Fatal(err)
	}
	var statementTimeout string
	if err := tx.QueryRow(ctx, "SHOW statement_timeout").Scan(&statementTimeout); err != nil {
		t.Fatal(err)
	}
	if statementTimeout != "15s" {
		t.Fatalf("transaction statement_timeout = %q, want 15s", statementTimeout)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := platformdb.Acquire(ctx, oltp, cfg.DBAcquireTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	assertSetting(t, ctx, conn, "statement_timeout", "5s")
}

func TestAcquireTimeoutIsRetryable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("drlife_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := platformdb.NewPool(ctx, config.Config{DatabaseURL: dsn, DBMaxConns: 1, DBConnectTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	_, err = platformdb.Acquire(context.Background(), pool, 20*time.Millisecond)
	if !errors.Is(err, platformdb.ErrAcquireTimeout) {
		t.Fatalf("Acquire error = %v, want retryable timeout", err)
	}
}

func TestPoolMetricsAreLabeledByPool(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://localhost:1/drlife?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	metrics := observability.NewMetrics()
	metrics.RegisterPool("oltp", pool)
	metrics.RegisterPool("report", pool)
	metrics.RegisterPool("worker", pool)
	response := httptest.NewRecorder()
	observability.MetricsHandler(metrics).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, name := range []string{"oltp", "report", "worker"} {
		if !strings.Contains(response.Body.String(), `drlife_db_pool_acquired_connections{pool="`+name+`"}`) {
			t.Fatalf("pool metric missing low-cardinality label %q: %s", name, response.Body.String())
		}
	}
}

func assertSetting(t *testing.T, ctx context.Context, conn *pgxpool.Conn, setting, want string) {
	t.Helper()
	var got string
	if err := conn.QueryRow(ctx, "SHOW "+setting).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", setting, got, want)
	}
}
