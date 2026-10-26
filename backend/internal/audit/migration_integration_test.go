package audit_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestAuditLogMigration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("drlife_audit_test"),
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
	if err := goose.Up(database, migrationDirectory(t)); err != nil {
		t.Fatal(err)
	}

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)

	t.Run("audit writer is append-only and schema-scoped", func(t *testing.T) {
		writer := createLogin(t, ctx, admin, dsn, "q01_audit_writer", "audit_writer")
		defer writer.Close(ctx)

		first := insertLog(t, ctx, writer, "2026-10-18T00:00:00Z", "withdrawal.created", "withdrawal:one")
		second := insertLog(t, ctx, writer, "2026-10-18T00:01:00Z", "withdrawal.reviewed", "withdrawal:one")
		if len(first.hash) != 32 || len(second.hash) != 32 {
			t.Fatalf("sha256 hash lengths: first=%d second=%d", len(first.hash), len(second.hash))
		}
		if first.prevHash != nil {
			t.Fatalf("first prev_hash=%x, want NULL", first.prevHash)
		}
		if string(second.prevHash) != string(first.hash) {
			t.Fatalf("second prev_hash=%x, want first hash=%x", second.prevHash, first.hash)
		}

		assertSQLState(t, ctx, writer, "42501", `UPDATE audit.log SET action='tampered' WHERE id=1`)
		assertSQLState(t, ctx, writer, "42501", `DELETE FROM audit.log WHERE id=1`)
		assertSQLState(t, ctx, writer, "42501", `SELECT * FROM iam.person`)
		assertSQLState(t, ctx, writer, "42501", `SELECT * FROM audit.chain_state`)
		assertSQLState(t, ctx, writer, "42501", `SELECT audit.log_hash_payload(NULL, now(), 1, 'x', NULL, NULL)`)
	})

	t.Run("administrator cannot bypass immutable rows", func(t *testing.T) {
		assertSQLState(t, ctx, admin, "55000", `UPDATE audit.log SET action='tampered' WHERE id=1`)
		assertSQLState(t, ctx, admin, "55000", `DELETE FROM audit.log WHERE id=1`)
		assertSQLState(t, ctx, admin, "55000", `TRUNCATE audit.log`)
	})

	t.Run("writer cannot truncate and separator character is rejected", func(t *testing.T) {
		writer := createLogin(t, ctx, admin, dsn, "q01_audit_truncate", "audit_writer")
		defer writer.Close(ctx)
		assertSQLState(t, ctx, writer, "42501", `TRUNCATE audit.log`)
		assertSQLState(t, ctx, writer, "23514", "INSERT INTO audit.log (actor_id, action, target) VALUES (1, 'a'||chr(31)||'b', 'x')")
		assertSQLState(t, ctx, writer, "23514", "INSERT INTO audit.log (actor_id, action, target) VALUES (1, 'a', 'x'||chr(31)||'y')")
	})

	t.Run("concurrent inserts form one continuous chain", func(t *testing.T) {
		writer := createLogin(t, ctx, admin, dsn, "q01_audit_concurrent", "audit_writer")
		defer writer.Close(ctx)
		writerDSN := loginDSN(t, dsn, "q01_audit_concurrent")

		const writes = 12
		errs := make(chan error, writes)
		var group sync.WaitGroup
		for i := 0; i < writes; i++ {
			group.Add(1)
			go func(i int) {
				defer group.Done()
				conn, err := pgx.Connect(ctx, writerDSN)
				if err == nil {
					defer conn.Close(ctx)
					_, err = conn.Exec(ctx, `INSERT INTO audit.log (ts, actor_id, action, target, detail) VALUES ($1, $2, $3, $4, '{}'::jsonb)`, time.Date(2026, 10, 18, 1, 0, i, 0, time.UTC), int64(i+100), "concurrent.write", fmt.Sprintf("entry:%d", i))
				}
				errs <- err
			}(i)
		}
		group.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}

		var roots, heads, danglingLinks, invalidHashes int
		if err := admin.QueryRow(ctx, `
			SELECT
				count(*) FILTER (WHERE child.prev_hash IS NULL),
				count(*) FILTER (WHERE child.hash NOT IN (SELECT prev_hash FROM audit.log WHERE prev_hash IS NOT NULL)),
				count(*) FILTER (WHERE child.prev_hash IS NOT NULL AND parent.id IS NULL),
				count(*) FILTER (WHERE child.hash <> public.digest(audit.log_hash_payload(child.prev_hash, child.ts, child.actor_id, child.action, child.target, child.detail), 'sha256'))
			FROM audit.log child
			LEFT JOIN audit.log parent ON parent.hash = child.prev_hash`).Scan(&roots, &heads, &danglingLinks, &invalidHashes); err != nil {
			t.Fatal(err)
		}
		if roots != 1 || heads != 1 || danglingLinks != 0 || invalidHashes != 0 {
			t.Fatalf("chain roots=%d heads=%d dangling_links=%d invalid_hashes=%d", roots, heads, danglingLinks, invalidHashes)
		}
	})

	t.Run("migration is reversible before audit records are retained", func(t *testing.T) {
		if err := goose.DownTo(database, migrationDirectory(t), 13); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := database.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema='audit' AND table_name='log'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("audit.log remains after down: count=%d", count)
		}
		if err := goose.Up(database, migrationDirectory(t)); err != nil {
			t.Fatal(err)
		}
	})
}

type logRow struct {
	hash     []byte
	prevHash []byte
}

func insertLog(t *testing.T, ctx context.Context, conn *pgx.Conn, ts, action, target string) logRow {
	t.Helper()
	var row logRow
	if err := conn.QueryRow(ctx, `
		INSERT INTO audit.log (ts, actor_id, actor_role, action, target, detail, request_id)
		VALUES ($1, 7, 'finance', $2, $3, '{"after":{"status":"submitted"}}'::jsonb, 'request-1')
		RETURNING hash, prev_hash`, ts, action, target).Scan(&row.hash, &row.prevHash); err != nil {
		t.Fatal(err)
	}
	return row
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

func loginDSN(t *testing.T, dsn, login string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword(login, "test-only")
	return parsed.String()
}

func assertSQLState(t *testing.T, ctx context.Context, conn *pgx.Conn, want, statement string) {
	t.Helper()
	_, err := conn.Exec(ctx, statement)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != want {
		t.Fatalf("statement %q error=%v, want SQLSTATE %s", statement, err, want)
	}
}

func migrationDirectory(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate migration directory")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}
