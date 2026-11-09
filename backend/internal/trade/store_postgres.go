package trade

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"time"

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
	order.GoodsAmountE4 = order.TotalE4
	order.PayableAmountE4 = order.TotalE4
	if errors.Is(err, pgx.ErrNoRows) {
		return BuyerOrderView{}, ErrOrderNotVisible
	}
	return order, err
}

func (s *PostgresStore) FindMerchantOrder(ctx context.Context, merchantID int64, publicID string) (MerchantOrderView, error) {
	var order MerchantOrderView
	err := s.db.QueryRow(ctx, `SELECT public_id,merchant_id,status,total_e4 FROM trade.merchant_order WHERE public_id=$1 AND merchant_id=$2`, publicID, merchantID).
		Scan(&order.PublicID, &order.MerchantID, &order.Status, &order.TotalE4)
	order.GoodsAmountE4 = order.TotalE4
	order.PayableAmountE4 = order.TotalE4
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
	goods := order.GoodsAmountE4
	if goods == 0 && order.TotalE4 != 0 {
		goods = order.TotalE4
	}
	payable := order.PayableAmountE4
	if payable == 0 {
		payable = goods + order.ShippingAmountE4
	}
	order.TotalE4, order.GoodsAmountE4, order.PayableAmountE4 = goods, goods, payable
	err = tx.QueryRow(ctx, `
		INSERT INTO trade.buyer_order (public_id,buyer_account_id,idempotency_key,request_hash,total_e4,goods_amount_e4,shipping_amount_e4,payable_amount_e4,shipping_quote_expires_at,shipping_address_public_id,shipping_recipient_name,shipping_address_text,shipping_phone)
		VALUES ($1,$2,$3,$4,$5,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),NULLIF($12,'')) ON CONFLICT (buyer_account_id,idempotency_key) DO NOTHING
		RETURNING id`, parentID, order.BuyerAccountID, idempotencyKey, requestHash, goods, order.ShippingAmountE4, payable, nullableTime(order.ShippingQuoteExpiresAt), order.ShippingAddress.PublicID, order.ShippingAddress.RecipientName, order.ShippingAddress.AddressText, order.ShippingAddress.Phone).Scan(&buyerOrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		var existingHash string
		if err := tx.QueryRow(ctx, `SELECT id,public_id,request_hash,goods_amount_e4 FROM trade.buyer_order WHERE buyer_account_id=$1 AND idempotency_key=$2`, order.BuyerAccountID, idempotencyKey).Scan(&buyerOrderID, &parentID, &existingHash, &order.TotalE4); err != nil {
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
		childGoods := child.GoodsAmountE4
		if childGoods == 0 && child.TotalE4 != 0 {
			childGoods = child.TotalE4
		}
		childPayable := child.PayableAmountE4
		if childPayable == 0 {
			childPayable = childGoods + child.ShippingAmountE4
		}
		if err := tx.QueryRow(ctx, `INSERT INTO trade.merchant_order (public_id,buyer_order_id,merchant_id,merchant_public_id,total_e4,goods_amount_e4,shipping_amount_e4,payable_amount_e4,shipping_quote_expires_at) VALUES ($1,$2,$3,NULLIF($4,''),$5,$5,$6,$7,$8) RETURNING id`, publicID, buyerOrderID, child.MerchantID, child.MerchantPublicID, childGoods, child.ShippingAmountE4, childPayable, nullableTime(child.ShippingQuoteExpiresAt)).Scan(&merchantOrderID); err != nil {
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
	order := CreateOrder{PublicID: publicID, BuyerAccountID: buyerID, IdempotencyKey: key, RequestHash: requestHash, TotalE4: total, GoodsAmountE4: total}
	if err := tx.QueryRow(ctx, `SELECT shipping_amount_e4,payable_amount_e4,COALESCE(shipping_quote_expires_at,'epoch'),COALESCE(shipping_address_public_id,''),COALESCE(shipping_recipient_name,''),COALESCE(shipping_address_text,''),COALESCE(shipping_phone,'') FROM trade.buyer_order WHERE id=$1`, buyerOrderID).Scan(&order.ShippingAmountE4, &order.PayableAmountE4, &order.ShippingQuoteExpiresAt, &order.ShippingAddress.PublicID, &order.ShippingAddress.RecipientName, &order.ShippingAddress.AddressText, &order.ShippingAddress.Phone); err != nil {
		return CreateOrder{}, err
	}
	rows, err := tx.Query(ctx, `SELECT id,merchant_id,COALESCE(merchant_public_id,''),goods_amount_e4,shipping_amount_e4,payable_amount_e4,COALESCE(shipping_quote_expires_at,'epoch') FROM trade.merchant_order WHERE buyer_order_id=$1 ORDER BY id`, buyerOrderID)
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
		if err := rows.Scan(&child.id, &child.child.MerchantID, &child.child.MerchantPublicID, &child.child.GoodsAmountE4, &child.child.ShippingAmountE4, &child.child.PayableAmountE4, &child.child.ShippingQuoteExpiresAt); err != nil {
			rows.Close()
			return CreateOrder{}, err
		}
		child.child.TotalE4 = child.child.GoodsAmountE4
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
	var key, errorCode string
	var applied, reconciliationRequired bool
	err := s.db.QueryRow(ctx, `SELECT reservation_key,applied,reconciliation_required,error_code FROM trade.confirm_order_payment($1,$2,$3)`, buyerOrderPublicID, source.Reference, paidAmountE4).Scan(&key, &applied, &reconciliationRequired, &errorCode)
	if err == nil {
		switch errorCode {
		case "TR001":
			return key, false, ErrReservationReleased
		case "TR002":
			return key, false, ErrPaymentAmount
		}
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

func (s *PostgresStore) ListBuyerOrders(ctx context.Context, buyerID int64, cursor string, limit int) ([]BuyerOrderView, string, error) {
	after, err := decodeCursor(cursor)
	if err != nil {
		return nil, "", ErrInvalidOrderLine
	}
	limit = pageLimit(limit)
	rows, err := s.db.Query(ctx, `SELECT id,public_id FROM trade.buyer_order WHERE buyer_account_id=$1 AND ($2=0 OR id<$2) ORDER BY id DESC LIMIT $3`, buyerID, after, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	type item struct {
		id       int64
		publicID string
	}
	var ids []item
	for rows.Next() {
		var value item
		if err := rows.Scan(&value.id, &value.publicID); err != nil {
			return nil, "", err
		}
		ids = append(ids, value)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(ids) > limit {
		next = encodeCursor(ids[limit-1].id)
		ids = ids[:limit]
	}
	orders := make([]BuyerOrderView, 0, len(ids))
	for _, id := range ids {
		order, err := s.FindBuyerOrderDetail(ctx, buyerID, id.publicID)
		if err != nil {
			return nil, "", err
		}
		orders = append(orders, order)
	}
	return orders, next, nil
}

func (s *PostgresStore) FindBuyerOrderDetail(ctx context.Context, buyerID int64, publicID string) (BuyerOrderView, error) {
	var order BuyerOrderView
	var paymentIntentStatus string
	err := s.db.QueryRow(ctx, `
		SELECT buyer.id,buyer.public_id,buyer.buyer_account_id,buyer.goods_amount_e4,buyer.shipping_amount_e4,buyer.payable_amount_e4,
		       COALESCE(buyer.shipping_address_public_id,''),COALESCE(buyer.shipping_recipient_name,''),COALESCE(buyer.shipping_address_text,''),COALESCE(buyer.shipping_phone,''),
		       buyer.created_at,buyer.updated_at,
		       CASE WHEN min(child.status)=max(child.status) THEN min(child.status) ELSE 'mixed' END,
		       COALESCE(latest.status,'')
		FROM trade.buyer_order buyer
		JOIN trade.merchant_order child ON child.buyer_order_id=buyer.id
		LEFT JOIN LATERAL (SELECT status FROM trade.kess_payment_intent WHERE source_ref=buyer.public_id ORDER BY (status='SUCCESS') DESC,attempt_no DESC LIMIT 1) latest ON true
		WHERE buyer.public_id=$1 AND buyer.buyer_account_id=$2
		GROUP BY buyer.id,latest.status`, publicID, buyerID).Scan(
		new(int64), &order.PublicID, &order.BuyerAccountID, &order.GoodsAmountE4, &order.ShippingAmountE4, &order.PayableAmountE4,
		&order.ShippingAddress.PublicID, &order.ShippingAddress.RecipientName, &order.ShippingAddress.AddressText, &order.ShippingAddress.Phone,
		&order.CreatedAt, &order.UpdatedAt, &order.Status, &paymentIntentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return BuyerOrderView{}, ErrOrderNotVisible
	}
	if err != nil {
		return BuyerOrderView{}, err
	}
	order.TotalE4 = order.GoodsAmountE4
	order.PaymentStatus = projectPaymentStatus(paymentIntentStatus)
	rows, err := s.db.Query(ctx, `SELECT public_id FROM trade.merchant_order WHERE buyer_order_id=(SELECT id FROM trade.buyer_order WHERE public_id=$1) ORDER BY id`, publicID)
	if err != nil {
		return BuyerOrderView{}, err
	}
	var childIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return BuyerOrderView{}, err
		}
		childIDs = append(childIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return BuyerOrderView{}, err
	}
	rows.Close()
	for _, id := range childIDs {
		child, err := s.findMerchantOrder(ctx, 0, id, false)
		if err != nil {
			return BuyerOrderView{}, err
		}
		order.Children = append(order.Children, child)
	}
	return order, nil
}

func (s *PostgresStore) ListMerchantOrders(ctx context.Context, merchantID int64, cursor string, limit int) ([]MerchantOrderView, string, error) {
	after, err := decodeCursor(cursor)
	if err != nil {
		return nil, "", ErrInvalidOrderLine
	}
	limit = pageLimit(limit)
	rows, err := s.db.Query(ctx, `SELECT id,public_id FROM trade.merchant_order WHERE merchant_id=$1 AND ($2=0 OR id<$2) ORDER BY id DESC LIMIT $3`, merchantID, after, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	type item struct {
		id       int64
		publicID string
	}
	var ids []item
	for rows.Next() {
		var value item
		if err := rows.Scan(&value.id, &value.publicID); err != nil {
			return nil, "", err
		}
		ids = append(ids, value)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(ids) > limit {
		next = encodeCursor(ids[limit-1].id)
		ids = ids[:limit]
	}
	orders := make([]MerchantOrderView, 0, len(ids))
	for _, id := range ids {
		order, err := s.findMerchantOrder(ctx, merchantID, id.publicID, true)
		if err != nil {
			return nil, "", err
		}
		orders = append(orders, order)
	}
	return orders, next, nil
}

func (s *PostgresStore) FindMerchantOrderDetail(ctx context.Context, merchantID int64, publicID string) (MerchantOrderView, error) {
	return s.findMerchantOrder(ctx, merchantID, publicID, true)
}

func (s *PostgresStore) findMerchantOrder(ctx context.Context, merchantID int64, publicID string, scoped bool) (MerchantOrderView, error) {
	var order MerchantOrderView
	err := s.db.QueryRow(ctx, `
		SELECT child.id,child.public_id,parent.public_id,child.merchant_id,COALESCE(child.merchant_public_id,''),child.status,child.goods_amount_e4,child.shipping_amount_e4,child.payable_amount_e4,
		       COALESCE(child.shipping_quote_expires_at,'epoch'),COALESCE(parent.shipping_address_public_id,''),COALESCE(parent.shipping_recipient_name,''),COALESCE(parent.shipping_address_text,''),COALESCE(parent.shipping_phone,''),child.created_at,child.updated_at
		FROM trade.merchant_order child JOIN trade.buyer_order parent ON parent.id=child.buyer_order_id
		WHERE child.public_id=$1 AND (NOT $3 OR child.merchant_id=$2)`, publicID, merchantID, scoped).Scan(
		new(int64), &order.PublicID, &order.ParentPublicID, &order.MerchantID, &order.MerchantPublicID, &order.Status, &order.GoodsAmountE4, &order.ShippingAmountE4, &order.PayableAmountE4,
		&order.ShippingQuoteExpiresAt, &order.ShippingAddress.PublicID, &order.ShippingAddress.RecipientName, &order.ShippingAddress.AddressText, &order.ShippingAddress.Phone, &order.CreatedAt, &order.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return MerchantOrderView{}, ErrOrderNotVisible
	}
	if err != nil {
		return MerchantOrderView{}, err
	}
	order.TotalE4 = order.GoodsAmountE4
	rows, err := s.db.Query(ctx, `SELECT sku_public_id,quantity,unit_price_e4,line_total_e4 FROM trade.order_line WHERE merchant_order_id=(SELECT id FROM trade.merchant_order WHERE public_id=$1) ORDER BY id`, publicID)
	if err != nil {
		return MerchantOrderView{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var line OrderLineView
		if err := rows.Scan(&line.SKUPublicID, &line.Quantity, &line.UnitPriceE4, &line.TotalE4); err != nil {
			return MerchantOrderView{}, err
		}
		order.Lines = append(order.Lines, line)
	}
	return order, rows.Err()
}

func (s *PostgresStore) ClaimBuyerReservationRelease(ctx context.Context, buyerID int64, publicID string) (Reservation, bool, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Reservation{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var reservation Reservation
	var orderID int64
	var confirming, paid, allowed, allCancelled bool
	err = tx.QueryRow(ctx, `SELECT id,buyer_account_id || ':' || idempotency_key,reservation_released_at IS NOT NULL FROM trade.buyer_order WHERE public_id=$1 AND buyer_account_id=$2 FOR UPDATE`, publicID, buyerID).Scan(&orderID, &reservation.Key, &reservation.Released)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, false, ErrOrderNotVisible
	}
	if err != nil {
		return Reservation{}, false, err
	}
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trade.kess_payment_intent WHERE source_ref=$1 AND status IN ('UNKNOWN','REVIEW'))`, publicID).Scan(&confirming); err != nil {
		return Reservation{}, false, err
	}
	if confirming {
		return Reservation{}, false, ErrPaymentConfirming
	}
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trade.kess_payment_intent WHERE source_ref=$1 AND status IN ('SUCCESS','REFUND'))`, publicID).Scan(&paid); err != nil {
		return Reservation{}, false, err
	}
	if paid {
		return Reservation{}, false, ErrInvalidTransition
	}
	if err := tx.QueryRow(ctx, `SELECT COALESCE(bool_and(status IN ('awaiting_payment','cancelled')),false),COALESCE(bool_and(status='cancelled'),false) FROM trade.merchant_order WHERE buyer_order_id=$1`, orderID).Scan(&allowed, &allCancelled); err != nil {
		return Reservation{}, false, err
	}
	if reservation.Released {
		return reservation, true, tx.Commit(ctx)
	}
	if !allowed {
		return Reservation{}, false, ErrInvalidTransition
	}
	if _, err := tx.Exec(ctx, `UPDATE trade.kess_payment_intent SET status='INVALIDATED' WHERE source_ref=$1 AND status IN ('PENDING','WAITING')`, publicID); err != nil {
		return Reservation{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trade.reservation_release_outbox (reservation_key,action,reason) VALUES ($1,'release','order_cancelled') ON CONFLICT (reservation_key,action) WHERE completed_at IS NULL DO NOTHING`, reservation.Key); err != nil {
		return Reservation{}, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE trade.buyer_order SET reservation_released_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, orderID); err != nil {
		return Reservation{}, false, err
	}
	if !allCancelled {
		sourceRef := "buyer-cancel:" + publicID
		if _, err := tx.Exec(ctx, `INSERT INTO trade.order_event_source (buyer_order_id,source_type,source_ref) VALUES ($1,'system',$2) ON CONFLICT (source_type,source_ref) DO NOTHING`, orderID, sourceRef); err != nil {
			return Reservation{}, false, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO trade.order_event (merchant_order_id,buyer_order_id,from_status,to_status,source_type,source_ref) SELECT id,$1,'awaiting_payment','cancelled','system',$2 FROM trade.merchant_order WHERE buyer_order_id=$1 AND status='awaiting_payment' ORDER BY id`, orderID, sourceRef); err != nil {
			return Reservation{}, false, err
		}
		if _, err := tx.Exec(ctx, `UPDATE trade.merchant_order SET status='cancelled',version=version+1,updated_at=clock_timestamp() WHERE buyer_order_id=$1 AND status='awaiting_payment'`, orderID); err != nil {
			return Reservation{}, false, err
		}
	}
	reservation.Status = Cancelled
	return reservation, true, tx.Commit(ctx)
}

func (s *PostgresStore) PaymentAttempt(ctx context.Context, buyerID int64, publicID string) (PaymentAttempt, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PaymentAttempt{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var attempt PaymentAttempt
	var orderID int64
	var released bool
	var quoteExpiry *time.Time
	var latestStatus, latestNo, latestURL *string
	var latestExpiry *time.Time
	var latestAttempt *int
	err = tx.QueryRow(ctx, `SELECT id,payable_amount_e4,reservation_released_at IS NOT NULL OR refund_required_at IS NOT NULL,shipping_quote_expires_at
		FROM trade.buyer_order WHERE public_id=$1 AND buyer_account_id=$2 FOR UPDATE`, publicID, buyerID).Scan(&orderID, &attempt.AmountE4, &released, &quoteExpiry)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentAttempt{}, ErrOrderNotVisible
	}
	if err != nil {
		return PaymentAttempt{}, err
	}
	if released {
		return PaymentAttempt{}, ErrReservationReleased
	}
	var childCount, unavailableCount int
	if err := tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE status<>'awaiting_payment') FROM trade.merchant_order WHERE buyer_order_id=$1`, orderID).Scan(&childCount, &unavailableCount); err != nil {
		return PaymentAttempt{}, err
	}
	if childCount == 0 {
		return PaymentAttempt{}, ErrOrderNotVisible
	}
	if unavailableCount != 0 {
		return PaymentAttempt{}, ErrInvalidTransition
	}
	if quoteExpiry == nil || !quoteExpiry.After(time.Now()) {
		return PaymentAttempt{}, ErrShippingQuoteExpired
	}
	if err := tx.QueryRow(ctx, `SELECT status,trade_no,payment_url,expires_at,attempt_no FROM trade.kess_payment_intent WHERE source_ref=$1 ORDER BY attempt_no DESC LIMIT 1`, publicID).Scan(&latestStatus, &latestNo, &latestURL, &latestExpiry, &latestAttempt); errors.Is(err, pgx.ErrNoRows) {
		latestStatus = nil
	} else if err != nil {
		return PaymentAttempt{}, err
	}
	if latestStatus != nil {
		switch *latestStatus {
		case "UNKNOWN", "REVIEW":
			return PaymentAttempt{}, ErrPaymentConfirming
		case "SUCCESS", "REFUND":
			return PaymentAttempt{}, ErrInvalidTransition
		case "INVALIDATED":
			return PaymentAttempt{}, ErrReservationReleased
		case "PENDING", "WAITING":
			if latestNo != nil && latestURL != nil && *latestURL != "" && latestExpiry != nil && latestExpiry.After(time.Now()) {
				attempt.Existing = &PaymentResult{PaymentNo: *latestNo, PaymentLink: *latestURL, ExpiresAt: *latestExpiry}
				return attempt, tx.Commit(ctx)
			}
			return PaymentAttempt{}, ErrPaymentConfirming
		}
		if *latestStatus != "FAILED" && *latestStatus != "CLOSED" {
			return PaymentAttempt{}, ErrInvalidTransition
		}
		attempt.AttemptNo = *latestAttempt + 1
	} else {
		attempt.AttemptNo = 1
	}
	return attempt, tx.Commit(ctx)
}

func pageLimit(limit int) int {
	if limit < 1 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}
func encodeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}
func decodeCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid cursor")
	}
	return id, nil
}
func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func projectPaymentStatus(status string) string {
	switch status {
	case "UNKNOWN", "REVIEW":
		return "confirming"
	case "SUCCESS":
		return "succeeded"
	case "FAILED", "CLOSED", "INVALIDATED":
		return "failed"
	case "REFUND":
		return "refunded"
	default:
		return "pending"
	}
}
