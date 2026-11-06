package trade

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

type tradeDB interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// PostgresStore persists only trade-owned tables. Cross-module identifiers are
// received as values by the service and are never joined here.
type PostgresStore struct{ db tradeDB }

type EventStore interface {
	Advance(ctx context.Context, merchantOrderID int64, from, to OrderStatus, source EventSource) (bool, error)
}

var _ Store = (*PostgresStore)(nil)
var _ OrderReader = (*PostgresStore)(nil)

func NewPostgresStore(db tradeDB) *PostgresStore { return &PostgresStore{db: db} }

func (s *PostgresStore) FindBuyerOrder(ctx context.Context, buyerID int64, publicID string) (BuyerOrderView, error) {
	var order BuyerOrderView
	err := s.db.QueryRow(ctx, `SELECT public_id,buyer_account_id,total_e4 FROM trade.buyer_order WHERE public_id=$1 AND buyer_account_id=$2`, publicID, buyerID).
		Scan(&order.PublicID, &order.BuyerAccountID, &order.TotalE4)
	if errors.Is(err, pgx.ErrNoRows) {
		return BuyerOrderView{}, ErrOrderNotVisible
	}
	return order, err
}

func (s *PostgresStore) FindMerchantOrder(ctx context.Context, merchantID int64, publicID string) (MerchantOrderView, error) {
	var order MerchantOrderView
	err := s.db.QueryRow(ctx, `SELECT public_id,merchant_id,status,total_e4 FROM trade.merchant_order WHERE public_id=$1 AND merchant_id=$2`, publicID, merchantID).
		Scan(&order.PublicID, &order.MerchantID, &order.Status, &order.TotalE4)
	if errors.Is(err, pgx.ErrNoRows) {
		return MerchantOrderView{}, ErrOrderNotVisible
	}
	return order, err
}

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

// Advance locks the suborder and binds each external source reference to one
// buyer order before recording the transition.
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
	var buyerOrderID int64
	if err := tx.QueryRow(ctx, `SELECT status,buyer_order_id FROM trade.merchant_order WHERE id=$1 FOR UPDATE`, merchantOrderID).Scan(&current, &buyerOrderID); err != nil {
		return false, err
	}
	var seenFrom, seenTo string
	err = tx.QueryRow(ctx, `SELECT from_status,to_status FROM trade.order_event WHERE merchant_order_id=$1 AND source_type=$2 AND source_ref=$3`, merchantOrderID, string(source.Type), source.Reference).Scan(&seenFrom, &seenTo)
	if err == nil {
		if OrderStatus(seenFrom) == from && OrderStatus(seenTo) == to {
			return false, tx.Commit(ctx)
		}
		return false, ErrInvalidTransition
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	if OrderStatus(current) != from {
		return false, ErrInvalidTransition
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trade.order_event_source (buyer_order_id,source_type,source_ref) VALUES ($1,$2,$3) ON CONFLICT (source_type,source_ref) DO NOTHING`, buyerOrderID, string(source.Type), source.Reference); err != nil {
		return false, err
	}
	var sourceBuyerOrderID int64
	if err := tx.QueryRow(ctx, `SELECT buyer_order_id FROM trade.order_event_source WHERE source_type=$1 AND source_ref=$2`, string(source.Type), source.Reference).Scan(&sourceBuyerOrderID); err != nil {
		return false, err
	}
	if sourceBuyerOrderID != buyerOrderID {
		return false, ErrInvalidTransition
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trade.order_event (merchant_order_id,buyer_order_id,from_status,to_status,source_type,source_ref) VALUES ($1,$2,$3,$4,$5,$6)`, merchantOrderID, buyerOrderID, string(from), string(to), string(source.Type), source.Reference); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE trade.merchant_order SET status=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, merchantOrderID, string(to)); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
