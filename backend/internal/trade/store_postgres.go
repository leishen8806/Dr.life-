package trade

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

type tradeDB interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// PostgresStore persists only trade-owned tables. Cross-module identifiers are
// received as values by the service and are never joined here.
type PostgresStore struct{ db tradeDB }

type EventStore interface {
	Advance(ctx context.Context, merchantOrderID int64, from, to OrderStatus, source EventSource) (bool, error)
	ConfirmPayment(ctx context.Context, buyerOrderPublicID string, source EventSource) (bool, error)
}

var _ Store = (*PostgresStore)(nil)
var _ OrderReader = (*PostgresStore)(nil)

func NewPostgresStore(db tradeDB) *PostgresStore { return &PostgresStore{db: db} }

func (s *PostgresStore) FindBuyerOrder(ctx context.Context, buyerID int64, publicID string) (BuyerOrderView, error) {
	var order BuyerOrderView
	err := s.db.QueryRow(ctx, `
		SELECT buyer.public_id,buyer.buyer_account_id,buyer.total_e4,
		       CASE WHEN min(child.status) = max(child.status) THEN min(child.status) ELSE 'mixed' END
		FROM trade.buyer_order buyer JOIN trade.merchant_order child ON child.buyer_order_id=buyer.id
		WHERE buyer.public_id=$1 AND buyer.buyer_account_id=$2
		GROUP BY buyer.id`, publicID, buyerID).
		Scan(&order.PublicID, &order.BuyerAccountID, &order.TotalE4, &order.Status)
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

func (s *PostgresStore) Create(ctx context.Context, idempotencyKey, requestHash string, order CreateOrder) (CreateOrder, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CreateOrder{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	parentID, err := ids.NewULID()
	if err != nil {
		return CreateOrder{}, err
	}
	var buyerOrderID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (buyer_account_id,idempotency_key) DO NOTHING
		RETURNING id`, parentID, order.BuyerAccountID, idempotencyKey, requestHash, order.TotalE4).Scan(&buyerOrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		var existingHash string
		if err := tx.QueryRow(ctx, `SELECT id,public_id,request_hash,total_e4 FROM trade.buyer_order WHERE buyer_account_id=$1 AND idempotency_key=$2`, order.BuyerAccountID, idempotencyKey).Scan(&buyerOrderID, &parentID, &existingHash, &order.TotalE4); err != nil {
			return CreateOrder{}, err
		}
		if existingHash != requestHash {
			return CreateOrder{}, ErrIdempotencyConflict
		}
		order, err = loadCreateOrder(ctx, tx, buyerOrderID, parentID, order.BuyerAccountID, idempotencyKey, requestHash, order.TotalE4)
		if err != nil {
			return CreateOrder{}, err
		}
		return order, tx.Commit(ctx)
	}
	if err != nil {
		return CreateOrder{}, err
	}
	for _, child := range order.Children {
		publicID, err := ids.NewULID()
		if err != nil {
			return CreateOrder{}, err
		}
		var merchantOrderID int64
		if err := tx.QueryRow(ctx, `INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,total_e4) VALUES ($1,$2,$3,$4) RETURNING id`, publicID, buyerOrderID, child.MerchantID, child.TotalE4).Scan(&merchantOrderID); err != nil {
			return CreateOrder{}, err
		}
		for _, line := range child.Lines {
			if _, err := tx.Exec(ctx, `INSERT INTO trade.order_line (merchant_order_id,sku_public_id,quantity,unit_price_e4,line_total_e4) VALUES ($1,$2,$3,$4,$5)`, merchantOrderID, line.SKUPublicID, line.Quantity, line.UnitPriceE4, line.LineTotalE4); err != nil {
				return CreateOrder{}, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CreateOrder{}, err
	}
	order.PublicID, order.IdempotencyKey, order.RequestHash = parentID, idempotencyKey, requestHash
	return order, nil
}

func (s *PostgresStore) QueueReservationRelease(ctx context.Context, reservationKey, reason string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO trade.reservation_release_outbox (reservation_key,reason)
		VALUES ($1,$2) ON CONFLICT (reservation_key) DO UPDATE SET reason=EXCLUDED.reason,updated_at=clock_timestamp()`, reservationKey, reason)
	return err
}

func loadCreateOrder(ctx context.Context, tx pgx.Tx, buyerOrderID int64, publicID string, buyerID int64, key, requestHash string, total int64) (CreateOrder, error) {
	order := CreateOrder{PublicID: publicID, BuyerAccountID: buyerID, IdempotencyKey: key, RequestHash: requestHash, TotalE4: total}
	rows, err := tx.Query(ctx, `SELECT id,merchant_id,total_e4 FROM trade.merchant_order WHERE buyer_order_id=$1 ORDER BY id`, buyerOrderID)
	if err != nil {
		return CreateOrder{}, err
	}
	type storedChild struct {
		id    int64
		child ChildOrder
	}
	var children []storedChild
	for rows.Next() {
		var child storedChild
		if err := rows.Scan(&child.id, &child.child.MerchantID, &child.child.TotalE4); err != nil {
			rows.Close()
			return CreateOrder{}, err
		}
		children = append(children, child)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return CreateOrder{}, err
	}
	rows.Close()
	for _, stored := range children {
		child := stored.child
		lineRows, err := tx.Query(ctx, `SELECT sku_public_id,quantity,unit_price_e4,line_total_e4 FROM trade.order_line WHERE merchant_order_id=$1 ORDER BY id`, stored.id)
		if err != nil {
			return CreateOrder{}, err
		}
		for lineRows.Next() {
			var line OrderItem
			line.MerchantID = child.MerchantID
			if err := lineRows.Scan(&line.SKUPublicID, &line.Quantity, &line.UnitPriceE4, &line.LineTotalE4); err != nil {
				lineRows.Close()
				return CreateOrder{}, err
			}
			child.Lines = append(child.Lines, line)
		}
		if err := lineRows.Err(); err != nil {
			lineRows.Close()
			return CreateOrder{}, err
		}
		lineRows.Close()
		order.Children = append(order.Children, child)
	}
	return order, nil
}

func (s *PostgresStore) ReservationForRelease(ctx context.Context, publicID string) (Reservation, error) {
	var reservation Reservation
	var allowed, allCancelled bool
	err := s.db.QueryRow(ctx, `
		SELECT buyer.buyer_account_id || ':' || buyer.idempotency_key,
		       bool_and(child.status IN ('awaiting_payment','cancelled')),
		       bool_and(child.status='cancelled'), buyer.reservation_released_at IS NOT NULL
		FROM trade.buyer_order buyer JOIN trade.merchant_order child ON child.buyer_order_id=buyer.id
		WHERE buyer.public_id=$1 GROUP BY buyer.id`, publicID).Scan(&reservation.Key, &allowed, &allCancelled, &reservation.Released)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, ErrOrderNotVisible
	}
	if err != nil {
		return Reservation{}, err
	}
	if allCancelled {
		reservation.Status = Cancelled
	} else if allowed {
		reservation.Status = AwaitingPayment
	} else {
		reservation.Status = Paid
	}
	return reservation, nil
}

func (s *PostgresStore) MarkReservationReleased(ctx context.Context, publicID string) (bool, error) {
	result, err := s.db.Exec(ctx, `UPDATE trade.buyer_order SET reservation_released_at=clock_timestamp(),updated_at=clock_timestamp() WHERE public_id=$1 AND reservation_released_at IS NULL`, publicID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
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

// ConfirmPayment advances every child of one parent order in one transaction.
func (s *PostgresStore) ConfirmPayment(ctx context.Context, buyerOrderPublicID string, source EventSource) (bool, error) {
	if source.Type != SourceKESSResult || source.Reference == "" {
		return false, ErrEventSourceRequired
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var buyerOrderID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM trade.buyer_order WHERE public_id=$1 FOR UPDATE`, buyerOrderPublicID).Scan(&buyerOrderID); err != nil {
		return false, err
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
	rows, err := tx.Query(ctx, `SELECT id,status FROM trade.merchant_order WHERE buyer_order_id=$1 ORDER BY id FOR UPDATE`, buyerOrderID)
	if err != nil {
		return false, err
	}
	type childState struct {
		id     int64
		status OrderStatus
	}
	var children []childState
	for rows.Next() {
		var child childState
		if err := rows.Scan(&child.id, &child.status); err != nil {
			rows.Close()
			return false, err
		}
		children = append(children, child)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()
	if len(children) == 0 {
		return false, ErrInvalidTransition
	}
	allPaid := true
	for _, child := range children {
		if child.status != Paid {
			allPaid = false
		}
		if child.status != Paid && child.status != AwaitingPayment {
			return false, ErrInvalidTransition
		}
	}
	if allPaid {
		return false, tx.Commit(ctx)
	}
	for _, child := range children {
		if child.status != AwaitingPayment {
			return false, ErrInvalidTransition
		}
	}
	for _, child := range children {
		if _, err := tx.Exec(ctx, `INSERT INTO trade.order_event (merchant_order_id,buyer_order_id,from_status,to_status,source_type,source_ref) VALUES ($1,$2,$3,$4,$5,$6)`, child.id, buyerOrderID, string(AwaitingPayment), string(Paid), string(source.Type), source.Reference); err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, `UPDATE trade.merchant_order SET status=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, child.id, string(Paid)); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}
