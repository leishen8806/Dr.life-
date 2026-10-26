package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestAuditIntegration(t *testing.T) {
	ctx := context.Background()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("audit_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Skipf("PostgreSQL container unavailable: %v", err)
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
	if err := goose.Up(database, migrationDirectory(t)); err != nil {
		t.Fatal(err)
	}
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	writerDSN := createLogin(t, ctx, admin, dsn, "test_audit", "audit_writer")
	pool, err := pgxpool.New(ctx, writerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	reportPool, err := pgxpool.New(ctx, createLogin(t, ctx, admin, dsn, "test_audit_report", "report_ro"))
	if err != nil {
		t.Fatal(err)
	}
	defer reportPool.Close()
	metricState := &fakeAuditMetrics{}
	store := &Store{DB: pool, Metrics: metricState, Now: func() time.Time { return time.Now().UTC() }}

	t.Run("migration round trip", func(t *testing.T) {
		if err := goose.DownTo(database, migrationDirectory(t), 12); err != nil {
			t.Fatal(err)
		}
		if err := goose.Up(database, migrationDirectory(t)); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("direct mutation is rejected", func(t *testing.T) {
		record, err := store.Append(ctx, fixtureEntry(1))
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			fmt.Sprintf(`UPDATE audit.log SET target='changed' WHERE id=%d`, record.ID),
			fmt.Sprintf(`DELETE FROM audit.log WHERE id=%d`, record.ID),
			`TRUNCATE audit.log`,
		} {
			assertSQLState(t, admin, statement, "55000")
			assertSQLState(t, pool, statement, "42501")
		}
	})

	t.Run("redaction and event idempotency", func(t *testing.T) {
		record, err := store.Append(ctx, Entry{ActorID: 7, ActorRole: "admin", Action: ActionLogin, Target: "account:7", RequestID: "redact_1", Detail: json.RawMessage(`{"name":"visible","password":"p","nested":{"phone":"0123456789","api_key":"k"}}`)})
		if err != nil {
			t.Fatal(err)
		}
		var stored map[string]any
		if err := json.Unmarshal(record.Detail, &stored); err != nil {
			t.Fatal(err)
		}
		nested := stored["nested"].(map[string]any)
		if stored["name"] != "visible" || stored["password"] != "[REDACTED]" || nested["phone"] != "[REDACTED]" || nested["api_key"] != "[REDACTED]" {
			t.Fatalf("stored detail = %s", record.Detail)
		}
		consumer := Consumer{Store: store}
		event := Event{EventID: "event-1", ActorID: 8, ActorRole: "admin", Action: ActionRuleChanged, Target: "rule:1", RequestID: "event_req", Before: json.RawMessage(`{"value":"old"}`), After: json.RawMessage(`{"value":"new"}`)}
		first, err := consumer.Consume(ctx, event)
		if err != nil || first.Replayed {
			t.Fatalf("first consume replayed=%v err=%v", first.Replayed, err)
		}
		second, err := consumer.Consume(ctx, event)
		if err != nil || !second.Replayed || second.Record.ID != first.Record.ID {
			t.Fatalf("second consume=%+v err=%v", second, err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit.event_seen WHERE event_id=$1`, event.EventID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("event count=%d err=%v", count, err)
		}
	})

	t.Run("report role can read but not mutate", func(t *testing.T) {
		var count int
		if err := reportPool.QueryRow(ctx, `SELECT count(*) FROM audit.log`).Scan(&count); err != nil || count == 0 {
			t.Fatalf("report count=%d err=%v", count, err)
		}
		assertSQLState(t, reportPool, `INSERT INTO audit.check_result(day,check_id,severity,detail) VALUES (current_date,'x','normal','{}')`, "42501")
	})

	t.Run("one thousand concurrent appends form one chain", func(t *testing.T) {
		const count = 1000
		errorsCh := make(chan error, count)
		var wg sync.WaitGroup
		for i := 0; i < count; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := store.Append(ctx, fixtureEntry(int64(1000+i)))
				errorsCh <- err
			}(i)
		}
		wg.Wait()
		close(errorsCh)
		for err := range errorsCh {
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Verify(ctx, 0, 0); err != nil {
			t.Fatal(err)
		}
		var logs, distinctPrevious int
		if err := pool.QueryRow(ctx, `SELECT count(*),count(DISTINCT prev_hash) FROM audit.log`).Scan(&logs, &distinctPrevious); err != nil {
			t.Fatal(err)
		}
		if logs != distinctPrevious {
			t.Fatalf("logs=%d distinct previous hashes=%d", logs, distinctPrevious)
		}
	})

	t.Run("query uses filters and keyset pagination", func(t *testing.T) {
		target := "account:1000"
		page, err := store.List(ctx, Query{Target: target, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0].Target != target {
			t.Fatalf("page=%+v", page)
		}
		all, err := store.List(ctx, Query{Limit: 2})
		if err != nil || len(all.Items) != 2 || all.NextBefore == nil {
			t.Fatalf("page=%+v err=%v", all, err)
		}
		next, err := store.List(ctx, Query{BeforeID: *all.NextBefore, Limit: 2})
		if err != nil || len(next.Items) == 0 || next.Items[0].ID >= *all.NextBefore {
			t.Fatalf("next=%+v err=%v", next, err)
		}
	})

	t.Run("anchor and missing signal", func(t *testing.T) {
		missing, err := store.CheckAnchorFreshness(ctx, time.Now().UTC())
		if err != nil || !missing || !metricState.anchorMissing.Load() {
			t.Fatalf("initial missing=%v metric=%v err=%v", missing, metricState.anchorMissing.Load(), err)
		}
		sink := &fakeAnchorSink{}
		record, err := store.AnchorDay(ctx, time.Now().UTC(), sink)
		if err != nil || sink.record.LastID != record.LastID {
			t.Fatalf("anchor=%+v err=%v", record, err)
		}
		missing, err = store.CheckAnchorFreshness(ctx, time.Now().UTC())
		if err != nil || missing || metricState.anchorMissing.Load() {
			t.Fatalf("fresh anchor missing=%v metric=%v err=%v", missing, metricState.anchorMissing.Load(), err)
		}
	})

	t.Run("tamper reports first changed row", func(t *testing.T) {
		var id int64
		if err := admin.QueryRow(ctx, `SELECT id FROM audit.log ORDER BY id OFFSET 10 LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `ALTER TABLE audit.log DISABLE TRIGGER audit_log_no_update`); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `UPDATE audit.log SET detail='{"tampered":true}' WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `ALTER TABLE audit.log ENABLE TRIGGER audit_log_no_update`); err != nil {
			t.Fatal(err)
		}
		err := store.Verify(ctx, 0, 0)
		var chainErr *ChainError
		if !errors.As(err, &chainErr) || chainErr.ID != id {
			t.Fatalf("verify error=%v, want id %d", err, id)
		}
		if metricState.verifyFailures.Load() == 0 {
			t.Fatal("verification failure metric was not recorded")
		}
	})
}

type fakeAnchorSink struct{ record AnchorRecord }

func (f *fakeAnchorSink) Put(_ context.Context, record AnchorRecord) error {
	f.record = record
	return nil
}

type fakeAuditMetrics struct {
	anchorMissing  atomic.Bool
	verifyFailures atomic.Int64
}

func (*fakeAuditMetrics) AuditAppend(bool)                  {}
func (m *fakeAuditMetrics) AuditVerifyFailure()             { m.verifyFailures.Add(1) }
func (m *fakeAuditMetrics) AuditAnchorMissing(missing bool) { m.anchorMissing.Store(missing) }

func fixtureEntry(id int64) Entry {
	return Entry{ActorID: id + 1, ActorRole: "admin", Action: ActionAccountChanged, Target: fmt.Sprintf("account:%d", id), RequestID: fmt.Sprintf("req_%d", id), Detail: json.RawMessage(`{"state":"changed"}`)}
}

func migrationDirectory(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate migration directory")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations")
}

func createLogin(t *testing.T, ctx context.Context, admin *pgx.Conn, dsn, login, role string) string {
	t.Helper()
	statement := fmt.Sprintf(`CREATE ROLE %s LOGIN PASSWORD 'test-only'; GRANT %s TO %s`, pgx.Identifier{login}.Sanitize(), pgx.Identifier{role}.Sanitize(), pgx.Identifier{login}.Sanitize())
	if _, err := admin.Exec(ctx, statement); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword(login, "test-only")
	return parsed.String()
}

type execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func assertSQLState(t *testing.T, conn execer, statement, code string) {
	t.Helper()
	_, err := conn.Exec(context.Background(), statement)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("statement %q error=%v, want SQLSTATE %s", statement, err, code)
	}
}
