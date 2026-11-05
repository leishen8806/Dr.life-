package iam_test

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/leishen8806/Dr.life-/backend/internal/iam"
	"github.com/leishen8806/Dr.life-/backend/internal/iam/legalpublish"
)

type poolExecutor struct{ pool *pgxpool.Pool }

func (e poolExecutor) Exec(ctx context.Context, statement string, args ...any) (int64, error) {
	tag, err := e.pool.Exec(ctx, statement, args...)
	return tag.RowsAffected(), err
}

func TestConsentMigrationAndCurrentVersionEnforcement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("consent_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test_only"), postgres.BasicWaitStrategies())
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
	dir := migrationDirectory(t)
	if err := goose.Up(database, dir); err != nil {
		t.Fatal(err)
	}
	var publishedSeeds int
	if err := database.QueryRow(`SELECT count(*) FROM ops.legal_doc WHERE published_at IS NOT NULL`).Scan(&publishedSeeds); err != nil {
		t.Fatal(err)
	}
	if publishedSeeds != 0 {
		t.Fatalf("published legal seeds=%d, want 0 pending board approval", publishedSeeds)
	}
	if err := goose.DownTo(database, dir, 25); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(database, dir); err != nil {
		t.Fatal(err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var publisherAdminID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO iam.admin_user (public_id, name, email, password_hash, role)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA2', 'Legal Publisher', 'legal-publisher@example.test', 'hash', 'super')
		RETURNING id`).Scan(&publisherAdminID); err != nil {
		t.Fatal(err)
	}
	var accountID string
	var accountDBID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO iam.person (public_id, tg_user_id) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA0', 998877)
		RETURNING id`).Scan(new(int64))
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO iam.account (public_id, person_id, role)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FA1', id, 'buyer' FROM iam.person WHERE tg_user_id=998877
		RETURNING id, public_id`).Scan(&accountDBID, &accountID); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	draftService := iam.NewConsentService(iam.PostgresConsentRepository{DB: pool}, func() time.Time { return now })
	if needs, err := draftService.NeedsConsent(ctx, accountID); err != nil || needs {
		t.Fatalf("draft-only legal documents needs=%v err=%v, want false nil", needs, err)
	}
	legalPublisher := poolExecutor{pool: pool}
	for _, kind := range []string{"terms", "privacy"} {
		if err := legalpublish.PublishDraft(ctx, legalPublisher, legalpublish.Request{Kind: kind, Version: "v1.0", ApprovedBy: publisherAdminID, ApprovalRef: "test-approval"}); err != nil {
			t.Fatalf("publish %s v1: %v", kind, err)
		}
	}
	service := iam.NewConsentService(iam.PostgresConsentRepository{DB: pool}, func() time.Time { return now })
	needs, err := service.NeedsConsent(ctx, accountID)
	if err != nil || !needs {
		t.Fatalf("new account needs=%v err=%v, want true nil", needs, err)
	}
	if err := service.Accept(ctx, accountID, iam.LegalDocumentPrivacy, "v1.0", "mini_app"); err != nil {
		t.Fatalf("accept privacy v1: %v", err)
	}
	if err := service.Accept(ctx, accountID, iam.LegalDocumentTerms, "v1.0", "mini_app"); err != nil {
		t.Fatalf("accept terms v1: %v", err)
	}
	if needs, err = service.NeedsConsent(ctx, accountID); err != nil || needs {
		t.Fatalf("accepted current versions needs=%v err=%v", needs, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ops.legal_doc (kind,version,effective_at,body_zh,body_en,body_km,published_by,approval_ref,published_at) VALUES ('privacy','v1.1',$1,'zh','en','km',$2,'test-approval',$1)`, now.Add(-time.Second), publisherAdminID); err != nil {
		t.Fatal(err)
	}
	if err := service.Accept(ctx, accountID, iam.LegalDocumentPrivacy, "v1.0", "mini_app"); !errors.Is(err, iam.ErrConsentVersionNotCurrent) {
		t.Fatalf("old version error=%v, want ErrConsentVersionNotCurrent", err)
	}
	if needs, err = service.NeedsConsent(ctx, accountID); err != nil || !needs {
		t.Fatalf("new version needs=%v err=%v, want true nil", needs, err)
	}
	if err := service.Accept(ctx, accountID, iam.LegalDocumentPrivacy, "v1.1", "mini_app"); err != nil {
		t.Fatal(err)
	}
	publisher, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('ops.legal_doc:privacy'))`); err != nil {
		t.Fatal(err)
	}
	acceptDone := make(chan error, 1)
	go func() { acceptDone <- service.Accept(ctx, accountID, iam.LegalDocumentPrivacy, "v1.1", "mini_app") }()
	time.Sleep(100 * time.Millisecond) // Let acceptance block behind the publication lock.
	if _, err := publisher.Exec(ctx, `INSERT INTO ops.legal_doc (kind,version,effective_at,body_zh,body_en,body_km,published_by,approval_ref,published_at) VALUES ('privacy','v1.2',$1,'zh','en','km',$2,'test-approval',$1)`, now.Add(-time.Second), publisherAdminID); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-acceptDone; !errors.Is(err, iam.ErrConsentVersionNotCurrent) {
		t.Fatalf("concurrent acceptance error=%v, want ErrConsentVersionNotCurrent", err)
	}
	if err := service.Revoke(ctx, accountID, iam.LegalDocumentTerms); err != nil {
		t.Fatal(err)
	}
	if needs, err = service.NeedsConsent(ctx, accountID); err != nil || !needs {
		t.Fatalf("revoked consent needs=%v err=%v, want true nil", needs, err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO ops.legal_doc (kind,version,effective_at,body_zh,body_en,body_km) VALUES ('terms','v1.1',$1,'draft zh','draft en','draft km')`, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	deleteTag, err := pool.Exec(ctx, `DELETE FROM ops.legal_doc WHERE kind='terms' AND version='v1.1'`)
	if err != nil {
		t.Fatal(err)
	}
	if deleteTag.RowsAffected() != 1 {
		t.Fatalf("deleted unpublished draft rows=%d, want 1", deleteTag.RowsAffected())
	}
	current, err := service.CurrentDocument(ctx, iam.LegalDocumentTerms)
	if err != nil || current.Version != "v1.0" {
		t.Fatalf("current terms = %#v, %v; want published v1.0", current, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ops.legal_doc (kind,version,effective_at,body_zh,body_en,body_km,published_by,approval_ref,published_at) VALUES ('terms','v0.9',$1,'zh','en','km',$2,'test-approval',$1)`, now, publisherAdminID); err == nil {
		t.Fatal("publishing v0.9 after v1.0 succeeded, want monotonic-version rejection")
	}
	var reportCanRead bool
	if err := pool.QueryRow(ctx, `SELECT has_table_privilege('report_ro', 'ops.consent', 'SELECT') OR has_table_privilege('report_ro', 'ops.legal_doc', 'SELECT')`).Scan(&reportCanRead); err != nil {
		t.Fatal(err)
	}
	if reportCanRead {
		t.Fatal("report_ro retained table-wide access to legal consent data")
	}

	if _, err := pool.Exec(ctx, `CREATE ROLE app_iam_consent_test LOGIN PASSWORD 'test-only'; GRANT iam_rw TO app_iam_consent_test`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`GRANT USAGE ON SCHEMA ops TO app_iam_consent_test`,
		`GRANT SELECT ON ops.legal_doc TO app_iam_consent_test`,
		`GRANT SELECT, INSERT ON ops.consent TO app_iam_consent_test`,
		`GRANT UPDATE (revoked_at) ON ops.consent TO app_iam_consent_test`,
		`GRANT USAGE, SELECT ON SEQUENCE ops.consent_id_seq TO app_iam_consent_test`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	iamURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	iamURL.User = url.UserPassword("app_iam_consent_test", "test-only")
	iamPool, err := pgxpool.New(ctx, iamURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer iamPool.Close()
	appService := iam.NewConsentService(iam.PostgresConsentRepository{DB: iamPool}, func() time.Time { return now })
	if _, err := appService.CurrentDocument(ctx, iam.LegalDocumentTerms); err != nil {
		t.Fatalf("IAM login current document: %v", err)
	}
	if err := appService.Accept(ctx, accountID, iam.LegalDocumentTerms, "v1.0", "mini_app"); err != nil {
		t.Fatalf("IAM login accept: %v", err)
	}
}
