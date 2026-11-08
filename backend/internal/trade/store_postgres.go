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
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// PostgresStore persists only trade-owned tables. Cross-module identifiers are
// received as values by the service and are never joined here.
type PostgresStore struct{ db tradeDB }

type EventStore interface {
	Advance(ctx context.Context, merchantOrderID int64, from, to OrderStatus, source EventSource) (bool, error)
	ConfirmPayment(ctx context.Context, buyerOrderPublicID string, paidAmountE4 int64, source EventSource) (string, bool, error)
}

var _ Store = (*PostgresStore)(nil)
var _ OrderReader = (*PostgresStore)(nil)

func NewPostgresStore(db tradeDB) *PostgresStore { return &PostgresStore{db: db} }

func (s *PostgresStore) FindByIdempotency(ctx context.Context, buyerID int64, key, requestHash string) (CreateOrder, bool, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return CreateOrder{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id, total int64
	var publicID, existingHash string
	err = tx.QueryRow(ctx, `SELECT id,public_id,request_hash,total_e4 FROM trade.buyer_order WHERE buyer_account_id=$1 AND idempotency_key=$2`, buyerID, key).
		Scan(&id, &publicID, &existingHash, &total)
	if errors.Is(err, pgx.ErrNoRows) {
		return CreateOrder{}, false, nil
	}
	if err != nil {
		return CreateOrder{}, false, err
	}
	if existingHash != requestHash {
		return CreateOrder{}, false, ErrIdempotencyConflict
	}
	order, err := loadCreateOrder(ctx, tx, id, publicID, buyerID, key, existingHash, total)
	if err != nil {
		return CreateOrder{}, false, err
	}
	return order, true, tx.Commit(ctx)
}

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
	_, err := s.db.Exec(ctx, `INSERT INTO trade.reservation_release_outbox (reservation_key,action,reason)
		VALUES ($1,'release',$2) ON CONFLICT (reservation_key,action) WHERE completed_at IS NULL DO NOTHING`, reservationKey, reason)
	return err
}

func (s *PostgresStore) QueueReservationReview(ctx context.Context, reservationKey, reason string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO trade.reservation_release_outbox (reservation_key,action,reason)
		VALUES ($1,'review_release',$2) ON CONFLICT (reservation_key,action) WHERE completed_at IS NULL DO NOTHING`, reservationKey, reason)
	return err
}

func (s *PostgresStore) ReservationExists(ctx context.Context, key string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM trade.buyer_order WHERE buyer_account_id || ':' || idempotency_key=$1)`, key).Scan(&exists)
	return exists, err
}

func (s *PostgresStore) CompleteReservationRelease(ctx context.Context, key string) error {
	_, err := s.db.Exec(ctx, `UPDATE trade.reservation_release_outbox SET completed_at=clock_timestamp(),claimed_at=NULL,updated_at=clock_timestamp() WHERE reservation_key=$1 AND action='release' AND completed_at IS NULL`, key)
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

// ClaimReservationRelease is worker-only. It deliberately accepts no buyer
// identity; buyer-facing callers must use a separate ownership-scoped entry.
func (s *PostgresStore) ClaimReservationRelease(ctx context.Context, publicID string) (Reservation, bool, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Reservation{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var reservation Reservation
	var buyerOrderID int64
	var allowed, allCancelled bool
	err = tx.QueryRow(ctx, `SELECT id,buyer_account_id || ':' || idempotency_key,reservation_released_at IS NOT NULL
		FROM trade.buyer_order WHERE public_id=$1 FOR UPDATE`, publicID).Scan(&buyerOrderID, &reservation.Key, &reservation.Released)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, false, ErrOrderNotVisible
	}
	if err != nil {
		return Reservation{}, false, err
	}
	if err := tx.QueryRow(ctx, `SELECT COALESCE(bool_and(status IN ('awaiting_payment','cancelled')),false),COALESCE(bool_and(status='cancelled'),false)
		FROM trade.merchant_order WHERE buyer_order_id=$1`, buyerOrderID).Scan(&allowed, &allCancelled); err != nil {
		return Reservation{}, false, err
	}
	if reservation.Released {
		return reservation, true, tx.Commit(ctx)
	}
	if !allowed {
		return Reservation{}, false, ErrInvalidTransition
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trade.reservation_release_outbox (reservation_key,action,reason)
		VALUES ($1,'release','order_cancelled') ON CONFLICT (reservation_key,action) WHERE completed_at IS NULL DO NOTHING`, reservation.Key); err != nil {
		return Reservation{}, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE trade.buyer_order SET reservation_released_at=clock_timestamp(),updated_at=clock_timestamp() WHERE public_id=$1`, publicID); err != nil {
		return Reservation{}, false, err
	}
	if allCancelled {
		reservation.Status = Cancelled
	} else {
		sourceRef := "reservation-release:" + publicID
		if _, err := tx.Exec(ctx, `INSERT INTO trade.order_event_source (buyer_order_id,source_type,source_ref)
			VALUES ($1,'system',$2) ON CONFLICT (source_type,source_ref) DO NOTHING`, buyerOrderID, sourceRef); err != nil {
			return Reservation{}, false, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO trade.order_event (merchant_order_id,buyer_order_id,from_status,to_status,source_type,source_ref)
			SELECT id,$1,'awaiting_payment','cancelled','system',$2 FROM trade.merchant_order
			WHERE buyer_order_id=$1 AND status='awaiting_payment' ORDER BY id`, buyerOrderID, sourceRef); err != nil {
			return Reservation{}, false, err
		}
		if _, err := tx.Exec(ctx, `UPDATE trade.merchant_order SET status='cancelled',version=version+1,updated_at=clock_timestamp()
			WHERE buyer_order_id=$1 AND status='awaiting_payment'`, buyerOrderID); err != nil {
			return Reservation{}, false, err
		}
		reservation.Status = Cancelled
	}
	return reservation, true, tx.Commit(ctx)
}

func (s *PostgresStore) ClaimReservationActions(ctx context.Context, limit int) ([]ReservationAction, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `
		WITH due AS (
			SELECT id FROM trade.reservation_release_outbox
			WHERE completed_at IS NULL
			  AND (claimed_at IS NULL OR claimed_at < clock_timestamp()-interval '5 minutes')
			  AND next_attempt_at <= clock_timestamp()
			ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED
		)
		UPDATE trade.reservation_release_outbox outbox
		SET claimed_at=clock_timestamp(),attempts=attempts+1,updated_at=clock_timestamp()
		FROM due WHERE outbox.id=due.id RETURNING outbox.id,outbox.reservation_key,outbox.action`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ReservationAction
	for rows.Next() {
		var item ReservationAction
		if err := rows.Scan(&item.ID, &item.Key, &item.Action); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) CompleteReservationAction(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `UPDATE trade.reservation_release_outbox SET completed_at=COALESCE(completed_at,clock_timestamp()),claimed_at=NULL,updated_at=clock_timestamp() WHERE id=$1`, id)
	return err
}

func (s *PostgresStore) FailReservationAction(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `UPDATE trade.reservation_release_outbox SET claimed_at=NULL,next_attempt_at=clock_timestamp()+interval '30 seconds',updated_at=clock_timestamp() WHERE id=$1 AND completed_at IS NULL`, id)
	return err
}

func (s *PostgresStore) CompleteReservationCommit(ctx context.Context, key string) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `UPDATE trade.buyer_order SET reservation_committed_at=COALESCE(reservation_committed_at,clock_timestamp()),updated_at=clock_timestamp()
		WHERE buyer_account_id || ':' || idempotency_key=$1`, key)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrOrderNotVisible
	}
	if _, err := tx.Exec(ctx, `UPDATE trade.reservation_release_outbox SET completed_at=clock_timestamp(),claimed_at=NULL,updated_at=clock_timestamp() WHERE reservation_key=$1 AND action='commit' AND completed_at IS NULL`, key); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) RequireRefund(ctx context.Context, key string) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	if err := tx.QueryRow(ctx, `SELECT id FROM trade.buyer_order WHERE buyer_account_id || ':' || idempotency_key=$1 FOR UPDATE`, key).Scan(&id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE trade.buyer_order SET refund_required_at=COALESCE(refund_required_at,clock_timestamp()),updated_at=clock_timestamp() WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE trade.reservation_release_outbox SET completed_at=clock_timestamp(),claimed_at=NULL,updated_at=clock_timestamp() WHERE reservation_key=$1 AND action='commit' AND completed_at IS NULL`, key); err != nil {
		return err
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
	var buyerOrderID int64
	if err := tx.QueryRow(ctx, `SELECT buyer_order_id FROM trade.merchant_order WHERE id=$1`, merchantOrderID).Scan(&buyerOrderID); err != nil {
		return false, err
	}
	var refundRequired, reservationCommitted bool
	if err := tx.QueryRow(ctx, `SELECT refund_required_at IS NOT NULL,reservation_committed_at IS NOT NULL FROM trade.buyer_order WHERE id=$1 FOR UPDATE`, buyerOrderID).Scan(&refundRequired, &reservationCommitted); err != nil {
		return false, err
	}
	var current string
	if err := tx.QueryRow(ctx, `SELECT status FROM trade.merchant_order WHERE id=$1 AND buyer_order_id=$2 FOR UPDATE`, merchantOrderID, buyerOrderID).Scan(&current); err != nil {
		return false, err
	}
	if refundRequired && from == Paid && to == Submitted {
		return false, ErrReservationReleased
	}
	if !reservationCommitted && from == Paid && to == Submitted {
		return false, ErrInvalidTransition
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

// ConfirmPayment delegates the parent-wide payment transition to the only
// database entry point allowed to mark child orders paid.
func (s *PostgresStore) ConfirmPayment(ctx context.Context, buyerOrderPublicID string, paidAmountE4 int64, source EventSource) (string, bool, error) {
	if source.Type != SourceKESSResult || source.Reference == "" {
		return "", false, ErrEventSourceRequired
	}
	var key string
	var applied, reconciliationRequired bool
	err := s.db.QueryRow(ctx, `SELECT reservation_key,applied,reconciliation_required FROM trade.confirm_order_payment($1,$2,$3)`, buyerOrderPublicID, source.Reference, paidAmountE4).Scan(&key, &applied, &reconciliationRequired)
	if err == nil {
		if reconciliationRequired {
			return key, false, ErrPaymentReconciliation
		}
		return key, applied, nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "TR001":
			return "", false, ErrReservationReleased
		case "TR002":
			return "", false, ErrPaymentAmount
		case "23514", "55000":
			return "", false, ErrInvalidTransition
		}
	}
	return "", false, err
}
