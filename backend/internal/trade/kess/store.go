package kess

import (
	"context"
	"errors"

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
	if source.CompanyID <= 0 || source.SellerCode == "" {
		return Intent{}, ErrInvalidAmount
	}
	if _, err := FormatAmount(source.Amount, source.Currency); err != nil {
		return Intent{}, err
	}
	publicID, err := ids.NewULID()
	if err != nil {
		return Intent{}, err
	}
	intent := Intent{CompanyID: source.CompanyID, Channel: ChannelKESS, TradeNo: "DRL-" + publicID, Amount: source.Amount, Currency: source.Currency, ExpectedSellerCode: source.SellerCode, Status: StatusPending}
	err = s.db.QueryRow(ctx, `
		INSERT INTO trade.kess_payment_intent (public_id, company_id, channel, trade_no, amount_minor, currency, seller_code, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`, publicID, intent.CompanyID, intent.Channel, intent.TradeNo, intent.Amount, intent.Currency, intent.ExpectedSellerCode, intent.Status).Scan(&intent.ID)
	if err != nil {
		return Intent{}, err
	}
	return intent, nil
}

func (s *PgStore) Intent(ctx context.Context, id int64) (Intent, error) {
	var intent Intent
	err := s.db.QueryRow(ctx, `
		SELECT id, company_id, channel, trade_no, amount_minor, currency, seller_code, status, create_attempts
		FROM trade.kess_payment_intent WHERE id = $1`, id).Scan(
		&intent.ID, &intent.CompanyID, &intent.Channel, &intent.TradeNo, &intent.Amount, &intent.Currency, &intent.ExpectedSellerCode, &intent.Status, &intent.CreateAttempts,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, ErrNotFound
	}
	return intent, err
}

func (s *PgStore) RecordCreateAttempt(ctx context.Context, id int64) error {
	command, err := s.db.Exec(ctx, `UPDATE trade.kess_payment_intent SET create_attempts = create_attempts + 1 WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PgStore) RecordChannelResult(ctx context.Context, id int64, result ChannelResult) error {
	command, err := s.db.Exec(ctx, `
		UPDATE trade.kess_payment_intent
		SET status = $2, channel_status = NULLIF($3, ''), channel_transaction_id = NULLIF($4, ''),
			payment_url = NULLIF($5, ''), last_checked_at = now()
		WHERE id = $1`, id, result.Status, result.ChannelStatus, result.TransactionID, result.PaymentURL)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
