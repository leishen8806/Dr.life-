package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestImportCatalogIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("catalogimport"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	defer database.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(database, migrationDir(t)); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	categories := filepath.Join(dir, "categories.csv")
	attributes := filepath.Join(dir, "sku_attributes.csv")
	if err := os.WriteFile(categories, []byte("id,parent_id,level,zh,en,km,sort,source\nCAT-TILE,,1,瓷砖,Tile,ក្បឿង（待审）,10,test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attributes, []byte("category_id,key,zh,en,km,type,unit,required,sku_variant,values_zh,values_en,values_km,source\nCAT-TILE,size,尺寸,Size,ទំហំ（待审）,enum,mm,true,true,300x300|600x600,300x300|600x600,300x300（待审）|600x600,test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := importFiles(ctx, dsn, categories, attributes)
	if err != nil {
		t.Fatal(err)
	}
	if first.CategoriesInserted != 1 || first.AttributesInserted != 1 || len(first.Errors) != 0 {
		t.Fatalf("first report=%+v", first)
	}
	second, err := importFiles(ctx, dsn, categories, attributes)
	if err != nil {
		t.Fatal(err)
	}
	if second.CategoriesInserted != 0 || second.AttributesInserted != 0 || second.CategoriesUpdated != 1 || second.AttributesUpdated != 1 {
		t.Fatalf("second report=%+v", second)
	}
	var categoriesCount, attributesCount int
	var categoryPending, attributePending bool
	if err := database.QueryRow(`SELECT count(*), bool_and(km_pending_review) FROM catalog.category WHERE code='cat_tile'`).Scan(&categoriesCount, &categoryPending); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT count(*), bool_and(km_pending_review) FROM catalog.category_attribute WHERE key='size'`).Scan(&attributesCount, &attributePending); err != nil {
		t.Fatal(err)
	}
	if categoriesCount != 1 || attributesCount != 1 || !categoryPending || !attributePending {
		t.Fatalf("counts/review flags=%d/%d/%v/%v", categoriesCount, attributesCount, categoryPending, attributePending)
	}
}

func TestImportRepositoryCatalogFiles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("catalogimport"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	defer database.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(database, migrationDir(t)); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(filepath.Dir(migrationDir(t)), "..")
	result, err := importFiles(ctx, dsn, filepath.Join(root, "data", "catalog", "categories.csv"), filepath.Join(root, "data", "catalog", "sku_attributes.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 || result.CategoriesInserted == 0 || result.AttributesInserted == 0 {
		t.Fatalf("repository catalog import=%+v", result)
	}
	optionalVariants := map[string]string{"cat_floor": "color", "cat_paint": "color", "cat_shower": "finish", "cat_sofa": "color", "cat_storage": "color", "cat_lock": "finish", "cat_clean": "scent"}
	for categoryCode, key := range optionalVariants {
		var attributeType string
		var skuVariant bool
		var valueCount int
		if err := database.QueryRow(`SELECT a.type, a.is_sku_dimension, jsonb_array_length(a.value_json) FROM catalog.category_attribute a JOIN catalog.category c ON c.id = a.category_id WHERE c.code = $1 AND a.key = $2`, categoryCode, key).Scan(&attributeType, &skuVariant, &valueCount); err != nil {
			t.Fatalf("optional SKU variant %s/%s: %v", categoryCode, key, err)
		}
		if attributeType != "enum" || !skuVariant || valueCount != 3 {
			t.Fatalf("optional SKU variant %s/%s = type=%s sku=%v values=%d", categoryCode, key, attributeType, skuVariant, valueCount)
		}
	}
	for _, attribute := range []struct{ categoryCode, key string }{{"cat_sofa", "seats"}, {"cat_storage", "doors"}, {"cat_switch", "gangs"}, {"cat_fastener", "diameter"}} {
		var skuVariant bool
		if err := database.QueryRow(`SELECT a.is_sku_dimension FROM catalog.category_attribute a JOIN catalog.category c ON c.id=a.category_id WHERE c.code=$1 AND a.key=$2`, attribute.categoryCode, attribute.key).Scan(&skuVariant); err != nil {
			t.Fatalf("number attribute %s/%s: %v", attribute.categoryCode, attribute.key, err)
		}
		if skuVariant {
			t.Fatalf("number attribute %s/%s was imported as a SKU variant", attribute.categoryCode, attribute.key)
		}
	}
}

func migrationDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}
