package trade

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestConfirmPaymentEnqueuesFulfillmentAndHandlesMissingMapping(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("trade_fulfillment"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	if err := goose.Up(db, migrationDirectory(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	const mappedBuyer = "01ARZ3NDEKTSV4RRFFQ69G5FA1"
	const mappedMerchant = "01ARZ3NDEKTSV4RRFFQ69G5FA2"
	const mappedSKU = "01ARZ3NDEKTSV4RRFFQ69G5FA3"
	const missingBuyer = "01ARZ3NDEKTSV4RRFFQ69G5FA4"
	const missingMerchant = "01ARZ3NDEKTSV4RRFFQ69G5FA5"
	const missingSKU = "01ARZ3NDEKTSV4RRFFQ69G5FA6"
	for _, fixture := range []struct{ buyer, merchant, sku string }{
		{mappedBuyer, mappedMerchant, mappedSKU},
		{missingBuyer, missingMerchant, missingSKU},
	} {
		if _, err := db.Exec(`
			INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4,goods_amount_e4,payable_amount_e4)
			VALUES ($1,7,$2,repeat('a',64),100,100,100);
			INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,merchant_public_id,total_e4,goods_amount_e4,shipping_amount_e4,payable_amount_e4)
			VALUES ($2,(SELECT id FROM trade.buyer_order WHERE public_id=$1),7,'merchant-public',100,100,0,100);
			INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4)
			VALUES ((SELECT id FROM trade.merchant_order WHERE public_id=$2),$3,1,100,100)`, fixture.buyer, fixture.merchant, fixture.sku); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO logistics.sku_map (sku_public_id,routefit_sku_id,warehouse_id,state) VALUES ($1,'routefit-sku','warehouse-1','active')`, mappedSKU); err != nil {
		t.Fatal(err)
	}

	store := NewPostgresStore(pool)
	store.SetFulfillmentEnqueuer(func(ctx context.Context, tx pgx.Tx, order FulfillmentOrder, _ string) (bool, error) {
		var mapped bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM logistics.sku_map WHERE sku_public_id=$1 AND state='active')`, order.Lines[0].SKU).Scan(&mapped); err != nil {
			return false, err
		}
		if !mapped {
			return true, nil
		}
		_, err := tx.Exec(ctx, `INSERT INTO logistics.fulfillment_outbox (external_order_id,idempotency_key,payload) VALUES ($1,$2,'{}') ON CONFLICT (idempotency_key) DO NOTHING`, order.ExternalOrderID, order.ExternalOrderID)
		return false, err
	})

	if _, applied, err := store.ConfirmPayment(ctx, mappedBuyer, 100, EventSource{Type: SourceKESSResult, Reference: "mapped-payment"}); err != nil || !applied {
		t.Fatalf("mapped payment applied=%v err=%v", applied, err)
	}
	if _, applied, err := store.ConfirmPayment(ctx, mappedBuyer, 100, EventSource{Type: SourceKESSResult, Reference: "mapped-payment"}); err != nil || applied {
		t.Fatalf("mapped payment replay applied=%v err=%v", applied, err)
	}
	var outboxCount int
	if err := db.QueryRow(`SELECT count(*) FROM logistics.fulfillment_outbox WHERE external_order_id=$1`, "DRL-"+mappedMerchant).Scan(&outboxCount); err != nil || outboxCount != 1 {
		t.Fatalf("mapped outbox count=%d err=%v, want 1", outboxCount, err)
	}

	if _, applied, err := store.ConfirmPayment(ctx, missingBuyer, 100, EventSource{Type: SourceKESSResult, Reference: "missing-payment"}); err != nil || !applied {
		t.Fatalf("missing mapping applied=%v err=%v", applied, err)
	}
	var status, sourceRef string
	if err := db.QueryRow(`SELECT status FROM trade.merchant_order WHERE public_id=$1`, missingMerchant).Scan(&status); err != nil || status != "exception" {
		t.Fatalf("missing mapping status=%q err=%v, want exception", status, err)
	}
	if err := db.QueryRow(`SELECT source_ref FROM trade.order_event WHERE merchant_order_id=(SELECT id FROM trade.merchant_order WHERE public_id=$1) AND to_status='exception'`, missingMerchant).Scan(&sourceRef); err != nil || sourceRef != "SKU_MAPPING_MISSING:"+missingMerchant {
		t.Fatalf("missing mapping source_ref=%q err=%v", sourceRef, err)
	}
}
