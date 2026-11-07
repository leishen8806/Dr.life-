package trade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
)

var (
	ErrBuyerInactive            = errors.New("trade: buyer account is not active")
	ErrInsufficientAvailability = errors.New("trade: insufficient availability")
	ErrIdempotencyRequired      = errors.New("trade: idempotency key is required")
	ErrIdempotencyConflict      = errors.New("trade: idempotency key conflicts with another request")
	// ErrOrderNotVisible intentionally covers both a missing order and a
	// foreign order, so callers cannot use reads to enumerate order IDs.
	ErrOrderNotVisible = errors.New("trade: order is not visible to caller")
)

type AccountDirectory interface {
	Status(context.Context, int64) (string, error)
}
type Catalog interface {
	// ResolveAndReserve must resolve current server-side facts and reserve stock
	// atomically. reservationKey makes retries idempotent; abandoned keys must be
	// reclaimed by the catalog timeout worker. Reusing a key with different
	// lines must fail without changing the existing reservation.
	ResolveAndReserve(context.Context, string, []OrderLineInput) ([]CatalogLine, error)
	// Release is idempotent for one reservation key.
	Release(context.Context, string) error
	// Commit makes a paid reservation ineligible for timeout reclamation.
	// Catalog must not reclaim on its own: timeout workers call
	// Service.ReleaseReservations so trade locks and cancels the order first.
	// Commit is idempotent and returns ErrReservationReleased when stock was
	// already reclaimed, which is the caller's signal to start a refund.
	Commit(context.Context, string) error
}
type Store interface {
	FindByIdempotency(context.Context, int64, string, string) (CreateOrder, bool, error)
	Create(context.Context, string, string, CreateOrder) (CreateOrder, error)
	QueueReservationRelease(context.Context, string, string) error
	ClaimReservationRelease(context.Context, string) (Reservation, bool, error)
	ClaimReservationActions(context.Context, int) ([]ReservationAction, error)
	CompleteReservationAction(context.Context, int64) error
	FailReservationAction(context.Context, int64) error
	CompleteReservationCommit(context.Context, string) error
	ConfirmPayment(context.Context, string, int64, EventSource) (string, bool, error)
}

// OrderReader is deliberately scoped by the caller's account/merchant ID.
// Its implementations must apply this predicate in SQL, before returning any
// order data.
type OrderReader interface {
	FindBuyerOrder(context.Context, int64, string) (BuyerOrderView, error)
	FindMerchantOrder(context.Context, int64, string) (MerchantOrderView, error)
}
type BuyerOrderView struct {
	PublicID, Status string
	BuyerAccountID   int64
	TotalE4          int64
}
type MerchantOrderView struct {
	PublicID, Status string
	MerchantID       int64
	TotalE4          int64
}
type ChildOrder struct {
	MerchantID, TotalE4 int64
	Lines               []OrderItem
}
type CreateOrder struct {
	BuyerAccountID, TotalE4 int64
	PublicID                string
	IdempotencyKey          string
	RequestHash             string
	Children                []ChildOrder
}
type Reservation struct {
	Key      string
	Status   OrderStatus
	Released bool
}
type ReservationAction struct {
	ID     int64
	Key    string
	Action string
}
type Service struct {
	store    Store
	accounts AccountDirectory
	catalog  Catalog
}

func NewService(store Store, accounts AccountDirectory, catalog Catalog) *Service {
	return &Service{store, accounts, catalog}
}

// BuyerOrder returns a parent order only when it belongs to buyerID.
func (s *Service) BuyerOrder(ctx context.Context, buyerID int64, publicID string) (BuyerOrderView, error) {
	reader, ok := s.store.(OrderReader)
	if !ok || buyerID <= 0 || publicID == "" {
		return BuyerOrderView{}, ErrOrderNotVisible
	}
	return reader.FindBuyerOrder(ctx, buyerID, publicID)
}

// MerchantOrder returns a suborder only when it belongs to merchantID.
func (s *Service) MerchantOrder(ctx context.Context, merchantID int64, publicID string) (MerchantOrderView, error) {
	reader, ok := s.store.(OrderReader)
	if !ok || merchantID <= 0 || publicID == "" {
		return MerchantOrderView{}, ErrOrderNotVisible
	}
	return reader.FindMerchantOrder(ctx, merchantID, publicID)
}
func (s *Service) Place(ctx context.Context, buyer int64, idempotencyKey string, lines []OrderLineInput) (CreateOrder, error) {
	if idempotencyKey == "" {
		return CreateOrder{}, ErrIdempotencyRequired
	}
	status, err := s.accounts.Status(ctx, buyer)
	if err != nil {
		return CreateOrder{}, err
	}
	if status != "active" {
		return CreateOrder{}, ErrBuyerInactive
	}
	normalized, err := normalizeOrderLines(lines)
	if err != nil {
		return CreateOrder{}, err
	}
	hash := requestHash(buyer, normalized)
	if existing, found, err := s.store.FindByIdempotency(ctx, buyer, idempotencyKey, hash); err != nil || found {
		return existing, err
	}
	reservationKey := fmt.Sprintf("%d:%s", buyer, idempotencyKey)
	snapshots, err := s.catalog.ResolveAndReserve(ctx, reservationKey, normalized)
	if err != nil {
		return CreateOrder{}, err
	}
	if err := validateCatalogSnapshot(normalized, snapshots); err != nil {
		return CreateOrder{}, errors.Join(err, s.releaseOrQueue(ctx, reservationKey, "invalid_snapshot"))
	}
	items, total, err := BuildOrderItems(snapshots)
	if err != nil {
		return CreateOrder{}, errors.Join(err, s.releaseOrQueue(ctx, reservationKey, "invalid_order"))
	}
	children := map[int64]int{}
	order := CreateOrder{BuyerAccountID: buyer, TotalE4: total}
	for _, item := range items {
		index, ok := children[item.MerchantID]
		if !ok {
			index = len(order.Children)
			children[item.MerchantID] = index
			order.Children = append(order.Children, ChildOrder{MerchantID: item.MerchantID})
		}
		child := &order.Children[index]
		child.Lines = append(child.Lines, item)
		child.TotalE4 += item.LineTotalE4
	}
	order.IdempotencyKey = idempotencyKey
	order.RequestHash = hash
	stored, err := s.store.Create(ctx, idempotencyKey, order.RequestHash, order)
	if err != nil {
		if errors.Is(err, ErrIdempotencyConflict) {
			return CreateOrder{}, err
		}
		return CreateOrder{}, errors.Join(err, s.releaseOrQueue(ctx, reservationKey, "create_failed"))
	}
	return stored, nil
}

func validateCatalogSnapshot(requested []OrderLineInput, snapshots []CatalogLine) error {
	if len(requested) != len(snapshots) {
		return ErrInvalidOrderLine
	}
	want := make(map[string]int64, len(requested))
	for _, line := range requested {
		want[line.SKUPublicID] = line.Quantity
	}
	for _, snapshot := range snapshots {
		if want[snapshot.SKUPublicID] != snapshot.Quantity {
			return ErrInvalidOrderLine
		}
		delete(want, snapshot.SKUPublicID)
	}
	if len(want) != 0 {
		return ErrInvalidOrderLine
	}
	return nil
}

func (s *Service) ReleaseReservations(ctx context.Context, buyerOrderPublicID string) error {
	reservation, claimed, err := s.store.ClaimReservationRelease(ctx, buyerOrderPublicID)
	if err != nil || !claimed {
		return err
	}
	if err := s.catalog.Release(ctx, reservation.Key); err != nil {
		return errors.Join(err, s.store.QueueReservationRelease(ctx, reservation.Key, "release_failed"))
	}
	return nil
}

// ConfirmPayment atomically pays every child order, then commits the catalog
// reservation. A replay retries Commit without paying twice.
func (s *Service) ConfirmPayment(ctx context.Context, buyerOrderPublicID string, paidAmountE4 int64, source EventSource) (bool, error) {
	key, applied, err := s.store.ConfirmPayment(ctx, buyerOrderPublicID, paidAmountE4, source)
	if err != nil {
		return false, err
	}
	if err := s.catalog.Commit(ctx, key); err != nil {
		return applied, err
	}
	return applied, s.store.CompleteReservationCommit(ctx, key)
}

// ProcessReservationActions drains durable release and commit compensation.
func (s *Service) ProcessReservationActions(ctx context.Context, limit int) error {
	items, err := s.store.ClaimReservationActions(ctx, limit)
	if err != nil {
		return err
	}
	var result error
	for _, item := range items {
		var actionErr error
		if item.Action == "commit" {
			actionErr = s.catalog.Commit(ctx, item.Key)
		} else {
			actionErr = s.catalog.Release(ctx, item.Key)
		}
		if actionErr != nil {
			result = errors.Join(result, actionErr, s.store.FailReservationAction(ctx, item.ID))
			continue
		}
		result = errors.Join(result, s.store.CompleteReservationAction(ctx, item.ID))
	}
	return result
}

func (s *Service) releaseOrQueue(ctx context.Context, key, reason string) error {
	if err := s.catalog.Release(ctx, key); err != nil {
		return errors.Join(err, s.store.QueueReservationRelease(ctx, key, reason))
	}
	return nil
}

func normalizeOrderLines(lines []OrderLineInput) ([]OrderLineInput, error) {
	quantities := make(map[string]int64, len(lines))
	for _, line := range lines {
		if line.SKUPublicID == "" || line.Quantity <= 0 || quantities[line.SKUPublicID] > maxInt64-line.Quantity {
			return nil, ErrInvalidOrderLine
		}
		quantities[line.SKUPublicID] += line.Quantity
	}
	if len(quantities) == 0 {
		return nil, ErrInvalidOrderLine
	}
	normalized := make([]OrderLineInput, 0, len(quantities))
	for sku, quantity := range quantities {
		normalized = append(normalized, OrderLineInput{SKUPublicID: sku, Quantity: quantity})
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].SKUPublicID < normalized[j].SKUPublicID })
	return normalized, nil
}

func requestHash(buyer int64, lines []OrderLineInput) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(strconv.FormatInt(buyer, 10)))
	for _, line := range lines {
		_, _ = hash.Write([]byte("\x00" + line.SKUPublicID + "\x00" + strconv.FormatInt(line.Quantity, 10)))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
