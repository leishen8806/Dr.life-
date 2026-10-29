package iam

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"log/slog"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

type SessionMetrics interface{ SessionRefresh(endpoint, result string) }
type SecurityAlerter interface {
	SessionReuseDetected(context.Context, string, string)
}

type LogSecurityAlerter struct{ Logger *slog.Logger }

func (a LogSecurityAlerter) SessionReuseDetected(ctx context.Context, accountID, familyID string) {
	if a.Logger != nil {
		a.Logger.WarnContext(ctx, "refresh token reuse detected", "account_id", accountID, "session_family_id", familyID)
	}
}

type RefreshResult struct {
	AccessToken      string `json:"access_token"`
	AccessExpiresIn  int64  `json:"access_expires_in"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
}

type SessionInfo struct {
	PublicID string    `json:"public_id"`
	DeviceID *string   `json:"device_id"`
	IPPrefix *string   `json:"ip_prefix"`
	LastUsed time.Time `json:"last_used_at"`
	Expires  time.Time `json:"expires_at"`
}

type SessionService struct {
	DB          *pgxpool.Pool
	Tokens      TokenManager
	Cache       SessionCache
	Grace       time.Duration
	IdleTTL     time.Duration
	AbsoluteTTL time.Duration
	Metrics     SessionMetrics
	Alerter     SecurityAlerter
	Now         func() time.Time
}

type DBSessionChecker struct {
	DB      *pgxpool.Pool
	Cache   SessionCache
	IdleTTL time.Duration
	Now     func() time.Time
}

func (c DBSessionChecker) Active(ctx context.Context, accountID, sessionID string) (bool, error) {
	if c.Cache == nil {
		return false, ErrSessionStoreUnavailable
	}
	active, found, err := c.Cache.Get(ctx, accountID, sessionID)
	if err != nil || found {
		return active, err
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	idleTTL := c.IdleTTL
	if idleTTL <= 0 {
		idleTTL = 24 * time.Hour
	}
	var epoch int64
	var expiresAt, lastUsedAt time.Time
	var status string
	err = c.DB.QueryRow(ctx, `SELECT a.token_epoch,a.status,s.expires_at,s.last_used_at FROM iam.session s JOIN iam.account a ON a.id=s.account_id WHERE s.public_id=$1 AND a.public_id=$2 AND s.revoked_at IS NULL`, sessionID, accountID).Scan(&epoch, &status, &expiresAt, &lastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (status != "active" || !now.Before(expiresAt) || !now.Before(lastUsedAt.Add(idleTTL))) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := c.Cache.Put(ctx, accountID, sessionID, epoch, cacheTTL(now, expiresAt, lastUsedAt.Add(idleTTL))); err != nil {
		return false, err
	}
	return true, nil
}

type refreshRow struct {
	ID, AccountID, Epoch, Rotation                                int64
	PublicID, FamilyID, AccountPublicID, Role, Status, RefreshKID string
	CurrentHash, PreviousHash                                     []byte
	RotatedAt                                                     *time.Time
	ExpiresAt, LastUsedAt                                         time.Time
	DeviceID                                                      *string
}

func (s *SessionService) Refresh(ctx context.Context, refreshToken, ip string) (RefreshResult, error) {
	if s.DB == nil || refreshToken == "" || len(refreshToken) > 512 {
		return RefreshResult{}, httpx.NewAppError(httpx.CodeUnauthenticated)
	}
	now := s.now()
	hash := sha256.Sum256([]byte(refreshToken))
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RefreshResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var row refreshRow
	err = tx.QueryRow(ctx, `SELECT s.id,s.public_id,s.family_id,s.refresh_token_hash,s.previous_hash,s.rotated_at,s.expires_at,s.last_used_at,s.rotation,COALESCE(s.refresh_kid,''),s.device_id,a.id,a.public_id,a.role,a.status,a.token_epoch FROM iam.session s JOIN iam.account a ON a.id=s.account_id WHERE s.revoked_at IS NULL AND (s.refresh_token_hash=$1 OR s.previous_hash=$1) ORDER BY CASE WHEN s.refresh_token_hash=$1 THEN 0 ELSE 1 END LIMIT 1 FOR UPDATE OF s,a`, hash[:]).Scan(&row.ID, &row.PublicID, &row.FamilyID, &row.CurrentHash, &row.PreviousHash, &row.RotatedAt, &row.ExpiresAt, &row.LastUsedAt, &row.Rotation, &row.RefreshKID, &row.DeviceID, &row.AccountID, &row.AccountPublicID, &row.Role, &row.Status, &row.Epoch)
	if err != nil {
		s.metric("unknown", "invalid")
		return RefreshResult{}, httpx.NewAppError(httpx.CodeUnauthenticated)
	}
	endpoint := gatewayRole(row.Role)
	if row.Status != "active" {
		s.metric(endpoint, "blocked")
		return RefreshResult{}, httpx.NewAppError(httpx.CodeAccountBlocked)
	}
	if !now.Before(row.ExpiresAt) || !now.Before(row.LastUsedAt.Add(s.idleTTL())) {
		s.metric(endpoint, "expired")
		return RefreshResult{}, httpx.NewAppError(httpx.CodeUnauthenticated)
	}
	previous := !equalHash(row.CurrentHash, hash[:])
	if previous {
		if row.RotatedAt != nil && now.Sub(*row.RotatedAt) <= s.grace() {
			result, issueErr := s.replay(ctx, row)
			if issueErr != nil {
				return RefreshResult{}, httpx.NewAppError(httpx.CodeInternalError)
			}
			if err := tx.Commit(ctx); err != nil {
				return RefreshResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
			}
			s.metric(endpoint, "replayed")
			return result, nil
		}
		if err := s.revokeFamilyForReuse(ctx, tx, row, now); err != nil {
			return RefreshResult{}, httpx.NewAppError(httpx.CodeInternalError)
		}
		if err := tx.Commit(ctx); err != nil {
			return RefreshResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
		}
		if s.Cache != nil {
			_ = s.Cache.SetAccountEpoch(ctx, row.AccountPublicID, row.Epoch+1, s.absoluteTTL())
		}
		if s.Alerter != nil {
			s.Alerter.SessionReuseDetected(ctx, row.AccountPublicID, row.FamilyID)
		}
		s.metric(endpoint, "reuse")
		return RefreshResult{}, httpx.NewAppError(httpx.CodeUnauthenticated)
	}

	newRotation := row.Rotation + 1
	refresh, refreshHash, kid, err := s.Tokens.IssueRefresh(ctx, row.PublicID, newRotation)
	if err != nil {
		return RefreshResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	access, err := s.Tokens.IssueRotatedAccess(ctx, row.AccountPublicID, endpoint, row.PublicID, kid, newRotation, now)
	if err != nil {
		return RefreshResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	if _, err := tx.Exec(ctx, `UPDATE iam.session SET previous_hash=refresh_token_hash,refresh_token_hash=$1,rotated_at=$2,last_used_at=$2,rotation=$3,refresh_kid=$4,ip=COALESCE($5,ip),updated_at=$2 WHERE id=$6`, refreshHash, now, newRotation, kid, nullableIP(ip), row.ID); err != nil {
		return RefreshResult{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	if row.DeviceID != nil {
		if _, err := tx.Exec(ctx, `UPDATE iam.account_device SET last_seen_at=$1,updated_at=$1 WHERE account_id=$2 AND device_id=$3`, now, row.AccountID, *row.DeviceID); err != nil {
			return RefreshResult{}, httpx.NewAppError(httpx.CodeInternalError)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return RefreshResult{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	if s.Cache != nil {
		_ = s.Cache.Put(ctx, row.AccountPublicID, row.PublicID, row.Epoch, cacheTTL(now, row.ExpiresAt, now.Add(s.idleTTL())))
	}
	s.metric(endpoint, "success")
	return RefreshResult{AccessToken: access, AccessExpiresIn: int64(s.Tokens.AccessTTL / time.Second), RefreshToken: refresh, RefreshExpiresIn: int64(row.ExpiresAt.Sub(now) / time.Second)}, nil
}

func (s *SessionService) replay(ctx context.Context, row refreshRow) (RefreshResult, error) {
	refresh, hash, err := s.Tokens.ReissueRefresh(ctx, row.PublicID, row.RefreshKID, row.Rotation)
	if err != nil || !equalHash(hash, row.CurrentHash) || row.RotatedAt == nil {
		return RefreshResult{}, ErrInvalidToken
	}
	access, err := s.Tokens.IssueRotatedAccess(ctx, row.AccountPublicID, gatewayRole(row.Role), row.PublicID, row.RefreshKID, row.Rotation, *row.RotatedAt)
	return RefreshResult{AccessToken: access, AccessExpiresIn: int64(s.Tokens.AccessTTL / time.Second), RefreshToken: refresh, RefreshExpiresIn: int64(row.ExpiresAt.Sub(s.now()) / time.Second)}, err
}

func (s *SessionService) revokeFamilyForReuse(ctx context.Context, tx pgx.Tx, row refreshRow, now time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE iam.session SET revoked_at=$1,revoked_reason='refresh_reuse',updated_at=$1 WHERE family_id=$2 AND revoked_at IS NULL`, now, row.FamilyID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE iam.account SET token_epoch=token_epoch+1,updated_at=$1 WHERE id=$2`, now, row.AccountID); err != nil {
		return err
	}
	publicID, err := ids.NewULID()
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO iam.security_event(public_id,account_id,session_family_id,event,reason) VALUES($1,$2,$3,'refresh_reuse','token_reused_outside_grace')`, publicID, row.AccountID, row.FamilyID)
	return err
}

func (s *SessionService) ListSessions(ctx context.Context, accountID string) ([]SessionInfo, error) {
	now := s.now()
	rows, err := s.DB.Query(ctx, `SELECT s.public_id,s.device_id,host(s.ip),s.last_used_at,s.expires_at FROM iam.session s JOIN iam.account a ON a.id=s.account_id WHERE a.public_id=$1 AND s.revoked_at IS NULL AND s.expires_at>$2 AND s.last_used_at>$2-$3::interval ORDER BY s.last_used_at DESC`, accountID, now, s.idleTTL().String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []SessionInfo
	for rows.Next() {
		var item SessionInfo
		var ip *string
		if err := rows.Scan(&item.PublicID, &item.DeviceID, &ip, &item.LastUsed, &item.Expires); err != nil {
			return nil, err
		}
		item.IPPrefix = maskedPrefix(ip)
		sessions = append(sessions, item)
	}
	return sessions, rows.Err()
}

func (s *SessionService) RevokeSession(ctx context.Context, accountID, sessionID, reason string) error {
	if reason == "" {
		reason = "user_revoked"
	}
	now := s.now()
	var expiresAt time.Time
	err := s.DB.QueryRow(ctx, `UPDATE iam.session s SET revoked_at=$1,revoked_reason=$2,updated_at=$1 FROM iam.account a WHERE s.account_id=a.id AND a.public_id=$3 AND s.public_id=$4 AND s.revoked_at IS NULL RETURNING s.expires_at`, now, reason, accountID, sessionID).Scan(&expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NewAppError(httpx.CodeResourceNotFound)
	}
	if err != nil {
		return err
	}
	if s.Cache != nil {
		_ = s.Cache.Revoke(ctx, sessionID, positiveTTL(now, expiresAt))
	}
	return nil
}

func (s *SessionService) RevokeAllSessions(ctx context.Context, accountID, reason string) error {
	if reason == "" {
		reason = "account_revoked"
	}
	now := s.now()
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var epoch int64
	if err := tx.QueryRow(ctx, `UPDATE iam.account SET token_epoch=token_epoch+1,updated_at=$1 WHERE public_id=$2 RETURNING token_epoch`, now, accountID).Scan(&epoch); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE iam.session s SET revoked_at=$1,revoked_reason=$2,updated_at=$1 FROM iam.account a WHERE s.account_id=a.id AND a.public_id=$3 AND s.revoked_at IS NULL`, now, reason, accountID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if s.Cache != nil {
		_ = s.Cache.SetAccountEpoch(ctx, accountID, epoch, s.absoluteTTL())
	}
	return nil
}

func (s *SessionService) RebuildCache(ctx context.Context) error {
	if s.Cache == nil {
		return ErrSessionStoreUnavailable
	}
	now := s.now()
	rows, err := s.DB.Query(ctx, `SELECT a.public_id,a.token_epoch,s.public_id,s.expires_at,s.last_used_at FROM iam.session s JOIN iam.account a ON a.id=s.account_id WHERE a.status='active' AND s.revoked_at IS NULL AND s.expires_at>$1 AND s.last_used_at>$1-$2::interval`, now, s.idleTTL().String())
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var accountID, sessionID string
		var epoch int64
		var expiresAt, lastUsedAt time.Time
		if err := rows.Scan(&accountID, &epoch, &sessionID, &expiresAt, &lastUsedAt); err != nil {
			return err
		}
		if err := s.Cache.Put(ctx, accountID, sessionID, epoch, cacheTTL(now, expiresAt, lastUsedAt.Add(s.idleTTL()))); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *SessionService) CleanupExpiredSessions(ctx context.Context) (int64, error) {
	result, err := s.DB.Exec(ctx, `DELETE FROM iam.session WHERE expires_at<clock_timestamp() OR last_used_at<clock_timestamp()-$1::interval`, s.idleTTL().String())
	return result.RowsAffected(), err
}

func (s *SessionService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func (s *SessionService) grace() time.Duration {
	if s.Grace > 0 {
		return s.Grace
	}
	return 10 * time.Second
}
func (s *SessionService) idleTTL() time.Duration {
	if s.IdleTTL > 0 {
		return s.IdleTTL
	}
	return 24 * time.Hour
}
func (s *SessionService) absoluteTTL() time.Duration {
	if s.AbsoluteTTL > 0 {
		return s.AbsoluteTTL
	}
	return defaultSessionAbsoluteTTL
}
func (s *SessionService) metric(endpoint, result string) {
	if s.Metrics != nil {
		s.Metrics.SessionRefresh(endpoint, result)
	}
}
func equalHash(a, b []byte) bool { return hmac.Equal(a, b) }
func positiveTTL(now, end time.Time) time.Duration {
	if ttl := end.Sub(now); ttl > 0 {
		return ttl
	}
	return time.Second
}
func cacheTTL(now, absolute, idle time.Time) time.Duration {
	if idle.Before(absolute) {
		return positiveTTL(now, idle)
	}
	return positiveTTL(now, absolute)
}

func maskedPrefix(raw *string) *string {
	if raw == nil || *raw == "" {
		return nil
	}
	addr, err := netip.ParseAddr(*raw)
	if err != nil {
		return nil
	}
	bits := 64
	if addr.Is4() {
		bits = 24
	}
	masked := netip.PrefixFrom(addr, bits).Masked().String()
	return &masked
}
