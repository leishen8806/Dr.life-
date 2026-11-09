package trade

import (
	"context"
	"errors"
	"time"
)

var (
	ErrOrderAdaptersUnavailable = errors.New("trade: order adapters are unavailable")
	ErrPaymentConfirming        = errors.New("trade: payment confirmation is in progress")
	ErrShippingQuoteExpired     = errors.New("trade: shipping quote expired")
	ErrAddressNotFound          = errors.New("trade: shipping address not found")
)

type AddressSnapshot struct {
	PublicID, RecipientName, AddressText, Phone string
}

type ShippingQuote struct {
	MerchantID int64
	AmountE4   int64
	ExpiresAt  time.Time
}

type PaymentResult struct {
	PaymentNo, PaymentLink string
	ExpiresAt              time.Time
}

type PaymentAttempt struct {
	AmountE4  int64
	AttemptNo int
	Existing  *PaymentResult
}

type AddressBook interface {
	Resolve(context.Context, int64, string) (AddressSnapshot, error)
}

// ShippingQuoter is the RouteFit-facing order quote port. Its implementation
// owns the external payload; trade consumes only the immutable quote result.
type ShippingQuoter interface {
	Quote(context.Context, AddressSnapshot, []ChildOrder) ([]ShippingQuote, error)
}

type PaymentCreator interface {
	Create(context.Context, string, int64, int) (PaymentResult, error)
}

type OrderHTTPStore interface {
	ListBuyerOrders(context.Context, int64, string, int) ([]BuyerOrderView, string, error)
	FindBuyerOrderDetail(context.Context, int64, string) (BuyerOrderView, error)
	ListMerchantOrders(context.Context, int64, string, int) ([]MerchantOrderView, string, error)
	FindMerchantOrderDetail(context.Context, int64, string) (MerchantOrderView, error)
	ClaimBuyerReservationRelease(context.Context, int64, string) (Reservation, bool, error)
	PaymentAttempt(context.Context, int64, string) (PaymentAttempt, error)
}

type OrderLineView struct {
	SKUPublicID                    string
	Quantity, UnitPriceE4, TotalE4 int64
}

type BuyerOrderView struct {
	PublicID, Status, PaymentStatus string
	BuyerAccountID                  int64
	TotalE4                         int64
	GoodsAmountE4                   int64
	ShippingAmountE4                int64
	PayableAmountE4                 int64
	ShippingAddress                 AddressSnapshot
	CreatedAt, UpdatedAt            time.Time
	Children                        []MerchantOrderView
}

type MerchantOrderView struct {
	PublicID, ParentPublicID, Status string
	MerchantPublicID                 string
	MerchantID                       int64
	TotalE4                          int64
	GoodsAmountE4                    int64
	ShippingAmountE4                 int64
	PayableAmountE4                  int64
	ShippingQuoteExpiresAt           time.Time
	ShippingAddress                  AddressSnapshot
	CreatedAt, UpdatedAt             time.Time
	Lines                            []OrderLineView
}

func (s *Service) ConfigureHTTP(addresses AddressBook, quotes ShippingQuoter, payments PaymentCreator) {
	s.addresses, s.quotes, s.payments = addresses, quotes, payments
}

func (s *Service) PlaceForDelivery(ctx context.Context, buyer int64, idempotencyKey, addressPublicID string, lines []OrderLineInput) (CreateOrder, error) {
	if s.addresses == nil || s.quotes == nil {
		return CreateOrder{}, ErrOrderAdaptersUnavailable
	}
	address, err := s.addresses.Resolve(ctx, buyer, addressPublicID)
	if err != nil {
		return CreateOrder{}, err
	}
	if address.PublicID == "" || address.PublicID != addressPublicID {
		return CreateOrder{}, ErrAddressNotFound
	}
	if address.RecipientName == "" || address.AddressText == "" || address.Phone == "" {
		return CreateOrder{}, ErrInvalidOrderLine
	}
	return s.place(ctx, buyer, idempotencyKey, address, lines)
}

func (s *Service) BuyerOrders(ctx context.Context, buyer int64, cursor string, limit int) ([]BuyerOrderView, string, error) {
	store, ok := s.store.(OrderHTTPStore)
	if !ok {
		return nil, "", ErrOrderAdaptersUnavailable
	}
	return store.ListBuyerOrders(ctx, buyer, cursor, limit)
}

func (s *Service) BuyerOrderDetail(ctx context.Context, buyer int64, publicID string) (BuyerOrderView, error) {
	store, ok := s.store.(OrderHTTPStore)
	if !ok {
		return BuyerOrderView{}, ErrOrderAdaptersUnavailable
	}
	return store.FindBuyerOrderDetail(ctx, buyer, publicID)
}

func (s *Service) MerchantOrders(ctx context.Context, merchant int64, cursor string, limit int) ([]MerchantOrderView, string, error) {
	store, ok := s.store.(OrderHTTPStore)
	if !ok {
		return nil, "", ErrOrderAdaptersUnavailable
	}
	return store.ListMerchantOrders(ctx, merchant, cursor, limit)
}

func (s *Service) MerchantOrderDetail(ctx context.Context, merchant int64, publicID string) (MerchantOrderView, error) {
	store, ok := s.store.(OrderHTTPStore)
	if !ok {
		return MerchantOrderView{}, ErrOrderAdaptersUnavailable
	}
	return store.FindMerchantOrderDetail(ctx, merchant, publicID)
}

func (s *Service) Cancel(ctx context.Context, buyer int64, publicID string) error {
	store, ok := s.store.(OrderHTTPStore)
	if !ok {
		return ErrOrderAdaptersUnavailable
	}
	reservation, claimed, err := store.ClaimBuyerReservationRelease(ctx, buyer, publicID)
	if err != nil || !claimed {
		return err
	}
	if err := s.catalog.Release(ctx, reservation.Key); err != nil {
		return errors.Join(err, s.store.QueueReservationRelease(ctx, reservation.Key, "release_failed"))
	}
	return s.store.CompleteReservationRelease(ctx, reservation.Key)
}

func (s *Service) CreatePayment(ctx context.Context, buyer int64, publicID string) (PaymentResult, error) {
	store, ok := s.store.(OrderHTTPStore)
	if !ok || s.payments == nil {
		return PaymentResult{}, ErrOrderAdaptersUnavailable
	}
	attempt, err := store.PaymentAttempt(ctx, buyer, publicID)
	if err != nil {
		return PaymentResult{}, err
	}
	if attempt.Existing != nil {
		return *attempt.Existing, nil
	}
	return s.payments.Create(ctx, publicID, attempt.AmountE4, attempt.AttemptNo)
}
