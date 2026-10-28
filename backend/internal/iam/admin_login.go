package iam

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

const adminLoginFailureLimit = 5

var adminLoginFailureWindow = 15 * time.Minute

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

type adminFailure struct {
	attempts int
	first    time.Time
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

	mu       sync.Mutex
	failures map[string]adminFailure
}

func (s *AdminLoginService) Login(ctx context.Context, input AdminLoginInput) (AdminLoginResult, error) {
	if s == nil || s.DB == nil || s.Cipher == nil || strings.TrimSpace(input.Email) == "" || input.Password == "" || input.TOTPCode == "" || (input.DeviceID != "" && !devicePattern.MatchString(input.DeviceID)) || net.ParseIP(input.IP) == nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	now := s.now()
	key := email + "\x00" + input.IP
	if s.locked(key, now) {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}

	var id int64
	var credential AdminCredentials
	err := s.DB.QueryRow(ctx, `SELECT id,public_id,password_hash,totp_secret_enc,status FROM iam.admin_user WHERE email=$1`, email).
		Scan(&id, &credential.PublicID, &credential.PasswordHash, &credential.TOTPSecretEnc, &credential.Status)
	if err != nil || AuthenticateAdmin(ctx, s.Cipher, credential, input.Password, input.TOTPCode, now) != nil {
		if err == nil {
			s.appendAudit(ctx, id, credential.PublicID, "failed", input, now)
		}
		s.recordFailure(key, now)
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
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
	ttl := s.RefreshTTL
	if ttl <= 0 {
		ttl = 8 * time.Hour
	}
	if _, err = tx.Exec(ctx, `INSERT INTO iam.session(public_id,admin_user_id,family_id,refresh_token_hash,refresh_kid,device_id,ip,expires_at,last_used_at) VALUES($1,$2,$1,$3,$4,$5,$6,$7,$8)`, sessionID, id, refreshHash, refreshKID, input.DeviceID, input.IP, now.Add(ttl), now); err != nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	if _, err = tx.Exec(ctx, `UPDATE iam.admin_user SET last_login_at=$1,updated_at=$1 WHERE id=$2`, now, id); err != nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	if err = tx.Commit(ctx); err != nil {
		return AdminLoginResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	access, err := s.Tokens.IssueAccess(ctx, credential.PublicID, "admin", sessionID)
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
	s.clearFailure(key)
	s.appendAudit(ctx, id, credential.PublicID, "ok", input, now)
	return AdminLoginResult{AccessToken: access, AccessExpiresIn: int64(s.Tokens.AccessTTL / time.Second), RefreshToken: refresh}, nil
}

func (s *AdminLoginService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func (s *AdminLoginService) locked(key string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.failures[key]
	if !ok || now.Sub(f.first) >= adminLoginFailureWindow {
		delete(s.failures, key)
		return false
	}
	return f.attempts >= adminLoginFailureLimit
}
func (s *AdminLoginService) recordFailure(key string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failures == nil {
		s.failures = make(map[string]adminFailure)
	}
	f := s.failures[key]
	if f.first.IsZero() || now.Sub(f.first) >= adminLoginFailureWindow {
		f = adminFailure{first: now}
	}
	f.attempts++
	s.failures[key] = f
}
func (s *AdminLoginService) clearFailure(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failures, key)
}

func (s *AdminLoginService) appendAudit(ctx context.Context, id int64, target, result string, input AdminLoginInput, at time.Time) {
	if s.Audit == nil || id <= 0 || target == "" || input.RequestID == "" {
		return
	}
	_, _ = s.Audit.Append(ctx, platformaudit.Entry{Time: at, ActorID: id, ActorRole: "admin", Action: platformaudit.ActionLogin, Target: "admin:" + target, Detail: []byte(`{"result":"` + result + `"}`), IP: input.IP, RequestID: input.RequestID})
}
