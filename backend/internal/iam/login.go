package iam

import (
	"context"
	"errors"
	"net"
	"regexp"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/iam/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

var devicePattern = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,128}$`)
var startPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type LoginMetrics interface{ LoginValidationFailed(endpoint, reason string) }

type LoginInput struct {
	End      telegram.End
	InitData string
	DeviceID string
	IP       string
}

type LoginAccount struct {
	PublicID string  `json:"public_id"`
	Role     string  `json:"role"`
	Lang     *string `json:"lang"`
	Status   string  `json:"status"`
}

type LoginResult struct {
	AccessToken      string       `json:"access_token"`
	AccessExpiresIn  int64        `json:"access_expires_in"`
	RefreshToken     string       `json:"refresh_token"`
	RefreshExpiresIn int64        `json:"refresh_expires_in"`
	Account          LoginAccount `json:"account"`
	NeedsLanguage    bool         `json:"needs_language"`
	IsNewAccount     bool         `json:"is_new_account"`
}

type Service struct {
	DB           *pgxpool.Pool
	Verifier     telegram.PoolVerifier
	Tokens       TokenManager
	RefreshTTL   time.Duration
	IdleTTL      time.Duration
	MaxSessions  int
	SessionCache SessionCache
	Failures     *FailureGuard
	Metrics      LoginMetrics
	Now          func() time.Time
}

func (s *Service) Login(ctx context.Context, input LoginInput) (LoginResult, error) {
	if s.DB == nil || !input.End.Valid() || len(input.InitData) == 0 || len(input.InitData) > telegram.MaxInitDataBytes || (input.DeviceID != "" && !devicePattern.MatchString(input.DeviceID)) {
		return LoginResult{}, httpx.NewAppError(httpx.CodeInvalidArgument)
	}
	blocked, blockErr := s.loginBlocked(ctx, input.IP)
	if blockErr != nil {
		return LoginResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	if blocked || (s.Failures != nil && s.Failures.Blocked(input.IP)) {
		if s.Metrics != nil {
			s.Metrics.LoginValidationFailed(string(input.End), "rate_limited")
		}
		s.recordFailure(ctx, input, telegram.PoolResult{}, "rate_limited", time.Time{})
		return LoginResult{}, httpx.NewAppError(httpx.CodeLoginTooManyAttempts)
	}
	verified, err := s.Verifier.Verify(ctx, input.End, input.InitData)
	if err != nil {
		reason := string(telegram.FailureOf(err))
		rateLimited := s.Failures != nil && !s.Failures.AllowFailure(ctx, input.IP)
		if rateLimited {
			reason = "rate_limited"
		}
		if s.Metrics != nil {
			s.Metrics.LoginValidationFailed(string(input.End), reason)
		}
		s.recordFailure(ctx, input, verified, reason, verified.Data.AuthDate)
		if rateLimited {
			return LoginResult{}, httpx.NewAppError(httpx.CodeLoginTooManyAttempts)
		}
		return LoginResult{}, httpx.NewAppError(httpx.CodeTelegramLoginFailed)
	}
	return s.loginValidated(ctx, input, verified)
}

func (s *Service) loginBlocked(ctx context.Context, ip string) (bool, error) {
	if s.Failures == nil || net.ParseIP(ip) == nil {
		return false, nil
	}
	var count int
	err := s.DB.QueryRow(ctx, `SELECT count(*) FROM iam.login_event WHERE ip=$1 AND result IN ('invalid_signature','expired','malformed','rate_limited') AND created_at >= clock_timestamp() - $2::interval`, ip, s.Failures.Rule.Window.String()).Scan(&count)
	return count > s.Failures.Rule.Limit, err
}

func (s *Service) loginValidated(ctx context.Context, input LoginInput, verified telegram.PoolResult) (LoginResult, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return LoginResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	personID, err := upsertPerson(ctx, tx, verified.Data.User)
	if err != nil {
		return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	role := roleForEnd(input.End)
	account, isNew, err := upsertAccount(ctx, tx, personID, role)
	if err != nil {
		return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	if account.Status != "active" {
		if s.Metrics != nil {
			s.Metrics.LoginValidationFailed(string(input.End), "account_blocked")
		}
		if err := insertLoginEvent(ctx, tx, personID, verified, input, "account_blocked", verified.Data.AuthDate); err != nil {
			return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
		}
		if err := tx.Commit(ctx); err != nil {
			return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
		}
		return LoginResult{}, httpx.NewAppError(httpx.CodeAccountBlocked)
	}
	if verified.ValidByBotID == nil {
		return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	if err := bindBot(ctx, tx, account.ID, *verified.ValidByBotID, verified.Data.User.AllowsWriteToPM); err != nil {
		return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	if input.DeviceID != "" {
		if err := upsertDevice(ctx, tx, account.ID, input.DeviceID); err != nil {
			return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
		}
	}
	sessionPublicID, err := ids.NewULID()
	if err != nil {
		return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	refresh, refreshHash, refreshKID, err := s.Tokens.IssueRefresh(ctx, sessionPublicID, 0)
	if err != nil {
		return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	var epoch int64
	if err := tx.QueryRow(ctx, `SELECT token_epoch FROM iam.account WHERE id=$1 FOR UPDATE`, account.ID).Scan(&epoch); err != nil {
		return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	maxSessions := s.MaxSessions
	if maxSessions <= 0 {
		maxSessions = 5
	}
	rows, err := tx.Query(ctx, `UPDATE iam.session SET revoked_at=$1,revoked_reason='session_limit',updated_at=$1 WHERE id IN (SELECT id FROM iam.session WHERE account_id=$2 AND revoked_at IS NULL ORDER BY created_at DESC,id DESC OFFSET $3) RETURNING public_id,expires_at`, now, account.ID, maxSessions-1)
	if err != nil {
		return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	type revokedSession struct {
		id      string
		expires time.Time
	}
	var revoked []revokedSession
	for rows.Next() {
		var item revokedSession
		if err := rows.Scan(&item.id, &item.expires); err != nil {
			rows.Close()
			return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
		}
		revoked = append(revoked, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	rows.Close()
	expiresAt := now.Add(s.RefreshTTL)
	if _, err := tx.Exec(ctx, `INSERT INTO iam.session(public_id,account_id,family_id,refresh_token_hash,refresh_kid,device_id,ip,expires_at,last_used_at) VALUES($1,$2,$1,$3,$4,NULLIF($5,''),$6,$7,$8)`, sessionPublicID, account.ID, refreshHash, refreshKID, input.DeviceID, nullableIP(input.IP), expiresAt, now); err != nil {
		return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	access, err := s.Tokens.IssueAccess(ctx, account.PublicID, gatewayRole(role), sessionPublicID)
	if err != nil {
		return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	if err := insertLoginEvent(ctx, tx, personID, verified, input, "ok", verified.Data.AuthDate); err != nil {
		return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	if err := tx.Commit(ctx); err != nil {
		return LoginResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	if s.SessionCache != nil {
		for _, item := range revoked {
			_ = s.SessionCache.Revoke(ctx, item.id, positiveTTL(now, item.expires))
		}
		idleTTL := s.IdleTTL
		if idleTTL <= 0 {
			idleTTL = 24 * time.Hour
		}
		_ = s.SessionCache.Put(ctx, account.PublicID, sessionPublicID, epoch, cacheTTL(now, expiresAt, now.Add(idleTTL)))
	}
	return LoginResult{AccessToken: access, AccessExpiresIn: int64(s.Tokens.AccessTTL / time.Second), RefreshToken: refresh, RefreshExpiresIn: int64(s.RefreshTTL / time.Second), Account: LoginAccount{PublicID: account.PublicID, Role: gatewayRole(role), Lang: account.Lang, Status: account.Status}, NeedsLanguage: account.Lang == nil, IsNewAccount: isNew}, nil
}

type accountRow struct {
	ID       int64
	PublicID string
	Lang     *string
	Status   string
}

func upsertPerson(ctx context.Context, tx pgx.Tx, user telegram.User) (int64, error) {
	publicID, err := ids.NewULID()
	if err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO iam.person(public_id,tg_user_id,tg_first_name,tg_username,tg_lang_code) VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,'')) ON CONFLICT(tg_user_id) DO UPDATE SET tg_first_name=COALESCE(EXCLUDED.tg_first_name,iam.person.tg_first_name),tg_username=COALESCE(EXCLUDED.tg_username,iam.person.tg_username),tg_lang_code=COALESCE(EXCLUDED.tg_lang_code,iam.person.tg_lang_code),updated_at=now() RETURNING id`, publicID, user.ID, user.FirstName, user.Username, user.LanguageCode).Scan(&id)
	return id, err
}

func upsertAccount(ctx context.Context, tx pgx.Tx, personID int64, role string) (accountRow, bool, error) {
	publicID, err := ids.NewULID()
	if err != nil {
		return accountRow{}, false, err
	}
	kyc := any(nil)
	if role == "driver" {
		kyc = "pending"
	}
	var row accountRow
	err = tx.QueryRow(ctx, `INSERT INTO iam.account(public_id,person_id,role,kyc_status) VALUES($1,$2,$3,$4) ON CONFLICT ON CONSTRAINT ux_account_person_role_merchant DO NOTHING RETURNING id,public_id,lang,status`, publicID, personID, role, kyc).Scan(&row.ID, &row.PublicID, &row.Lang, &row.Status)
	if err == nil {
		return row, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return accountRow{}, false, err
	}
	err = tx.QueryRow(ctx, `SELECT id,public_id,lang,status FROM iam.account WHERE person_id=$1 AND role=$2 AND merchant_id IS NULL`, personID, role).Scan(&row.ID, &row.PublicID, &row.Lang, &row.Status)
	return row, false, err
}

func bindBot(ctx context.Context, tx pgx.Tx, accountID, botID int64, write bool) error {
	publicID, err := ids.NewULID()
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO iam.account_bot(public_id,account_id,bot_id,write_access) VALUES($1,$2,$3,$4) ON CONFLICT(account_id,bot_id) DO UPDATE SET write_access=iam.account_bot.write_access OR EXCLUDED.write_access,updated_at=now()`, publicID, accountID, botID, write)
	return err
}

func upsertDevice(ctx context.Context, tx pgx.Tx, accountID int64, device string) error {
	publicID, err := ids.NewULID()
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO iam.account_device(public_id,account_id,device_id) VALUES($1,$2,$3) ON CONFLICT(account_id,device_id) DO UPDATE SET last_seen_at=now(),updated_at=now()`, publicID, accountID, device)
	return err
}

func insertLoginEvent(ctx context.Context, tx pgx.Tx, personID int64, verified telegram.PoolResult, input LoginInput, result string, authDate time.Time) error {
	publicID, err := ids.NewULID()
	if err != nil {
		return err
	}
	var auth any
	if !authDate.IsZero() {
		auth = authDate
	}
	start := ""
	if startPattern.MatchString(verified.Data.StartParam) {
		start = verified.Data.StartParam
	}
	_, err = tx.Exec(ctx, `INSERT INTO iam.login_event(public_id,person_id,bot_id,valid_by_bot_id,tried_count,auth_date,ip,device_id,result,start_param) VALUES($1,NULLIF($2,0),$3,$4,$5,$6,$7,NULLIF($8,''),$9,NULLIF($10,''))`, publicID, personID, verified.PrimaryBotID, verified.ValidByBotID, verified.TriedCount, auth, nullableIP(input.IP), input.DeviceID, result, start)
	return err
}

func (s *Service) recordFailure(ctx context.Context, input LoginInput, verified telegram.PoolResult, result string, authDate time.Time) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if insertLoginEvent(ctx, tx, 0, verified, input, result, authDate) == nil {
		_ = tx.Commit(ctx)
	}
}

func nullableIP(raw string) any {
	if net.ParseIP(raw) == nil {
		return nil
	}
	return raw
}
func roleForEnd(end telegram.End) string {
	if end == telegram.Merchant {
		return "merchant_owner"
	}
	return string(end)
}
func gatewayRole(role string) string {
	if role == "merchant_owner" {
		return "merchant"
	}
	return role
}

type FailureGuard struct {
	Limiter gateway.Limiter
	Rule    gateway.Rule
	Now     func() time.Time
	mu      sync.Mutex
	blocked map[string]time.Time
}

func (g *FailureGuard) Blocked(ip string) bool {
	if ip == "" {
		ip = "unknown"
	}
	now := time.Now()
	if g.Now != nil {
		now = g.Now()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	until := g.blocked[ip]
	if !now.Before(until) {
		delete(g.blocked, ip)
		return false
	}
	return true
}

func (g *FailureGuard) AllowFailure(ctx context.Context, ip string) bool {
	if g == nil || g.Limiter == nil {
		return true
	}
	if ip == "" {
		ip = "unknown"
	}
	allowed, retry, err := g.Limiter.Allow(ctx, "telegram-login-failure:"+ip, g.Rule)
	if err != nil {
		return false
	}
	if allowed {
		return true
	}
	now := time.Now()
	if g.Now != nil {
		now = g.Now()
	}
	g.mu.Lock()
	if g.blocked == nil {
		g.blocked = make(map[string]time.Time)
	}
	g.blocked[ip] = now.Add(retry)
	g.mu.Unlock()
	return false
}
