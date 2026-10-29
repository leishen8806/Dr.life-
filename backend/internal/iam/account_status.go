package iam

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

var ErrNoStatusChange = errors.New("account status is unchanged")

type StatusActor struct {
	Type string
	ID   string
}

type AccountStatusChange struct {
	PublicID   string
	AccountID  string
	FromStatus string
	ToStatus   string
	Reason     string
	Actor      StatusActor
	CreatedAt  time.Time
}

type AccountStatusService struct {
	DB          *pgxpool.Pool
	Cache       SessionCache
	AbsoluteTTL time.Duration
	Now         func() time.Time
	NewID       func() string
}

func (s *AccountStatusService) SetAccountStatus(ctx context.Context, accountPublicID, to, reason string, actor StatusActor) (AccountStatusChange, error) {
	reason = strings.TrimSpace(reason)
	actor.ID = strings.TrimSpace(actor.ID)
	if s.DB == nil || accountPublicID == "" || !validAccountStatus(to) || reason == "" || !validStatusActor(actor) {
		return AccountStatusChange{}, httpx.NewAppError(httpx.CodeInvalidArgument)
	}
	now := s.now()
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AccountStatusChange{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id int64
	var from string
	err = tx.QueryRow(ctx, `SELECT id,status FROM iam.account WHERE public_id=$1 FOR UPDATE`, accountPublicID).Scan(&id, &from)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountStatusChange{}, httpx.NewAppError(httpx.CodeResourceNotFound)
	}
	if err != nil {
		return AccountStatusChange{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	if from == to {
		return AccountStatusChange{}, ErrNoStatusChange
	}
	if _, err = tx.Exec(ctx, `UPDATE iam.account SET status=$1,status_reason=CASE WHEN $1='active' THEN NULL ELSE $2 END,updated_at=$3 WHERE id=$4`, to, reason, now, id); err != nil {
		return AccountStatusChange{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	change := AccountStatusChange{PublicID: s.newID(), AccountID: accountPublicID, FromStatus: from, ToStatus: to, Reason: reason, Actor: actor, CreatedAt: now}
	if change.PublicID == "" {
		return AccountStatusChange{}, httpx.NewAppError(httpx.CodeInternalError)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO iam.account_status_log(public_id,account_id,from_status,to_status,reason,actor_type,actor_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, change.PublicID, id, from, to, reason, actor.Type, actor.ID, now); err != nil {
		return AccountStatusChange{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	var epoch int64
	if to != "active" {
		if err = tx.QueryRow(ctx, `UPDATE iam.account SET token_epoch=token_epoch+1,updated_at=$1 WHERE id=$2 RETURNING token_epoch`, now, id).Scan(&epoch); err != nil {
			return AccountStatusChange{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
		}
		if _, err = tx.Exec(ctx, `UPDATE iam.session SET revoked_at=$1,revoked_reason=$2,updated_at=$1 WHERE account_id=$3 AND revoked_at IS NULL`, now, "account_"+to, id); err != nil {
			return AccountStatusChange{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return AccountStatusChange{}, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	if to != "active" && s.Cache != nil {
		_ = s.Cache.SetAccountEpoch(ctx, accountPublicID, epoch, s.absoluteTTL())
	}
	return change, nil
}

func (s *AccountStatusService) StatusHistory(ctx context.Context, accountPublicID string, limit int) ([]AccountStatusChange, error) {
	if s.DB == nil || accountPublicID == "" || limit < 1 || limit > 100 {
		return nil, httpx.NewAppError(httpx.CodeInvalidArgument)
	}
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM iam.account WHERE public_id=$1)`, accountPublicID).Scan(&exists); err != nil {
		return nil, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	if !exists {
		return nil, httpx.NewAppError(httpx.CodeResourceNotFound)
	}
	rows, err := s.DB.Query(ctx, `SELECT l.public_id,l.from_status,l.to_status,l.reason,l.actor_type,l.actor_id,l.created_at FROM iam.account_status_log l JOIN iam.account a ON a.id=l.account_id WHERE a.public_id=$1 ORDER BY l.created_at DESC,l.id DESC LIMIT $2`, accountPublicID, limit)
	if err != nil {
		return nil, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	defer rows.Close()
	changes := make([]AccountStatusChange, 0)
	for rows.Next() {
		var change AccountStatusChange
		change.AccountID = accountPublicID
		if err := rows.Scan(&change.PublicID, &change.FromStatus, &change.ToStatus, &change.Reason, &change.Actor.Type, &change.Actor.ID, &change.CreatedAt); err != nil {
			return nil, httpx.NewAppError(httpx.CodeServiceUnavailable)
		}
		changes = append(changes, change)
	}
	if err := rows.Err(); err != nil {
		return nil, httpx.NewAppError(httpx.CodeServiceUnavailable)
	}
	return changes, nil
}

func validAccountStatus(status string) bool {
	return status == "active" || status == "frozen" || status == "disabled"
}
func validStatusActor(actor StatusActor) bool {
	return (actor.Type == "admin" || actor.Type == "system") && strings.TrimSpace(actor.ID) != ""
}
func (s *AccountStatusService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func (s *AccountStatusService) absoluteTTL() time.Duration {
	if s.AbsoluteTTL > 0 {
		return s.AbsoluteTTL
	}
	return defaultSessionAbsoluteTTL
}
func (s *AccountStatusService) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	id, err := ids.NewULID()
	if err != nil {
		return ""
	}
	return id
}
