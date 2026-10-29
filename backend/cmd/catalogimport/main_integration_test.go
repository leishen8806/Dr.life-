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

func migrationDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}
