package kess

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

type db interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// PgStore is limited to the trade schema payment-intent table.
type PgStore struct{ db db }

func NewPgStore(db db) *PgStore { return &PgStore{db: db} }

func (s *PgStore) CreateIntent(ctx context.Context, source ReceivableSnapshot) (Intent, error) {
	if source.CompanyID <= 0 || source.SellerCode == "" || source.SourceRef == "" || source.AttemptNo < 1 {
		return Intent{}, ErrInvalidAmount
	}
	if _, err := FormatAmount(source.AmountE4, source.Currency); err != nil {
		return Intent{}, err
	}
	publicID, err := ids.NewULID()
	if err != nil {
		return Intent{}, err
	}
	intent := Intent{CompanyID: source.CompanyID, Channel: ChannelKESS, TradeNo: fmt.Sprintf("DRL-%s-%d", source.SourceRef, source.AttemptNo), SourceRef: source.SourceRef, AttemptNo: source.AttemptNo, AmountE4: source.AmountE4, Currency: source.Currency, ExpectedSellerCode: source.SellerCode, Status: StatusPending}
	err = s.db.QueryRow(ctx, `
		INSERT INTO trade.kess_payment_intent (public_id, company_id, channel, trade_no, source_ref, attempt_no, amount_e4, currency, seller_code, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (company_id, source_ref, attempt_no) DO NOTHING
		RETURNING id, public_id, trade_no, amount_e4, currency, seller_code, status, create_attempts, created_at`, publicID, intent.CompanyID, intent.Channel, intent.TradeNo, intent.SourceRef, intent.AttemptNo, intent.AmountE4, intent.Currency, intent.ExpectedSellerCode, intent.Status).Scan(&intent.ID, &publicID, &intent.TradeNo, &intent.AmountE4, &intent.Currency, &intent.ExpectedSellerCode, &intent.Status, &intent.CreateAttempts, &intent.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.db.QueryRow(ctx, `
			SELECT id, public_id, trade_no, amount_e4, currency, seller_code, status, create_attempts, created_at
			FROM trade.kess_payment_intent WHERE company_id = $1 AND source_ref = $2 AND attempt_no = $3`, source.CompanyID, source.SourceRef, source.AttemptNo).Scan(&intent.ID, &publicID, &intent.TradeNo, &intent.AmountE4, &intent.Currency, &intent.ExpectedSellerCode, &intent.Status, &intent.CreateAttempts, &intent.CreatedAt)
	}
	if err != nil {
		return Intent{}, err
	}
	if intent.TradeNo != fmt.Sprintf("DRL-%s-%d", source.SourceRef, source.AttemptNo) || intent.AmountE4 != source.AmountE4 || intent.Currency != source.Currency || intent.ExpectedSellerCode != source.SellerCode {
		return Intent{}, ErrConflict
	}
	return intent, nil
}

func (s *PgStore) Intent(ctx context.Context, id int64) (Intent, error) {
	var intent Intent
	err := s.db.QueryRow(ctx, `
		SELECT id, company_id, channel, trade_no, source_ref, attempt_no, amount_e4, currency, seller_code, status, create_attempts, created_at
		FROM trade.kess_payment_intent WHERE id = $1`, id).Scan(
		&intent.ID, &intent.CompanyID, &intent.Channel, &intent.TradeNo, &intent.SourceRef, &intent.AttemptNo, &intent.AmountE4, &intent.Currency, &intent.ExpectedSellerCode, &intent.Status, &intent.CreateAttempts, &intent.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, ErrNotFound
	}
	return intent, err
}

func (s *PgStore) ClaimPending(ctx context.Context, id int64) (Intent, bool, error) {
	var intent Intent
	// shortcut: fixed five-minute claim lease, make it configurable when worker runtimes need a different timeout.
	err := s.db.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id FROM trade.kess_payment_intent
			WHERE id = $1 AND status = 'PENDING'
			  AND (create_claimed_at IS NULL OR create_claimed_at < now() - interval '5 minutes')
			FOR UPDATE SKIP LOCKED
		)
		UPDATE trade.kess_payment_intent AS intent
		SET create_attempts = intent.create_attempts + 1, create_claimed_at = now()
		FROM candidate WHERE intent.id = candidate.id
		RETURNING intent.id, intent.company_id, intent.channel, intent.trade_no, intent.source_ref, intent.attempt_no, intent.amount_e4, intent.currency, intent.seller_code, intent.status, intent.create_attempts, intent.created_at`, id).Scan(
		&intent.ID, &intent.CompanyID, &intent.Channel, &intent.TradeNo, &intent.SourceRef, &intent.AttemptNo, &intent.AmountE4, &intent.Currency, &intent.ExpectedSellerCode, &intent.Status, &intent.CreateAttempts, &intent.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, false, nil
	}
	return intent, err == nil, err
}

func (s *PgStore) ReleasePendingClaim(ctx context.Context, id int64) error {
	command, err := s.db.Exec(ctx, `
		UPDATE trade.kess_payment_intent SET create_claimed_at = NULL
		WHERE id = $1 AND status = 'PENDING' AND create_claimed_at IS NOT NULL`, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (s *PgStore) RecordChannelResult(ctx context.Context, id int64, from Status, result ChannelResult) error {
	command, err := s.db.Exec(ctx, `
		UPDATE trade.kess_payment_intent
		SET status = $2, channel_status = NULLIF($3, ''), channel_transaction_id = NULLIF($4, ''),
			payment_url = COALESCE(NULLIF($5, ''), payment_url), last_checked_at = now()
			, create_claimed_at = NULL
		WHERE id = $1 AND status = $6`, id, result.Status, result.ChannelStatus, result.TransactionID, result.PaymentURL, from)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}
