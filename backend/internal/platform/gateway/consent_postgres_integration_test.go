package gateway_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	api "github.com/leishen8806/Dr.life-/backend/internal/api"
	"github.com/leishen8806/Dr.life-/backend/internal/iam"
	"github.com/leishen8806/Dr.life-/backend/internal/iam/legalpublish"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

type consentIntegrationAuthenticator struct{}

func (consentIntegrationAuthenticator) Authenticate(*http.Request) (gateway.Identity, bool, error) {
	return gateway.Identity{AccountID: "01ARZ3NDEKTSV4RRFFQ69G5FA1", Role: gateway.RoleBuyer}, true, nil
}

type integrationPoolExecutor struct{ pool *pgxpool.Pool }

func (e integrationPoolExecutor) Exec(ctx context.Context, statement string, args ...any) (int64, error) {
	tag, err := e.pool.Exec(ctx, statement, args...)
	return tag.RowsAffected(), err
}

func TestConsentGateAllowsDraftOnlyDocumentsFromPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("consent_gateway_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test_only"), postgres.BasicWaitStrategies())
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
	if err := goose.Up(database, consentMigrationDirectory(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var publisherAdminID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO iam.admin_user (public_id, name, email, password_hash, role)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA2', 'Legal Publisher', 'legal-publisher@example.test', 'hash', 'super')
		RETURNING id`).Scan(&publisherAdminID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO iam.admin_user (public_id, name, email, password_hash, role)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA5', 'Backup Super', 'backup-super@example.test', 'hash', 'super')`); err != nil {
		t.Fatal(err)
	}
	var opsAdminID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO iam.admin_user (public_id, name, email, password_hash, role)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA8', 'Operations', 'ops@example.test', 'hash', 'ops')
		RETURNING id`).Scan(&opsAdminID); err != nil {
		t.Fatal(err)
	}
	var financeAdminID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO iam.admin_user (public_id, name, email, password_hash, role)
		VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA9', 'Finance', 'finance@example.test', 'hash', 'finance')
		RETURNING id`).Scan(&financeAdminID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO iam.person (public_id, tg_user_id) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA3', 998876)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO iam.account (public_id, person_id, role)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FA4', id, 'buyer' FROM iam.person WHERE tg_user_id=998876`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO iam.person (public_id, tg_user_id) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA6', 998875)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO iam.account (public_id, person_id, role)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FA7', id, 'buyer' FROM iam.person WHERE tg_user_id=998875`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO iam.person (public_id, tg_user_id) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAB', 998874)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO iam.account (public_id, person_id, role)
		SELECT '01ARZ3NDEKTSV4RRFFQ69G5FAC', id, 'buyer' FROM iam.person WHERE tg_user_id=998874`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO iam.person (public_id, tg_user_id) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA0', 998877)`); err != nil {
		t.Fatal(err)
	}
	var buyerAccountID int64
	if err := pool.QueryRow(ctx, `INSERT INTO iam.account (public_id, person_id, role) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FA1', id, 'buyer' FROM iam.person WHERE tg_user_id=998877 RETURNING id`).Scan(&buyerAccountID); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	service := iam.NewConsentService(iam.PostgresConsentRepository{DB: pool}, func() time.Time {
		return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := gateway.Authenticate(consentIntegrationAuthenticator{}, "")(gateway.RequireCurrentConsent(service, logger)(next))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/buyer/products", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d want downstream %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if got := logs.String(); !strings.Contains(got, "level=WARN") || !strings.Contains(got, "event=consent_no_published_document") || !strings.Contains(got, "kind=privacy") {
		t.Fatalf("logs=%q, want observable no-published-document warning", got)
	}

	if err := legalpublish.PublishDraft(ctx, integrationPoolExecutor{pool: pool}, legalpublish.Request{Kind: "privacy", Version: "v1.0", ApprovedBy: 987654321, ApprovalRef: "invalid-publisher"}); err == nil {
		t.Fatal("publish with nonexistent publisher succeeded")
	}
	if err := legalpublish.PublishDraft(ctx, integrationPoolExecutor{pool: pool}, legalpublish.Request{Kind: "terms", Version: "v1.0", ApprovedBy: buyerAccountID, ApprovalRef: "buyer-publisher"}); err == nil {
		t.Fatal("publish with buyer account succeeded")
	}
	if err := legalpublish.PublishDraft(ctx, integrationPoolExecutor{pool: pool}, legalpublish.Request{Kind: "terms", Version: "v1.0", ApprovedBy: financeAdminID, ApprovalRef: "finance-publisher"}); err == nil {
		t.Fatal("publish with non-authorized administrator succeeded")
	}
	if _, err := pool.Exec(ctx, `UPDATE iam.admin_user SET status='disabled' WHERE id=$1`, publisherAdminID); err != nil {
		t.Fatal(err)
	}
	if err := legalpublish.PublishDraft(ctx, integrationPoolExecutor{pool: pool}, legalpublish.Request{Kind: "privacy", Version: "v1.0", ApprovedBy: publisherAdminID, ApprovalRef: "disabled-publisher"}); err == nil {
		t.Fatal("publish with disabled publisher succeeded")
	}
	if _, err := pool.Exec(ctx, `UPDATE iam.admin_user SET status='active' WHERE id=$1`, publisherAdminID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE iam.admin_user SET status='frozen' WHERE id=$1`, opsAdminID); err != nil {
		t.Fatal(err)
	}
	if err := legalpublish.PublishDraft(ctx, integrationPoolExecutor{pool: pool}, legalpublish.Request{Kind: "terms", Version: "v1.0", ApprovedBy: opsAdminID, ApprovalRef: "frozen-ops-publisher"}); err == nil {
		t.Fatal("publish with frozen ops publisher succeeded")
	}
	if _, err := pool.Exec(ctx, `UPDATE iam.admin_user SET status='active' WHERE id=$1`, opsAdminID); err != nil {
		t.Fatal(err)
	}
	if err := legalpublish.PublishDraft(ctx, integrationPoolExecutor{pool: pool}, legalpublish.Request{Kind: "privacy", Version: "v1.0", ApprovedBy: publisherAdminID, ApprovalRef: "test-approval"}); err != nil {
		t.Fatalf("publish privacy v1: %v", err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/buyer/products", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("privacy published but unaccepted status=%d want=%d body=%s", response.Code, http.StatusForbidden, response.Body.String())
	}
	var body struct {
		Error struct {
			Code httpx.ErrorCode `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != httpx.CodeConsentRequired {
		t.Fatalf("privacy published but unaccepted code=%d want=%d", body.Error.Code, httpx.CodeConsentRequired)
	}
	var approvalRef string
	var publishedBy int64
	if err := pool.QueryRow(ctx, `SELECT approval_ref, published_by FROM ops.legal_doc WHERE kind='privacy' AND version='v1.0'`).Scan(&approvalRef, &publishedBy); err != nil {
		t.Fatal(err)
	}
	if approvalRef != "test-approval" || publishedBy != publisherAdminID {
		t.Fatalf("publication audit=(%q,%d), want (test-approval,1)", approvalRef, publishedBy)
	}
	var constraintDefinition string
	if err := pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conname='fk_legal_doc_published_by_admin_user'`).Scan(&constraintDefinition); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(constraintDefinition, "iam.admin_user") {
		t.Fatalf("published_by constraint=%q, want iam.admin_user reference", constraintDefinition)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE ops.legal_doc
		SET published_by=$1, approval_ref=NULL, published_at=clock_timestamp()
		WHERE kind='terms' AND version='v1.0'`, publisherAdminID); err == nil {
		t.Fatal("publish without approval_ref succeeded")
	}
	consentStatus := httptest.NewRecorder()
	consentHandler := gateway.Authenticate(consentIntegrationAuthenticator{}, "")(gateway.RequireCurrentConsent(service, logger)(http.HandlerFunc(api.Handler{Consents: service}.GetBuyerMeConsents)))
	consentHandler.ServeHTTP(consentStatus, httptest.NewRequest(http.MethodGet, "/v1/buyer/me/consents", nil))
	if consentStatus.Code != http.StatusOK {
		t.Fatalf("partial-publish consent status=%d want=%d body=%s", consentStatus.Code, http.StatusOK, consentStatus.Body.String())
	}
	var statusBody struct {
		Data struct {
			Items []struct {
				Kind    string `json:"kind"`
				Version string `json:"version"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(consentStatus.Body.Bytes(), &statusBody); err != nil {
		t.Fatal(err)
	}
	if len(statusBody.Data.Items) != 2 || statusBody.Data.Items[0].Kind != "privacy" || statusBody.Data.Items[0].Version != "v1.0" || statusBody.Data.Items[1].Kind != "terms" || statusBody.Data.Items[1].Version != "" {
		t.Fatalf("partial-publish consent items=%+v", statusBody.Data.Items)
	}
	if err := legalpublish.PublishDraft(ctx, integrationPoolExecutor{pool: pool}, legalpublish.Request{Kind: "terms", Version: "v1.0", ApprovedBy: opsAdminID, ApprovalRef: "ops-approved"}); err != nil {
		t.Fatalf("publish terms as active ops administrator: %v", err)
	}
}

func consentMigrationDirectory(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate migration directory")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations")
}
