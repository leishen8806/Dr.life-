package trade

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

type tradeDB interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// PostgresStore persists only trade-owned tables. Cross-module identifiers are
// received as values by the service and are never joined here.
type PostgresStore struct{ db tradeDB }

type EventStore interface {
	Advance(ctx context.Context, merchantOrderID int64, from, to OrderStatus, source EventSource) (bool, error)
}

var _ Store = (*PostgresStore)(nil)

func NewPostgresStore(db tradeDB) *PostgresStore { return &PostgresStore{db: db} }

func (s *PostgresStore) Create(ctx context.Context, order CreateOrder) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	parentID, err := ids.NewULID()
	if err != nil {
		return err
	}
	var buyerOrderID int64
	if err := tx.QueryRow(ctx, `INSERT INTO trade.buyer_order (public_id,buyer_account_id,total_e4) VALUES ($1,$2,$3) RETURNING id`, parentID, order.BuyerAccountID, order.TotalE4).Scan(&buyerOrderID); err != nil {
		return err
	}
	for _, child := range order.Children {
		publicID, err := ids.NewULID()
		if err != nil {
			return err
		}
		var merchantOrderID int64
		if err := tx.QueryRow(ctx, `INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) VALUES ($1,$2,$3,$4) RETURNING id`, publicID, buyerOrderID, child.MerchantID, child.TotalE4).Scan(&merchantOrderID); err != nil {
			return err
		}
		for _, line := range child.Lines {
			if _, err := tx.Exec(ctx, `INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4) VALUES ($1,$2,$3,$4,$5)`, merchantOrderID, line.SKUPublicID, line.Quantity, line.UnitPriceE4, line.LineTotalE4); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

// Advance locks the suborder, validates the domain transition, and uses the
// source-key uniqueness constraint as the idempotency boundary.
func (s *PostgresStore) Advance(ctx context.Context, merchantOrderID int64, from, to OrderStatus, source EventSource) (bool, error) {
	if _, err := Advance(from, to, source); err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current string
	if err := tx.QueryRow(ctx, `SELECT status FROM trade.merchant_order WHERE id=$1 FOR UPDATE`, merchantOrderID).Scan(&current); err != nil {
		return false, err
	}
	if OrderStatus(current) != from {
		return false, ErrInvalidTransition
	}
	ct, err := tx.Exec(ctx, `INSERT INTO trade.order_event (merchant_order_id,from_status,to_status,source_type,source_ref) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (merchant_order_id,source_type,source_ref) DO NOTHING`, merchantOrderID, string(from), string(to), string(source.Type), source.Reference)
	if err != nil {
		return false, err
	}
	if ct.RowsAffected() == 0 {
		return false, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE trade.merchant_order SET status=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, merchantOrderID, string(to)); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
