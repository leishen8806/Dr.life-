package db_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
	platformdb "github.com/leishen8806/Dr.life-/backend/internal/platform/db"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/server"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

var schemas = []string{"iam", "catalog", "trade", "wms", "logistics", "ledger", "comms", "ai", "ops", "audit"}

func TestDatabaseFramework(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("drlife_test"),
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
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	migrationsDir := migrationDirectory(t)

	t.Run("migrate rollback and migrate again", func(t *testing.T) {
		if err := goose.Up(sqlDB, migrationsDir); err != nil {
			t.Fatal(err)
		}
		assertSchemasAndRoles(t, sqlDB)
		if err := goose.DownTo(sqlDB, migrationsDir, 0); err != nil {
			t.Fatal(err)
		}
		if err := goose.Up(sqlDB, migrationsDir); err != nil {
			t.Fatal(err)
		}
		assertSchemasAndRoles(t, sqlDB)
	})

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)

	t.Run("ops file writer cannot delete or truncate files", func(t *testing.T) {
		for _, privilege := range []string{"DELETE", "TRUNCATE"} {
			var allowed bool
			if err := admin.QueryRow(ctx, `SELECT has_table_privilege('ops_rw', 'ops.file', $1)`, privilege).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed {
				t.Fatalf("ops_rw unexpectedly has %s on ops.file", privilege)
			}
		}
	})

	createPermissionFixtures(t, ctx, admin)

	t.Run("module roles cannot cross schema boundaries", func(t *testing.T) {
		ledger := createLogin(t, ctx, admin, dsn, "test_ledger", "ledger_rw")
		defer ledger.Close(ctx)
		assertCurrentUser(t, ctx, ledger, "test_ledger")
		if _, err := ledger.Exec(ctx, `INSERT INTO ledger.permission_probe (id, note) VALUES (1, 'own')`); err != nil {
			t.Fatalf("ledger own-schema insert failed: %v", err)
		}
		for _, statement := range []string{
			`CREATE TABLE iam.blocked (id int)`,
			`SELECT * FROM iam.permission_probe`,
			`INSERT INTO iam.permission_probe (id, note) VALUES (1, 'blocked')`,
			`UPDATE iam.permission_probe SET note = 'blocked'`,
			`DELETE FROM iam.permission_probe`,
		} {
			assertPermissionDenied(t, ctx, ledger, statement)
		}

		iam := createLogin(t, ctx, admin, dsn, "test_iam", "iam_rw")
		defer iam.Close(ctx)
		assertCurrentUser(t, ctx, iam, "test_iam")
		assertPermissionDenied(t, ctx, iam, `SELECT * FROM ledger.permission_probe`)
	})

	t.Run("audit writer can only insert and select", func(t *testing.T) {
		audit := createLogin(t, ctx, admin, dsn, "test_audit", "audit_writer")
		defer audit.Close(ctx)
		assertCurrentUser(t, ctx, audit, "test_audit")
		if _, err := audit.Exec(ctx, `INSERT INTO audit.permission_probe (id, note) VALUES (1, 'allowed')`); err != nil {
			t.Fatalf("audit insert failed: %v", err)
		}
		if _, err := audit.Exec(ctx, `SELECT * FROM audit.permission_probe`); err != nil {
			t.Fatalf("audit select failed: %v", err)
		}
		assertPermissionDenied(t, ctx, audit, `UPDATE audit.permission_probe SET note = 'blocked'`)
		assertPermissionDenied(t, ctx, audit, `DELETE FROM audit.permission_probe`)
	})

	t.Run("report role is read only across every schema", func(t *testing.T) {
		report := createLogin(t, ctx, admin, dsn, "test_report", "report_ro")
		defer report.Close(ctx)
		assertCurrentUser(t, ctx, report, "test_report")
		for _, schema := range schemas {
			if _, err := report.Exec(ctx, fmt.Sprintf(`SELECT * FROM %s.report_probe`, pgx.Identifier{schema}.Sanitize())); err != nil {
				t.Errorf("report select from %s failed: %v", schema, err)
			}
		}
		for _, statement := range []string{
			`INSERT INTO ops.report_probe (id) VALUES (1)`,
			`UPDATE ops.report_probe SET id = 2`,
			`DELETE FROM ops.report_probe`,
			`CREATE TABLE ops.blocked (id int)`,
		} {
			assertPermissionDenied(t, ctx, report, statement)
		}
	})

	t.Run("ready endpoint follows database availability", func(t *testing.T) {
		cfg := config.Config{DatabaseURL: dsn, DBMaxConns: 2, DBMinConns: 1, DBConnectTimeout: time.Second}
		pool, err := platformdb.NewPool(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		handler := server.NewHandler(nil, platformdb.Ping(pool))
		assertReadyStatus(t, handler, http.StatusOK)
		if err := container.Stop(ctx, nil); err != nil {
			t.Fatal(err)
		}
		assertReadyStatus(t, handler, http.StatusServiceUnavailable)
	})
}

func migrationDirectory(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate migration directory")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations")
}

func assertSchemasAndRoles(t *testing.T, database *sql.DB) {
	t.Helper()
	var schemaCount int
	if err := database.QueryRow(`SELECT count(*) FROM information_schema.schemata WHERE schema_name = ANY($1)`, schemas).Scan(&schemaCount); err != nil {
		t.Fatal(err)
	}
	if schemaCount != len(schemas) {
		t.Fatalf("schema count = %d, want %d", schemaCount, len(schemas))
	}
	roles := []string{"iam_rw", "catalog_rw", "trade_rw", "wms_rw", "logistics_rw", "ledger_rw", "comms_rw", "ai_rw", "ops_rw", "audit_writer", "report_ro"}
	var roleCount int
	if err := database.QueryRow(`SELECT count(*) FROM pg_roles WHERE rolname = ANY($1) AND NOT rolcanlogin`, roles).Scan(&roleCount); err != nil {
		t.Fatal(err)
	}
	if roleCount != len(roles) {
		t.Fatalf("NOLOGIN role count = %d, want %d", roleCount, len(roles))
	}
}

func createPermissionFixtures(t *testing.T, ctx context.Context, admin *pgx.Conn) {
	t.Helper()
	for _, schema := range schemas {
		statement := fmt.Sprintf(`CREATE TABLE %s.report_probe (id int PRIMARY KEY)`, pgx.Identifier{schema}.Sanitize())
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, schema := range []string{"iam", "ledger", "audit"} {
		statement := fmt.Sprintf(`CREATE TABLE %s.permission_probe (id int PRIMARY KEY, note text)`, pgx.Identifier{schema}.Sanitize())
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
}

func createLogin(t *testing.T, ctx context.Context, admin *pgx.Conn, dsn, login, permission string) *pgx.Conn {
	t.Helper()
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE ROLE %s LOGIN PASSWORD 'test-only'`, pgx.Identifier{login}.Sanitize())); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf(`GRANT %s TO %s`, pgx.Identifier{permission}.Sanitize(), pgx.Identifier{login}.Sanitize())); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword(login, "test-only")
	conn, err := pgx.Connect(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func assertCurrentUser(t *testing.T, ctx context.Context, conn *pgx.Conn, want string) {
	t.Helper()
	var got string
	if err := conn.QueryRow(ctx, `SELECT current_user`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("current_user = %q, want %q", got, want)
	}
}

func assertPermissionDenied(t *testing.T, ctx context.Context, conn *pgx.Conn, statement string) {
	t.Helper()
	_, err := conn.Exec(ctx, statement)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("statement %q error = %v, want insufficient_privilege", statement, err)
	}
}

func assertReadyStatus(t *testing.T, handler http.Handler, want int) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("ready status = %d, want %d; body=%s", response.Code, want, response.Body.String())
	}
}
