package iam

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

// AdminPasswordChangeInput contains the authenticated administrator identity
// and the credentials used to replace a first-login password. It is never
// logged or included in audit details.
type AdminPasswordChangeInput struct {
	AdminPublicID   string
	SessionPublicID string
	CurrentPassword string
	NewPassword     string
	TOTPCode        string
	IP              string
	RequestID       string
}

// AdminPasswordService changes an administrator password and retains only the
// session that authenticated the request.
type AdminPasswordService struct {
	DB           *pgxpool.Pool
	Cipher       TOTPSecretCipher
	Audit        platformaudit.Writer
	SessionCache SessionCache
	Security     AdminSecurityStore
	Now          func() time.Time
}

func (s *AdminPasswordService) Change(ctx context.Context, input AdminPasswordChangeInput) error {
	if s == nil || s.DB == nil || s.Cipher == nil || s.Audit == nil || strings.TrimSpace(input.AdminPublicID) == "" || strings.TrimSpace(input.SessionPublicID) == "" || input.CurrentPassword == "" || len(input.NewPassword) < 20 || input.TOTPCode == "" || strings.TrimSpace(input.RequestID) == "" {
		return httpx.NewAppError(httpx.CodeInvalidArgument)
	}
	if s.Security == nil {
		return httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	now := s.now()
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var adminID int64
	var email string
	var credential AdminCredentials
	err = tx.QueryRow(ctx, `SELECT id,public_id,email,password_hash,totp_secret_enc,status FROM iam.admin_user WHERE public_id=$1 FOR UPDATE`, strings.TrimSpace(input.AdminPublicID)).
		Scan(&adminID, &credential.PublicID, &email, &credential.PasswordHash, &credential.TOTPSecretEnc, &credential.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NewAppError(httpx.CodeUnauthenticated)
	}
	if err != nil {
		return err
	}
	locked, err := s.Security.Locked(ctx, email, input.IP)
	if err != nil {
		return httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	if locked {
		return httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	authenticated := AuthenticateAdmin(ctx, s.Cipher, credential, input.CurrentPassword, input.TOTPCode, now) == nil
	if authenticated {
		fresh, consumeErr := s.Security.ConsumeTOTP(ctx, credential.PublicID, now.Unix()/30, 90*time.Second)
		if consumeErr != nil {
			return httpx.NewAppError(httpx.CodeServiceUnavailable)
		}
		authenticated = fresh
	}
	if !authenticated {
		if err := s.Security.RecordFailure(ctx, email, input.IP); err != nil {
			return httpx.NewAppError(httpx.CodeServiceUnavailable)
		}
		detail, _ := json.Marshal(map[string]string{"result": "failed"})
		_, _ = s.Audit.Append(ctx, platformaudit.Entry{Time: now, ActorID: adminID, ActorRole: "admin", Action: platformaudit.ActionAccountChanged, Target: "admin:" + credential.PublicID, Detail: detail, IP: input.IP, RequestID: input.RequestID})
		return httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	if err := s.Security.ClearFailures(ctx, email, input.IP); err != nil {
		return httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	hash, err := HashPassword(input.NewPassword)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE iam.admin_user SET password_hash=$1,password_change_required=false,updated_at=$2 WHERE id=$3`, hash, now, adminID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `UPDATE iam.session SET revoked_at=$1,revoked_reason='admin_password_changed',updated_at=$1 WHERE admin_user_id=$2 AND public_id<>$3 AND revoked_at IS NULL RETURNING public_id,expires_at`, now, adminID, input.SessionPublicID)
	if err != nil {
		return err
	}
	var revoked []adminSession
	for rows.Next() {
		var item adminSession
		if err := rows.Scan(&item.ID, &item.ExpiresAt); err != nil {
			rows.Close()
			return err
		}
		revoked = append(revoked, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	detail, _ := json.Marshal(map[string]string{"result": "ok"})
	outboxID, err := queueAdminAudit(ctx, tx, platformaudit.Entry{Time: now, ActorID: adminID, ActorRole: "admin", Action: platformaudit.ActionAccountChanged, Target: "admin:" + credential.PublicID, Detail: detail, IP: input.IP, RequestID: input.RequestID})
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	for _, item := range revoked {
		if s.SessionCache != nil {
			_ = s.SessionCache.Revoke(ctx, item.ID, positiveTTL(now, item.ExpiresAt))
		}
	}
	_ = deliverAdminAudit(ctx, s.DB, s.Audit, outboxID)
	return nil
}

func (s *AdminPasswordService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
