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
// verified session. It is the IAM-owned adapter for ledger read authorization.
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
