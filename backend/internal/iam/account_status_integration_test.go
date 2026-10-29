package iam

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/iam/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

func (f *loginFixture) testAccountStatus(t *testing.T) {
	ctx := context.Background()
	login, err := f.service.Login(ctx, LoginInput{End: telegram.Buyer, InitData: signInit(f.now, 9600, "buyer-standby-token", ""), DeviceID: "status-device"})
	if err != nil {
		t.Fatal(err)
	}
	service := &AccountStatusService{DB: f.pool, Cache: f.cache, Now: func() time.Time { return f.now }}
	actor := StatusActor{Type: "admin", ID: "admin-status-test"}
	if _, err := service.SetAccountStatus(ctx, login.Account.PublicID, "frozen", "  ", actor); err == nil {
		t.Fatal("blank freeze reason accepted")
	}
	var status string
	var logs int
	if err := f.pool.QueryRow(ctx, `SELECT status FROM iam.account WHERE public_id=$1`, login.Account.PublicID).Scan(&status); err != nil || status != "active" {
		t.Fatalf("blank reason changed status=%q err=%v", status, err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM iam.account_status_log WHERE account_id=(SELECT id FROM iam.account WHERE public_id=$1)`, login.Account.PublicID).Scan(&logs); err != nil || logs != 0 {
		t.Fatalf("blank reason logs=%d err=%v", logs, err)
	}

	change, err := service.SetAccountStatus(ctx, login.Account.PublicID, "frozen", "review required", actor)
	if err != nil || change.ToStatus != "frozen" {
		t.Fatalf("freeze=%+v err=%v", change, err)
	}
	var epoch int64
	var revoked int
	var reason *string
	if err := f.pool.QueryRow(ctx, `SELECT status,status_reason,token_epoch FROM iam.account WHERE public_id=$1`, login.Account.PublicID).Scan(&status, &reason, &epoch); err != nil || status != "frozen" || reason == nil || *reason != "review required" || epoch != 1 {
		t.Fatalf("frozen account status=%q reason=%v epoch=%d err=%v", status, reason, epoch, err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM iam.session s JOIN iam.account a ON a.id=s.account_id WHERE a.public_id=$1 AND s.revoked_reason='account_frozen'`, login.Account.PublicID).Scan(&revoked); err != nil || revoked != 1 {
		t.Fatalf("revoked sessions=%d err=%v", revoked, err)
	}
	checker := DBSessionChecker{DB: f.pool, Cache: f.cache, IdleTTL: 24 * time.Hour, Now: func() time.Time { return f.now }}
	manager := f.service.Tokens
	manager.Checker = checker
	request := httptest.NewRequest(http.MethodGet, "/v1/buyer/orders", nil)
	request.Header.Set("Authorization", "Bearer "+login.AccessToken)
	if _, ok, _ := (Authenticator{Tokens: manager}).Authenticate(request); ok {
		t.Fatal("frozen account access token remained active")
	}
	if _, err := (&SessionService{DB: f.pool, Tokens: f.service.Tokens, Cache: f.cache, Now: func() time.Time { return f.now }}).Refresh(ctx, login.RefreshToken, ""); err == nil {
		t.Fatal("frozen account refresh token remained valid")
	}
	if _, err := service.SetAccountStatus(ctx, login.Account.PublicID, "active", "restored", actor); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT status_reason FROM iam.account WHERE public_id=$1`, login.Account.PublicID).Scan(&reason); err != nil || reason != nil {
		t.Fatalf("restored reason=%v err=%v", reason, err)
	}
	if _, err := service.SetAccountStatus(ctx, login.Account.PublicID, "active", "again", actor); !errors.Is(err, ErrNoStatusChange) {
		t.Fatalf("same status error=%v", err)
	}

	concurrent, err := f.service.Login(ctx, LoginInput{End: telegram.Buyer, InitData: signInit(f.now, 9601, "buyer-standby-token", "")})
	if err != nil {
		t.Fatal(err)
	}
	var successes int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.SetAccountStatus(ctx, concurrent.Account.PublicID, "disabled", "concurrent test", actor)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successes++
			} else if !errors.Is(err, ErrNoStatusChange) {
				t.Errorf("concurrent status change: %v", err)
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("successes=%d, want 1", successes)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM iam.account_status_log WHERE account_id=(SELECT id FROM iam.account WHERE public_id=$1)`, concurrent.Account.PublicID).Scan(&logs); err != nil || logs != 1 {
		t.Fatalf("concurrent logs=%d err=%v", logs, err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM iam.session s JOIN iam.account a ON a.id=s.account_id WHERE a.public_id=$1 AND s.revoked_reason='account_disabled'`, concurrent.Account.PublicID).Scan(&revoked); err != nil || revoked != 1 {
		t.Fatalf("disabled revoked sessions=%d err=%v", revoked, err)
	}
	if _, err := service.StatusHistory(ctx, concurrent.Account.PublicID, 101); err == nil {
		t.Fatal("history accepted a limit above 100")
	}
	var appErr *httpx.AppError
	if _, err := service.StatusHistory(ctx, "missing", 1); !errors.As(err, &appErr) || appErr.Code != httpx.CodeResourceNotFound {
		t.Fatalf("missing history error=%v", err)
	}
}
