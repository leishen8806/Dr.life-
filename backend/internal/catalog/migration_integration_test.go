package catalog_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/leishen8806/Dr.life-/backend/internal/catalog"
)

const (
	firstCategoryPublicID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	firstProductPublicID  = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	secondProductPublicID = "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	scratchCategoryCode   = "test_scratch"
	productNameJSON       = `{"zh":"瓷砖","en":"Tiles","km":"ក្បឿង"}`
	seededCategoryCount   = 8
)

func TestCatalogMigration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("drlife_catalog_test"),
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
	migrations := catalogMigrationDirectory(t)
	if err := goose.Up(database, migrations); err != nil {
		t.Fatal(err)
	}

	t.Run("up down up is reversible", func(t *testing.T) {
		assertCatalogTables(t, database, true)
		if err := goose.DownTo(database, migrations, 11); err != nil {
			t.Fatal(err)
		}
		assertCatalogTables(t, database, false)
		if err := goose.Up(database, migrations); err != nil {
			t.Fatal(err)
		}
		assertCatalogTables(t, database, true)
	})

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)

	t.Run("identity and timestamp conventions", func(t *testing.T) {
		var identityCount, badTimestamp int
		if err := admin.QueryRow(ctx, `
			SELECT
				count(*) FILTER (WHERE c.is_identity = 'YES'),
				count(*) FILTER (WHERE c.column_name IN ('created_at','updated_at') AND (c.data_type <> 'timestamp with time zone' OR c.is_nullable <> 'NO' OR c.column_default IS NULL))
			FROM information_schema.columns c
			WHERE c.table_schema = 'catalog' AND c.table_name <> 'search_log_default'`).Scan(&identityCount, &badTimestamp); err != nil {
			t.Fatal(err)
		}
		if identityCount != 9 || badTimestamp != 0 {
			t.Fatalf("identity columns=%d, bad timestamps=%d", identityCount, badTimestamp)
		}
	})

	t.Run("search_log is partitioned by day", func(t *testing.T) {
		var relkind string
		if err := admin.QueryRow(ctx, `SELECT c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'catalog' AND c.relname = 'search_log'`).Scan(&relkind); err != nil {
			t.Fatal(err)
		}
		if relkind != "p" {
			t.Fatalf("search_log relkind=%q, want partitioned table", relkind)
		}
	})

	seedCatalogFixtures(t, ctx, admin)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := catalog.NewPgStore(pool)

	t.Run("publish revalidates the locked row", func(t *testing.T) {
		const publicID = "01ARZ3NDEKTSV4RRFFQ69G5FC0"
		if _, err := admin.Exec(ctx, `
			INSERT INTO catalog.product (public_id, merchant_id, category_id, name, unit, origin, status)
			SELECT $1, 42, id, '{"zh":"瓷砖","en":"Tiles","km":""}'::jsonb, 'pcs', 'cn', 'draft'
			FROM catalog.category WHERE code = $2`, publicID, scratchCategoryCode); err != nil {
			t.Fatal(err)
		}
		for _, tier := range []struct {
			minQty string
			price  int64
		}{{"10", 140000}, {"100", 120000}} {
			if _, err := admin.Exec(ctx, `
				INSERT INTO catalog.product_tier (product_id, min_qty, unit_price_e4)
				SELECT id, $2::text::numeric, $3 FROM catalog.product WHERE public_id = $1`,
				publicID, tier.minQty, tier.price); err != nil {
				t.Fatal(err)
			}
		}

		if _, err := store.PublishProduct(ctx, 42, publicID); err == nil {
			t.Fatal("PublishProduct accepted a product whose name is not trilingual")
		} else {
			var validation *catalog.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("PublishProduct error = %v, want *catalog.ValidationError", err)
			}
		}
		var status string
		if err := admin.QueryRow(ctx, `SELECT status FROM catalog.product WHERE public_id = $1`, publicID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != string(catalog.StatusDraft) {
			t.Fatalf("status = %q, want draft after a rejected publish", status)
		}
	})

	t.Run("update revalidates an on-sale product's locked row", func(t *testing.T) {
		const publicID = "01ARZ3NDEKTSV4RRFFQ69G5FC1"
		const originalName = `{"zh":"瓷砖","en":"Tiles","km":"ក្បឿង"}`
		if _, err := admin.Exec(ctx, `
			INSERT INTO catalog.product (public_id, merchant_id, category_id, name, unit, origin, status)
			SELECT $1, 42, id, $2::jsonb, 'pcs', 'cn', 'on'
			FROM catalog.category WHERE code = $3`, publicID, originalName, scratchCategoryCode); err != nil {
			t.Fatal(err)
		}
		for _, tier := range []struct {
			minQty string
			price  int64
		}{{"10", 140000}, {"100", 120000}} {
			if _, err := admin.Exec(ctx, `
				INSERT INTO catalog.product_tier (product_id, min_qty, unit_price_e4)
				SELECT id, $2::text::numeric, $3 FROM catalog.product WHERE public_id = $1`,
				publicID, tier.minQty, tier.price); err != nil {
				t.Fatal(err)
			}
		}

		blankKm := catalog.LocalizedText{
			catalog.LangZH: "瓷砖",
			catalog.LangEN: "Tiles",
			catalog.LangKM: "   ",
		}
		if _, err := store.UpdateProduct(ctx, 42, publicID, catalog.ProductPatch{Name: blankKm}); err == nil {
			t.Fatal("UpdateProduct blanked a published product's Khmer name")
		} else {
			var validation *catalog.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("UpdateProduct error = %v, want *catalog.ValidationError", err)
			}
		}
		var km string
		if err := admin.QueryRow(ctx, `SELECT name->>'km' FROM catalog.product WHERE public_id = $1`, publicID).Scan(&km); err != nil {
			t.Fatal(err)
		}
		if km == "" {
			t.Fatal("the rejected update still cleared the Khmer name")
		}
	})

	t.Run("category constraints", func(t *testing.T) {
		assertSQLState(t, ctx, admin, "23514", fmt.Sprintf(`INSERT INTO catalog.category (public_id, code, name) VALUES ('bad-ulid', 'bad_cat', '%s')`, productNameJSON))
		assertSQLState(t, ctx, admin, "23505", fmt.Sprintf(`INSERT INTO catalog.category (public_id, code, name) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAZ', '%s', '%s')`, scratchCategoryCode, productNameJSON))
		assertSQLState(t, ctx, admin, "23514", fmt.Sprintf(`INSERT INTO catalog.category (public_id, code, name) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FB0', 'bad name', '%s')`, productNameJSON))
	})

	t.Run("product constraints", func(t *testing.T) {
		assertSQLState(t, ctx, admin, "23503", fmt.Sprintf(`INSERT INTO catalog.product (public_id, merchant_id, category_id, name, unit, origin) VALUES ('%s', 42, 999999, '%s', 'u_pc', 'cn')`, secondProductPublicID, productNameJSON))
		assertSQLState(t, ctx, admin, "23514", fmt.Sprintf(`INSERT INTO catalog.product (public_id, merchant_id, category_id, name, unit, origin, status) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FB1', 42, id, '%s', 'u_pc', 'cn', 'bogus' FROM catalog.category WHERE code = '%s'`, productNameJSON, scratchCategoryCode))
		assertSQLState(t, ctx, admin, "23514", fmt.Sprintf(`INSERT INTO catalog.product (public_id, merchant_id, category_id, name, unit, origin) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FB2', 42, id, '%s', 'u_pc', 'eu' FROM catalog.category WHERE code = '%s'`, productNameJSON, scratchCategoryCode))
		assertSQLState(t, ctx, admin, "23514", fmt.Sprintf(`INSERT INTO catalog.product (public_id, merchant_id, category_id, name, unit, origin, std_score) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FB3', 42, id, '%s', 'u_pc', 'cn', 101 FROM catalog.category WHERE code = '%s'`, productNameJSON, scratchCategoryCode))
		assertSQLState(t, ctx, admin, "23505", fmt.Sprintf(`INSERT INTO catalog.product (public_id, merchant_id, category_id, name, unit, origin) SELECT '%s', 42, id, '%s', 'u_pc', 'cn' FROM catalog.category WHERE code = '%s'`, firstProductPublicID, productNameJSON, scratchCategoryCode))
	})

	t.Run("search_text is derived from the trilingual name", func(t *testing.T) {
		var matches bool
		if err := admin.QueryRow(ctx, `SELECT search_text @@ to_tsquery('simple', 'tiles') FROM catalog.product WHERE public_id = $1`, firstProductPublicID).Scan(&matches); err != nil {
			t.Fatal(err)
		}
		if !matches {
			t.Fatal("search_text did not index the english product name")
		}
	})

	t.Run("product_tier constraints", func(t *testing.T) {
		assertSQLState(t, ctx, admin, "23505", fmt.Sprintf(`INSERT INTO catalog.product_tier (product_id, min_qty, unit_price_e4) SELECT id, 10, 120000 FROM catalog.product WHERE public_id = '%s'`, firstProductPublicID))
		assertSQLState(t, ctx, admin, "23514", fmt.Sprintf(`INSERT INTO catalog.product_tier (product_id, min_qty, unit_price_e4) SELECT id, 0, 120000 FROM catalog.product WHERE public_id = '%s'`, firstProductPublicID))
		assertSQLState(t, ctx, admin, "23514", fmt.Sprintf(`INSERT INTO catalog.product_tier (product_id, min_qty, unit_price_e4) SELECT id, 50, -1 FROM catalog.product WHERE public_id = '%s'`, firstProductPublicID))
		assertSQLState(t, ctx, admin, "23503", fmt.Sprintf(`INSERT INTO catalog.product_tier (product_id, min_qty, unit_price_e4) VALUES (999999, 1, 1)`))
	})

	t.Run("sku backfill and constraints", func(t *testing.T) {
		var categoryPendingReview bool
		if err := admin.QueryRow(ctx, `SELECT km_pending_review FROM catalog.category WHERE code = $1`, scratchCategoryCode).Scan(&categoryPendingReview); err != nil {
			t.Fatalf("catalog.category must retain Khmer review state: %v", err)
		}
		if categoryPendingReview {
			t.Fatal("fixture category must not be marked pending Khmer review")
		}

		var skuID int64
		if err := admin.QueryRow(ctx, `INSERT INTO catalog.sku (public_id, product_id, attribute_values) SELECT '01H5K000000000000000000001', id, '{}'::jsonb FROM catalog.product WHERE public_id = $1 RETURNING id`, firstProductPublicID).Scan(&skuID); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO catalog.sku_tier (sku_id, min_qty, unit_price_e4) VALUES ($1, 1, 120000)`, skuID); err != nil {
			t.Fatal(err)
		}
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO catalog.category_attribute (category_id, key, name, type, is_sku_dimension) SELECT id, 'length', '{"zh":"长","en":"Length","km":"ប្រវែង"}'::jsonb, 'number', true FROM catalog.category LIMIT 1`)
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO catalog.category_attribute (category_id, key, name, type, value_json) SELECT id, 'color', '{"zh":"颜色","en":"Color","km":"ពណ៌"}'::jsonb, 'enum', '[]'::jsonb FROM catalog.category LIMIT 1`)
		var attributePendingReview bool
		if err := admin.QueryRow(ctx, `INSERT INTO catalog.category_attribute (category_id, key, name, type, value_json, km_pending_review) SELECT id, 'color_pending', '{"zh":"color","en":"color","km":"color"}'::jsonb, 'enum', '[{"code":"red","zh":"red","en":"red","km":"red"}]'::jsonb, true FROM catalog.category WHERE code = $1 RETURNING km_pending_review`, scratchCategoryCode).Scan(&attributePendingReview); err != nil {
			t.Fatalf("catalog.category_attribute must retain Khmer review state: %v", err)
		}
		if !attributePendingReview {
			t.Fatal("explicit category attribute Khmer review marker was not persisted")
		}
		assertSQLState(t, ctx, admin, "23503", `INSERT INTO catalog.sku_tier (sku_id, min_qty, unit_price_e4) VALUES (999999, 1, 1)`)
	})

	t.Run("search and import constraints", func(t *testing.T) {
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO catalog.search_synonym (terms) VALUES (ARRAY['瓷砖'])`)
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO catalog.search_log (q) VALUES ('   ')`)
		if _, err := admin.Exec(ctx, `INSERT INTO catalog.search_log (account_id, q, result_n) VALUES (7, 'tile', 3)`); err != nil {
			t.Fatalf("insert search_log: %v", err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO catalog.product_import (merchant_id, source, items) VALUES (42, 'csv', '[]'::jsonb)`); err != nil {
			t.Fatalf("insert product_import: %v", err)
		}
		var rightsAck bool
		if err := admin.QueryRow(ctx, `SELECT rights_ack FROM catalog.product_import WHERE merchant_id = 42`).Scan(&rightsAck); err != nil {
			t.Fatal(err)
		}
		if rightsAck {
			t.Fatal("rights_ack must default to false")
		}
	})

	// 默认分区会从 ALTER DEFAULT PRIVILEGES 继承整表 SELECT，从而绕过父表的列级收窄，
	// 所以 report_ro 对分区本身也必须被收窄到不含 account_id。
	t.Run("report_ro cannot read the search log account id", func(t *testing.T) {
		var parentAccount, parentQuery bool
		if err := admin.QueryRow(ctx, `
	        SELECT has_column_privilege('report_ro', 'catalog.search_log', 'account_id', 'SELECT'),
	               has_column_privilege('report_ro', 'catalog.search_log', 'q', 'SELECT')`).Scan(&parentAccount, &parentQuery); err != nil {
			t.Fatal(err)
		}
		if parentAccount {
			t.Fatal("report_ro must not read catalog.search_log.account_id")
		}
		if !parentQuery {
			t.Fatal("report_ro must still read the non-sensitive search log columns")
		}

		var partitionTable, partitionAccount, partitionQuery bool
		if err := admin.QueryRow(ctx, `
	        SELECT has_table_privilege('report_ro', 'catalog.search_log_default', 'SELECT'),
	               has_column_privilege('report_ro', 'catalog.search_log_default', 'account_id', 'SELECT'),
	               has_column_privilege('report_ro', 'catalog.search_log_default', 'q', 'SELECT')`).Scan(&partitionTable, &partitionAccount, &partitionQuery); err != nil {
			t.Fatal(err)
		}
		if partitionTable {
			t.Fatal("the default partition must not carry a table-wide SELECT for report_ro")
		}
		if partitionAccount {
			t.Fatal("report_ro must not read catalog.search_log_default.account_id")
		}
		if !partitionQuery {
			t.Fatal("report_ro must still read the non-sensitive default partition columns")
		}
	})

	t.Run("seeded categories are trilingual", func(t *testing.T) {
		var count, incomplete int
		if err := admin.QueryRow(ctx, `
			SELECT count(*), count(*) FILTER (
				WHERE btrim(coalesce(name->>'zh','')) = '' OR btrim(coalesce(name->>'en','')) = '' OR btrim(coalesce(name->>'km','')) = '')
			FROM catalog.category WHERE is_custom = false`).Scan(&count, &incomplete); err != nil {
			t.Fatal(err)
		}
		if count != seededCategoryCount || incomplete != 0 {
			t.Fatalf("seeded categories=%d incomplete=%d, want %d/0", count, incomplete, seededCategoryCount)
		}
	})
}

func seedCatalogFixtures(t *testing.T, ctx context.Context, admin *pgx.Conn) {
	t.Helper()
	if _, err := admin.Exec(ctx, `INSERT INTO catalog.category (public_id, code, name, is_custom) VALUES ($1, $2, $3::jsonb, true)`, firstCategoryPublicID, scratchCategoryCode, productNameJSON); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO catalog.product (public_id, merchant_id, category_id, name, spec, unit, origin, lead_days, vol_m3, status, source, source_ref, std_score)
		SELECT $1, 42, id, $2::jsonb, '{"zh":"600x600","en":"600x600","km":""}'::jsonb, 'pcs', 'cn', 12, 0.0125, 'draft', 'manual', 'ref-1', 88 FROM catalog.category WHERE code = $3`, firstProductPublicID, productNameJSON, scratchCategoryCode); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO catalog.product_tier (product_id, min_qty, unit_price_e4) SELECT id, 10, 140000 FROM catalog.product WHERE public_id = $1`, firstProductPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO catalog.product_tier (product_id, min_qty, unit_price_e4) SELECT id, 100, 120000 FROM catalog.product WHERE public_id = $1`, firstProductPublicID); err != nil {
		t.Fatal(err)
	}
}

func assertCatalogTables(t *testing.T, database *sql.DB, want bool) {
	t.Helper()
	for _, name := range []string{"category", "product", "product_tier", "category_attribute", "sku", "sku_tier", "product_import", "search_synonym", "search_log"} {
		var exists bool
		if err := database.QueryRow(`SELECT to_regclass('catalog.' || $1) IS NOT NULL`, name).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != want {
			t.Fatalf("catalog.%s exists=%v, want %v", name, exists, want)
		}
	}
}

func assertSQLState(t *testing.T, ctx context.Context, conn *pgx.Conn, want, statement string) {
	t.Helper()
	_, err := conn.Exec(ctx, statement)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != want {
		t.Fatalf("statement %q error=%v, want SQLSTATE %s", statement, err, want)
	}
}

func catalogMigrationDirectory(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate migration directory")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}
