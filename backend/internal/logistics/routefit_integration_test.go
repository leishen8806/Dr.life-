package logistics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/leishen8806/Dr.life-/backend/internal/logistics/routefit"
	"github.com/leishen8806/Dr.life-/backend/internal/testutil/routefitmock"
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

	sensitive := routeFitIntegrationEvent("evt-4", 4)
	if err := recorder.Record(ctx, sensitive, []byte(`{"trackingNo":"TRACK-1","recipientPhone":"+85510000001","unexpected":"do-not-store"}`)); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := pool.QueryRow(ctx, `SELECT row_to_json(shipment_event)::text FROM logistics.shipment_event WHERE event_id='evt-4'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "+85510000001") || strings.Contains(stored, "do-not-store") {
		t.Fatalf("shipment inbox retained non-whitelisted payload: %s", stored)
	}
}

func TestBuildFulfillmentCommandUsesActiveSKUMap(t *testing.T) {
	e := newRouteFitIntegrationEnvironment(t)
	ctx := e.ctx
	const categoryID = 991
	const productID = 992
	const skuID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	const buyerID = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	const merchantOrderID = "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO catalog.category (id, public_id, code, name) VALUES ($1,$2,$3,$4)`, []any{categoryID, "01ARZ3NDEKTSV4RRFFQ69G5FAY", "sku_map_test", `{"zh":"测试","en":"Test","km":"សាកល្បង"}`}},
		{`INSERT INTO catalog.product (id, public_id, merchant_id, category_id, name, unit, origin, status) VALUES ($1,$2,7,$3,$4,'piece','cn','on')`, []any{productID, "01ARZ3NDEKTSV4RRFFQ69G5FAZ", categoryID, `{"zh":"测试","en":"Test","km":"សាកល្បង"}`}},
		{`INSERT INTO catalog.sku (public_id, product_id, status) VALUES ($1,$2,'on')`, []any{skuID, productID}},
		{`INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4,goods_amount_e4,payable_amount_e4) VALUES ($1,7,'sku-map-test',repeat('a',64),100,100,100)`, []any{buyerID}},
		{`INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,merchant_public_id,total_e4,goods_amount_e4,payable_amount_e4) VALUES ($1,(SELECT id FROM trade.buyer_order WHERE public_id=$2),7,'merchant-public',100,100,100)`, []any{merchantOrderID, buyerID}},
		{`INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4) VALUES ((SELECT id FROM trade.merchant_order WHERE public_id=$1),$2,2,50,100)`, []any{merchantOrderID, skuID}},
		{`INSERT INTO logistics.sku_map (product_id,routefit_sku_id,warehouse_id,unit_ratio,state) VALUES ($1,'routefit-sku','warehouse-1',2,'active')`, []any{productID}},
	}
	for _, statement := range statements {
		if _, err := e.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	command, err := BuildFulfillmentCommandTx(ctx, tx, merchantOrderID, "")
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if command.ExternalOrderID != "DRL-"+merchantOrderID || command.OwnerRef != "merchant-public" || command.WarehouseID != "warehouse-1" || command.ServiceLevel != "S2" || len(command.Lines) != 1 || command.Lines[0].SKU != "routefit-sku" || command.Lines[0].Quantity != 4 {
		t.Fatalf("unexpected fulfillment command: %+v", command)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE logistics.sku_map SET state='disabled' WHERE product_id=$1`, productID); err != nil {
		t.Fatal(err)
	}
	tx, err = e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := EnqueuePaidFulfillmentTx(ctx, tx, merchantOrderID, "S1")
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if !missing {
		_ = tx.Rollback(ctx)
		t.Fatal("disabled SKU mapping was accepted")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestFulfillmentOutboxWorkerDispatchesRouteFitMock(t *testing.T) {
	e := newRouteFitIntegrationEnvironment(t)
	mock := routefitmock.New(routefitmock.Config{})
	mock.AddOwner(routefitmock.Owner{OwnerRef: "merchant-1", WarehouseIDs: []string{"warehouse-1"}})
	mock.AddSKU("merchant-1", routefitmock.SKU{ID: "sku-1", State: "active"}, "warehouse-1", 10)
	server := httptest.NewServer(mock.Handler())
	t.Cleanup(server.Close)
	client, err := routefit.New(routefit.Config{BaseURL: server.URL, ServiceToken: "test-service-token", Timeout: 2 * time.Second, RetryAttempts: 2, RetryInitialDelay: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	command := routefit.CreateShipmentCmd{
		ExternalOrderID:         "order-outbox-1",
		OwnerRef:                "merchant-1",
		WarehouseID:             "warehouse-1",
		Lines:                   []routefit.Line{{Ref: "line-1", SKU: "sku-1", Quantity: 1}},
		Delivery:                json.RawMessage(`{"address":"test"}`),
		RequestedDeliveryWindow: json.RawMessage(`{"from":"2026-11-18T08:00:00Z","to":"2026-11-18T10:00:00Z"}`),
		ServiceLevel:            "S1",
		DeclaredValue:           routefit.Money{AmountMinor: 100, Currency: "USD"},
	}
	tx, err := e.pool.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnqueueFulfillmentTx(e.ctx, tx, command); err != nil {
		_ = tx.Rollback(e.ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(e.ctx); err != nil {
		t.Fatal(err)
	}

	worker := FulfillmentOutboxWorker{DB: e.pool, Client: client, RetryDelay: time.Millisecond}
	if err := worker.RunOnce(e.ctx); err != nil {
		t.Fatal(err)
	}
	var status, fulfillmentID, storedKey string
	var attempts int
	if err := e.pool.QueryRow(e.ctx, `SELECT status, fulfillment_id, idempotency_key, attempts FROM logistics.fulfillment_outbox WHERE external_order_id=$1`, command.ExternalOrderID).Scan(&status, &fulfillmentID, &storedKey, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "delivered" || fulfillmentID == "" || attempts != 1 {
		t.Fatalf("outbox status=%q fulfillment=%q attempts=%d", status, fulfillmentID, attempts)
	}
	if storedKey != fulfillmentIdempotencyKey(command.ExternalOrderID) {
		t.Fatalf("stored idempotency key=%q does not match stable key", storedKey)
	}
	if _, err := client.Deliver(e.ctx, command); err != nil {
		t.Fatal(err)
	}
	var fulfillmentCount int
	if err := e.pool.QueryRow(e.ctx, `SELECT count(*) FROM logistics.fulfillment_outbox WHERE external_order_id=$1`, command.ExternalOrderID).Scan(&fulfillmentCount); err != nil {
		t.Fatal(err)
	}
	if fulfillmentCount != 1 {
		t.Fatalf("outbox rows=%d, want 1", fulfillmentCount)
	}

	recoveredCommand := command
	recoveredCommand.ExternalOrderID = "order-outbox-recovered"
	tx, err = e.pool.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnqueueFulfillmentTx(e.ctx, tx, recoveredCommand); err != nil {
		_ = tx.Rollback(e.ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(e.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(e.ctx, `UPDATE logistics.fulfillment_outbox SET status='processing', attempts=1, updated_at=clock_timestamp()-interval '1 hour' WHERE external_order_id=$1`, recoveredCommand.ExternalOrderID); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(e.ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(e.ctx, `SELECT status, attempts FROM logistics.fulfillment_outbox WHERE external_order_id=$1`, recoveredCommand.ExternalOrderID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "delivered" || attempts != 2 {
		t.Fatalf("recovered outbox status=%q attempts=%d", status, attempts)
	}

	maxAttemptCommand := command
	maxAttemptCommand.ExternalOrderID = "order-outbox-max-attempts"
	tx, err = e.pool.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnqueueFulfillmentTx(e.ctx, tx, maxAttemptCommand); err != nil {
		_ = tx.Rollback(e.ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(e.ctx); err != nil {
		t.Fatal(err)
	}
	mock.SetFault(routefitmock.Fault{Status: 500, Count: 5})
	maxAttemptWorker := worker
	maxAttemptWorker.MaxAttempts = 1
	if err := maxAttemptWorker.RunOnce(e.ctx); err != nil {
		t.Fatal(err)
	}
	mock.SetFault(routefitmock.Fault{})
	var errorCode string
	if err := e.pool.QueryRow(e.ctx, `SELECT status, last_error_code FROM logistics.fulfillment_outbox WHERE external_order_id=$1`, maxAttemptCommand.ExternalOrderID).Scan(&status, &errorCode); err != nil {
		t.Fatal(err)
	}
	if status != "exception" || errorCode != "MAX_ATTEMPTS_EXCEEDED" {
		t.Fatalf("max-attempt outbox status=%q error=%q", status, errorCode)
	}
}

func TestRouteFitEventRecorderAcceptsOnlyOneConcurrentFirstAggregateVersion(t *testing.T) {
	e := newRouteFitIntegrationEnvironment(t)
	var handled atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	recorder := RouteFitEventRecorder{DB: e.pool, Handlers: map[string]RouteFitEventHandler{"fulfillment.shipped": func(context.Context, pgx.Tx, RouteFitEvent) error {
		if handled.Add(1) == 1 {
			close(started)
		}
		<-release
		return nil
	}}}

	errs := make(chan error, 2)
	for _, eventID := range []string{"evt-first-a", "evt-first-b"} {
		event := routeFitIntegrationEvent(eventID, 1)
		event.Aggregate.ID = "first-concurrent"
		go func() { errs <- recorder.Record(e.ctx, event, []byte(`{"concurrent":true}`)) }()
	}
	select {
	case <-started:
	case err := <-errs:
		t.Fatalf("first Record() error = %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("no concurrent handler started")
	}
	close(release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}
	if got := handled.Load(); got != 1 {
		t.Fatalf("handled = %d, want 1", got)
	}
	assertRouteFitCount(t, e.ctx, e.pool, `SELECT count(*) FROM logistics.shipment_event WHERE aggregate_id='first-concurrent'`, 2)
	assertRouteFitCount(t, e.ctx, e.pool, `SELECT count(*) FROM logistics.shipment_event WHERE aggregate_id='first-concurrent' AND processing_state='accepted'`, 1)
}

type routeFitIntegrationEnvironment struct {
	ctx  context.Context
	pool *pgxpool.Pool
}

func newRouteFitIntegrationEnvironment(t *testing.T) routeFitIntegrationEnvironment {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
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
	return routeFitIntegrationEnvironment{ctx: ctx, pool: pool}
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
