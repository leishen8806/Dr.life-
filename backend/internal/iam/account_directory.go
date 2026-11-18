package iam

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// AccountResolution is the narrow, read-only IAM fact set that other modules
// may consume. It deliberately excludes personal data and credentials.
type AccountResolution struct {
	ID         int64
	Role       string
	MerchantID *int64
	Status     string
}

type accountDirectoryDB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// AccountDirectory resolves the opaque public account identifier carried in a
// verified session. It is the IAM-owned adapter for cross-module authorization.
type AccountDirectory struct{ DB accountDirectoryDB }

func (d AccountDirectory) Resolve(ctx context.Context, publicID string) (AccountResolution, bool, error) {
	if d.DB == nil || publicID == "" {
		return AccountResolution{}, false, nil
	}
	var result AccountResolution
	err := d.DB.QueryRow(ctx, `SELECT id,role,merchant_id,status FROM iam.account WHERE public_id=$1`, publicID).
		Scan(&result.ID, &result.Role, &result.MerchantID, &result.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountResolution{}, false, nil
	}
	if err != nil {
		return AccountResolution{}, false, err
	}
	return result, true, nil
}

func (d AccountDirectory) Status(ctx context.Context, id int64) (string, error) {
	if d.DB == nil || id <= 0 {
		return "", pgx.ErrNoRows
	}
	var status string
	err := d.DB.QueryRow(ctx, `SELECT status FROM iam.account WHERE id=$1`, id).Scan(&status)
	return status, err
}

// MerchantOwnerAccount returns the active merchant-owner account for a
// merchant. Other modules consume this through their adapter interface rather
// than joining IAM tables.
func (d AccountDirectory) MerchantOwnerAccount(ctx context.Context, merchantID int64) (int64, error) {
	if d.DB == nil || merchantID <= 0 {
		return 0, pgx.ErrNoRows
	}
	var accountID int64
	err := d.DB.QueryRow(ctx, `SELECT owner_account_id FROM iam.merchant WHERE id=$1 AND status='active'`, merchantID).Scan(&accountID)
	return accountID, err
}

// TelegramChatID returns the Telegram chat identifier for an account. It is
// kept in the IAM adapter so notification delivery does not query IAM tables
// from the comms worker.
func (d AccountDirectory) TelegramChatID(ctx context.Context, accountID int64) (int64, error) {
	if d.DB == nil || accountID <= 0 {
		return 0, pgx.ErrNoRows
	}
	var chatID int64
	err := d.DB.QueryRow(ctx, `SELECT p.tg_user_id FROM iam.account a JOIN iam.person p ON p.id=a.person_id WHERE a.id=$1`, accountID).Scan(&chatID)
	return chatID, err
}
