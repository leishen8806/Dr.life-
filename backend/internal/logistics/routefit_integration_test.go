package logistics

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestRouteFitEventRecorderCommitsInboxAndDedupAtomically(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("routefit_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	if err := goose.Up(database, routeFitMigrationDirectory(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `CREATE TABLE ops.routefit_effect (event_id text PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	handler := func(ctx context.Context, tx pgx.Tx, event RouteFitEvent) error {
		_, err := tx.Exec(ctx, `INSERT INTO ops.routefit_effect (event_id) VALUES ($1)`, event.EventID)
		return err
	}
	recorder := RouteFitEventRecorder{DB: pool, Handlers: map[string]RouteFitEventHandler{"fulfillment.shipped": handler}}
	first := routeFitIntegrationEvent("evt-1", 2)
	if err := recorder.Record(ctx, first, []byte(`{"one":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Record(ctx, first, []byte(`{"one":1}`)); err != nil {
		t.Fatal(err)
	}
	assertRouteFitCount(t, ctx, pool, `SELECT count(*) FROM ops.processed_event WHERE consumer='routefit'`, 1)
	assertRouteFitCount(t, ctx, pool, `SELECT count(*) FROM logistics.shipment_event`, 1)
	assertRouteFitCount(t, ctx, pool, `SELECT count(*) FROM ops.routefit_effect`, 1)

	stale := routeFitIntegrationEvent("evt-2", 1)
	if err := recorder.Record(ctx, stale, []byte(`{"two":2}`)); err != nil {
		t.Fatal(err)
	}
	assertRouteFitCount(t, ctx, pool, `SELECT count(*) FROM logistics.shipment_event WHERE processing_state='stale'`, 1)
	assertRouteFitCount(t, ctx, pool, `SELECT count(*) FROM ops.routefit_effect`, 1)

	failing := RouteFitEventRecorder{DB: pool, Handlers: map[string]RouteFitEventHandler{"fulfillment.shipped": func(context.Context, pgx.Tx, RouteFitEvent) error {
		return errors.New("handler failure")
	}}}
	if err := failing.Record(ctx, routeFitIntegrationEvent("evt-3", 3), []byte(`{"three":3}`)); err == nil {
		t.Fatal("Record() succeeded after handler failure")
	}
	assertRouteFitCount(t, ctx, pool, `SELECT count(*) FROM ops.processed_event WHERE event_id='evt-3'`, 0)
	assertRouteFitCount(t, ctx, pool, `SELECT count(*) FROM logistics.shipment_event WHERE event_id='evt-3'`, 0)
}

func routeFitIntegrationEvent(eventID string, version int64) RouteFitEvent {
	return RouteFitEvent{EventID: eventID, EventType: "fulfillment.shipped", SchemaVersion: 1, OccurredAt: time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC), Aggregate: RouteFitAggregate{Type: "fulfillment", ID: "ful-1", Version: version}, Payload: []byte(`{}`)}
}

func assertRouteFitCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, query).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("count=%d, want %d", got, want)
	}
}

func routeFitMigrationDirectory(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate migration directory")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}
