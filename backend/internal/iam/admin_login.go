package iam

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

const adminLoginFailureLimit = 5
const adminLoginIPFailureLimit = adminLoginFailureLimit * 4

var adminLoginFailureWindow = 15 * time.Minute

const dummyAdminPasswordHash = "$argon2id$v=19$m=65536,t=3,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// AdminLoginInput deliberately keeps credential values out of errors and logs.
type AdminLoginInput struct {
	Email     string
	Password  string
	TOTPCode  string
	IP        string
	DeviceID  string
	RequestID string
}

type AdminLoginResult struct {
	AccessToken     string `json:"access_token"`
	AccessExpiresIn int64  `json:"access_expires_in"`
	RefreshToken    string `json:"-"`
}

type AdminStepUpResult struct {
	Token     string    `json:"step_up_token"`
	Scope     string    `json:"scope"`
	ExpiresAt time.Time `json:"expires_at"`
}

// AdminLoginService authenticates a web administrator and uses the shared iam.session chain.
// Failed-attempt state is intentionally keyed by the normalized account identifier and source IP.
type AdminLoginService struct {
	DB           *pgxpool.Pool
	Tokens       TokenManager
	Cipher       TOTPSecretCipher
	Audit        platformaudit.Writer
	RefreshTTL   time.Duration
	IdleTTL      time.Duration
	SessionCache SessionCache
	Now          func() time.Time
	Security     AdminSecurityStore
}

func (s *AdminLoginService) Login(ctx context.Context, input AdminLoginInput) (AdminLoginResult, error) {
	if s == nil || s.DB == nil || s.Cipher == nil || strings.TrimSpace(input.Email) == "" || input.Password == "" || input.TOTPCode == "" || strings.TrimSpace(input.RequestID) == "" || (input.DeviceID != "" && !devicePattern.MatchString(input.DeviceID)) || net.ParseIP(input.IP) == nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	now := s.now()
	if s.Security == nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	allowed, err := s.Security.ReserveAttempt(ctx, email, input.IP)
	if err != nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	if !allowed {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}

	var id int64
	var passwordChangeRequired bool
	var credential AdminCredentials
	err = s.DB.QueryRow(ctx, `SELECT id,public_id,password_hash,totp_secret_enc,status,password_change_required FROM iam.admin_user WHERE email=$1`, email).
		Scan(&id, &credential.PublicID, &credential.PasswordHash, &credential.TOTPSecretEnc, &credential.Status, &passwordChangeRequired)
	passwordHash := dummyAdminPasswordHash
	if err == nil && credential.Status == AdminStatusActive {
		passwordHash = credential.PasswordHash
	}
	passwordOK, verifyErr := VerifyPasswordContext(ctx, passwordHash, input.Password)
	if verifyErr != nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	authenticated := err == nil && credential.Status == AdminStatusActive && passwordOK
	if authenticated {
		secret, decryptErr := s.Cipher.Decrypt(ctx, credential.TOTPSecretEnc, adminTOTPAAD(credential.PublicID))
		if decryptErr != nil {
			authenticated = false
		} else {
			authenticated = ValidateTOTP(string(secret), input.TOTPCode, now)
			zero(secret)
		}
	}
	if authenticated {
		fresh, consumeErr := s.Security.ConsumeTOTP(ctx, credential.PublicID, now.Unix()/30, 90*time.Second)
		if consumeErr != nil {
			return AdminLoginResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
		}
		authenticated = fresh
	}
	if !authenticated {
		if err == nil {
			if _, auditErr := enqueueAdminAudit(ctx, s.DB, adminLoginAuditEntry(id, credential.PublicID, "failed", input, now)); auditErr != nil {
				return AdminLoginResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
			}
		}
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	if err := s.Security.ClearAccountFailures(ctx, email); err != nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}

	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sessionID, err := ids.NewULID()
	if err != nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	refresh, refreshHash, refreshKID, err := s.Tokens.IssueRefresh(ctx, sessionID, 0)
	if err != nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	ttl := 8 * time.Hour
	if _, err = tx.Exec(ctx, `INSERT INTO iam.session(public_id,admin_user_id,family_id,refresh_token_hash,refresh_kid,device_id,ip,expires_at,last_used_at) VALUES($1,$2,$1,$3,$4,$5,$6,$7,$8)`, sessionID, id, refreshHash, refreshKID, input.DeviceID, input.IP, now.Add(ttl), now); err != nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	if _, err = tx.Exec(ctx, `UPDATE iam.admin_user SET last_login_at=$1,updated_at=$1 WHERE id=$2`, now, id); err != nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	auditOutboxID, err := queueAdminAudit(ctx, tx, adminLoginAuditEntry(id, credential.PublicID, "ok", input, now))
	if err != nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	if err = tx.Commit(ctx); err != nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	var access string
	if passwordChangeRequired {
		access, err = s.Tokens.IssueRestrictedAdminAccess(ctx, credential.PublicID, sessionID)
	} else {
		access, err = s.Tokens.IssueAccess(ctx, credential.PublicID, "admin", sessionID)
	}
	if err != nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	if s.SessionCache != nil {
		idle := s.IdleTTL
		if idle <= 0 {
			idle = 15 * time.Minute
		}
		_ = s.SessionCache.Put(ctx, credential.PublicID, sessionID, 0, cacheTTL(now, now.Add(ttl), now.Add(idle)))
	}
	_ = deliverAdminAudit(ctx, s.DB, s.Audit, auditOutboxID)
	return AdminLoginResult{AccessToken: access, AccessExpiresIn: int64(s.Tokens.AccessTTL / time.Second), RefreshToken: refresh}, nil
}

func (s *AdminLoginService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func adminLoginAuditEntry(id int64, target, result string, input AdminLoginInput, at time.Time) platformaudit.Entry {
	detail, _ := json.Marshal(map[string]string{"result": result})
	return platformaudit.Entry{Time: at, ActorID: id, ActorRole: "admin", Action: platformaudit.ActionLogin, Target: "admin:" + target, Detail: detail, IP: input.IP, RequestID: input.RequestID}
}
