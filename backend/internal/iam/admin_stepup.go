package iam

import (
	"context"
	"encoding/json"
	"net"
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
	Security    AdminSecurityStore
	Now         func() time.Time
}

func (s *AdminStepUpService) Issue(ctx context.Context, adminPublicID, sessionPublicID, code, scope, ip, requestID string) (AdminStepUpResult, error) {
	module, allowed := adminStepUpScopes[scope]
	if s == nil || s.DB == nil || s.Cipher == nil || s.Security == nil || !allowed {
		var id int64
		if s != nil && s.DB != nil && adminPublicID != "" {
			_ = s.DB.QueryRow(ctx, `SELECT id FROM iam.admin_user WHERE public_id=$1`, adminPublicID).Scan(&id)
			if err := s.queueAudit(ctx, id, adminPublicID, "unknown", "failed", ip, requestID); err != nil {
				return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
			}
		}
		if s != nil && s.Security == nil {
			return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
		}
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeInvalidArgument)
	}
	if adminPublicID == "" || sessionPublicID == "" || net.ParseIP(ip) == nil {
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	var id int64
	var secretEnc []byte
	var status AdminStatus
	var email string
	err := s.DB.QueryRow(ctx, `SELECT id,email,totp_secret_enc,status FROM iam.admin_user WHERE public_id=$1`, adminPublicID).Scan(&id, &email, &secretEnc, &status)
	if err != nil || status != AdminStatusActive {
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	can, err := s.Permissions.Can(ctx, adminPublicID, string(module), string(PermissionEdit))
	if err != nil {
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	if !can {
		if err := s.queueAudit(ctx, id, adminPublicID, scope, "denied", ip, requestID); err != nil {
			return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
		}
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodePermissionDenied)
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
	allowedAttempt, err := s.Security.ReserveAttempt(ctx, email, ip)
	if err != nil {
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	if !allowedAttempt {
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	fresh := false
	if ValidateTOTP(string(secret), code, now) {
		fresh, err = s.Security.ConsumeTOTP(ctx, adminPublicID, now.Unix()/30, 90*time.Second)
		if err != nil {
			return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
		}
	}
	if !fresh {
		if err := s.queueAudit(ctx, id, adminPublicID, scope, "failed", ip, requestID); err != nil {
			return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
		}
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	if err := s.Security.ClearAccountFailures(ctx, email, ip); err != nil {
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	token, expiresAt, err := s.Tokens.IssueStepUp(ctx, adminPublicID, sessionPublicID, scope)
	if err != nil {
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	if err := s.queueAudit(ctx, id, adminPublicID, scope, "ok", ip, requestID); err != nil {
		return AdminStepUpResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	return AdminStepUpResult{Token: token, Scope: scope, ExpiresAt: expiresAt}, nil
}

func (s *AdminStepUpService) queueAudit(ctx context.Context, id int64, publicID, scope, result, ip, requestID string) error {
	if s == nil || s.DB == nil || id <= 0 || publicID == "" || requestID == "" {
		return nil
	}
	if _, allowed := adminStepUpScopes[scope]; !allowed {
		scope = "unknown"
	}
	detail, _ := json.Marshal(map[string]string{"result": result, "scope": scope})
	outboxID, err := enqueueAdminAudit(ctx, s.DB, platformaudit.Entry{Time: s.now(), ActorID: id, ActorRole: "admin", Action: platformaudit.ActionStepUp, Target: "admin:" + publicID, Detail: detail, IP: ip, RequestID: requestID})
	if err == nil {
		_ = deliverAdminAudit(ctx, s.DB, s.Audit, outboxID)
	}
	return err
}

func (s *AdminStepUpService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
