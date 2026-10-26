package iam

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresAccountLinkRepository keeps all account-link queries inside iam.
type PostgresAccountLinkRepository struct{ DB *pgxpool.Pool }

func (r PostgresAccountLinkRepository) IsActiveMerchantOwner(ctx context.Context, accountID string) (bool, error) {
	var ok bool
	err := r.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM iam.account a JOIN iam.merchant m ON m.owner_account_id=a.id WHERE a.public_id=$1 AND a.role='merchant_owner' AND a.status='active')`, accountID).Scan(&ok)
	return ok, err
}

func (r PostgresAccountLinkRepository) CandidateBuyers(ctx context.Context, merchantAccountID string) ([]AccountLinkCandidate, error) {
	rows, err := r.DB.Query(ctx, `SELECT b.public_id FROM iam.account m JOIN iam.account b ON b.person_id=m.person_id WHERE m.public_id=$1 AND b.role='buyer' AND b.status='active'`, merchantAccountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AccountLinkCandidate
	for rows.Next() {
		var c AccountLinkCandidate
		if err := rows.Scan(&c.AccountID); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// Link inserts the link and its audit entry in one transaction. A repeated active link maps to
// ErrAccountLinkExists; accounts that fail the same-person / active-owner rule map to
// ErrAccountLinkForbidden, never to a raw database error.
func (r PostgresAccountLinkRepository) Link(ctx context.Context, link AccountLink, audit AccountLinkAudit) (AccountLink, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return AccountLink{}, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `INSERT INTO iam.account_link (public_id,buyer_account_id,merchant_account_id) SELECT $1,b.id,m.id FROM iam.account b, iam.account m WHERE b.public_id=$2 AND m.public_id=$3 RETURNING public_id`, link.PublicID, link.BuyerAccountID, link.MerchantAccountID).Scan(&link.PublicID)
	if err != nil {
		return AccountLink{}, mapAccountLinkError(err)
	}
	if err := insertAccountLinkAudit(ctx, tx, audit); err != nil {
		return AccountLink{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AccountLink{}, err
	}
	return link, nil
}

func (r PostgresAccountLinkRepository) Unlink(ctx context.Context, linkID, actor string, audit AccountLinkAudit) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ct, err := tx.Exec(ctx, `UPDATE iam.account_link l SET unlinked_at=now(), updated_at=now() FROM iam.account m WHERE l.public_id=$1 AND l.merchant_account_id=m.id AND m.public_id=$2 AND l.unlinked_at IS NULL`, linkID, actor)
	if err != nil {
		return err
	}
	if ct.RowsAffected() != 1 {
		return ErrAccountLinkNotFound
	}
	if err := insertAccountLinkAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertAccountLinkAudit(ctx context.Context, tx pgx.Tx, audit AccountLinkAudit) error {
	ct, err := tx.Exec(ctx, `INSERT INTO iam.merchant_op_log (public_id,merchant_id,actor_account_id,action,detail) SELECT $1,a.merchant_id,a.id,$2,jsonb_build_object('account_link_id',$3::text) FROM iam.account a WHERE a.public_id=$4 AND a.merchant_id IS NOT NULL`, audit.LogPublicID, audit.Action, audit.LinkPublicID, audit.ActorAccountID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() != 1 {
		// No merchant-scoped actor row: refuse rather than change links without an audit trail.
		return ErrAccountLinkForbidden
	}
	return nil
}

func mapAccountLinkError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAccountLinkForbidden
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return ErrAccountLinkExists
		case "23514":
			return ErrAccountLinkForbidden
		}
	}
	return err
}

var _ AccountLinkRepository = PostgresAccountLinkRepository{}
