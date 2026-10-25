package iam

import (
	"context"

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
func (r PostgresAccountLinkRepository) Link(ctx context.Context, link AccountLink) (AccountLink, error) {
	err := r.DB.QueryRow(ctx, `INSERT INTO iam.account_link (public_id,buyer_account_id,merchant_account_id) SELECT $1,b.id,m.id FROM iam.account b, iam.account m WHERE b.public_id=$2 AND m.public_id=$3 RETURNING public_id`, link.PublicID, link.BuyerAccountID, link.MerchantAccountID).Scan(&link.PublicID)
	return link, err
}
func (r PostgresAccountLinkRepository) Unlink(ctx context.Context, linkID, actor string) error {
	ct, err := r.DB.Exec(ctx, `UPDATE iam.account_link l SET unlinked_at=now(), updated_at=now() FROM iam.account m WHERE l.public_id=$1 AND l.merchant_account_id=m.id AND m.public_id=$2 AND l.unlinked_at IS NULL`, linkID, actor)
	if err != nil {
		return err
	}
	if ct.RowsAffected() != 1 {
		return ErrAccountLinkNotFound
	}
	return nil
}
func (r PostgresAccountLinkRepository) Audit(ctx context.Context, audit AccountLinkAudit) error {
	_, err := r.DB.Exec(ctx, `INSERT INTO iam.merchant_op_log (public_id,merchant_id,actor_account_id,action,detail) SELECT $1,a.merchant_id,a.id,$2,jsonb_build_object('account_link_id',$3) FROM iam.account a WHERE a.public_id=$4`, audit.LinkPublicID, audit.Action, audit.LinkPublicID, audit.ActorAccountID)
	return err
}

var _ AccountLinkRepository = PostgresAccountLinkRepository{}
