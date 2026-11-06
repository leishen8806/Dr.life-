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
