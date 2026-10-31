package iam

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidMerchantID = errors.New("iam: invalid merchant id")
	ErrMerchantNotFound  = errors.New("iam: merchant not found")
)

// MerchantPermInitializer is the IAM-owned primitive used when a merchant is
// created. It copies the fixed role template without creating staff accounts
// or granting owner-level permissions.
type MerchantPermInitializer struct{ db *pgxpool.Pool }

func NewMerchantPermInitializer(db *pgxpool.Pool) *MerchantPermInitializer {
	return &MerchantPermInitializer{db: db}
}

// InitMerchantPerms atomically copies every permission-template row for one
// merchant. Repeated calls are safe and leave the original rows unchanged.
func (s *MerchantPermInitializer) InitMerchantPerms(ctx context.Context, merchantID int64) error {
	if s == nil || s.db == nil || merchantID <= 0 {
		return ErrInvalidMerchantID
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var id int64
	if err := tx.QueryRow(ctx, `SELECT id FROM iam.merchant WHERE id=$1 FOR KEY SHARE`, merchantID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMerchantNotFound
		}
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO iam.merchant_role_perm (merchant_id, role, module, level)
		SELECT $1, role, module, level
		FROM iam.merchant_role_perm_template
		ON CONFLICT (merchant_id, role, module) DO NOTHING`, id); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
