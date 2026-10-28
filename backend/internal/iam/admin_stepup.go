package iam

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

var adminStepUpScopes = map[string]Module{
	"withdrawal_review":      ModuleWithdrawals,
	"withdrawal_paid":        ModuleWithdrawals,
	"account_freeze":         ModuleUsers,
	"merchant_status_change": ModuleMerchants,
	"permission_change":      ModuleRBAC,
}

// AdminStepUpService verifies a fresh administrator TOTP before minting a
// scope-bound five-minute token. A TOTP may only be used once per time step.
type AdminStepUpService struct {
	DB          *pgxpool.Pool
	Tokens      TokenManager
	Cipher      TOTPSecretCipher
	Permissions AdminPermissionStore
	Audit       platformaudit.Writer
	Failures    *AdminFailureGuard
	Now         func() time.Time

	mu   sync.Mutex
	used map[string]time.Time
}

func (s *AdminStepUpService) Issue(ctx context.Context, adminPublicID, code, scope, ip, requestID string) (AdminStepUpResult, error) {
	module, allowed := adminStepUpScopes[scope]
	if s == nil || s.DB == nil || s.Cipher == nil || !allowed {
		var id int64
		if s != nil && s.DB != nil && adminPublicID != "" {
			_ = s.DB.QueryRow(ctx, `SELECT id FROM iam.admin_user WHERE public_id=$1`, adminPublicID).Scan(&id)
			s.appendAudit(ctx, id, adminPublicID, scope, "failed", ip, requestID)
		}
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeInvalidArgument)
	}
	if adminPublicID == "" || net.ParseIP(ip) == nil {
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	can, err := s.Permissions.Can(ctx, adminPublicID, string(module), string(PermissionEdit))
	if err != nil {
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	if !can {
		s.appendAudit(ctx, 0, adminPublicID, scope, "denied", ip, requestID)
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodePermissionDenied)
	}
	var id int64
	var secretEnc []byte
	var status AdminStatus
	var email string
	err = s.DB.QueryRow(ctx, `SELECT id,email,totp_secret_enc,status FROM iam.admin_user WHERE public_id=$1`, adminPublicID).Scan(&id, &email, &secretEnc, &status)
	if err != nil || status != AdminStatusActive {
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	secret, err := s.Cipher.Decrypt(ctx, secretEnc, adminTOTPAAD(adminPublicID))
	if err != nil {
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	defer zero(secret)
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	key := email + "\x00" + ip
	if s.Failures != nil && s.Failures.locked(key, now) {
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	if !ValidateTOTP(string(secret), code, now) || !s.consume(adminPublicID, code, now) {
		if s.Failures != nil {
			s.Failures.recordFailure(key, now)
		}
		s.appendAudit(ctx, id, adminPublicID, scope, "failed", ip, requestID)
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	if s.Failures != nil {
		s.Failures.clear(key)
	}
	token, expiresAt, err := s.Tokens.IssueStepUp(ctx, adminPublicID, scope)
	if err != nil {
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	s.appendAudit(ctx, id, adminPublicID, scope, "ok", ip, requestID)
	return AdminStepUpResult{Token: token, Scope: scope, ExpiresAt: expiresAt}, nil
}

func (s *AdminStepUpService) appendAudit(ctx context.Context, id int64, publicID, scope, result, ip, requestID string) {
	if s == nil || s.Audit == nil || id <= 0 || publicID == "" || requestID == "" {
		return
	}
	_, _ = s.Audit.Append(ctx, platformaudit.Entry{Time: s.now(), ActorID: id, ActorRole: "admin", Action: platformaudit.ActionStepUp, Target: "admin:" + publicID, Detail: []byte(`{"result":"` + result + `","scope":"` + scope + `"}`), IP: ip, RequestID: requestID})
}

func (s *AdminStepUpService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *AdminStepUpService) consume(adminPublicID, code string, now time.Time) bool {
	key := adminPublicID + "\x00" + code
	window := now.Truncate(30 * time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used == nil {
		s.used = make(map[string]time.Time)
	}
	for k, until := range s.used {
		if !until.After(now) {
			delete(s.used, k)
		}
	}
	if until, exists := s.used[key]; exists && until.After(now) {
		return false
	}
	s.used[key] = window.Add(30 * time.Second)
	return true
}
