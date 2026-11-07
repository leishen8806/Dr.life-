// Package trade owns the order domain. It never queries catalog or iam tables;
// callers supply their verified snapshots through module adapters.
package trade

import "errors"

const maxInt64 = int64(^uint64(0) >> 1)

type OrderStatus string

const (
	AwaitingPayment   OrderStatus = "awaiting_payment"
	Paid              OrderStatus = "paid"
	Submitted         OrderStatus = "submitted"
	Accepted          OrderStatus = "accepted"
	InFulfillment     OrderStatus = "in_fulfillment"
	Shipped           OrderStatus = "shipped"
	DeliveredVerified OrderStatus = "delivered_verified"
	Settled           OrderStatus = "settled"
	Rejected          OrderStatus = "rejected"
	Cancelled         OrderStatus = "cancelled"
	DeliveryFailed    OrderStatus = "delivery_failed"
	Returning         OrderStatus = "returning"
	Returned          OrderStatus = "returned"
	Exception         OrderStatus = "exception"
)

type EventSourceType string

const (
	SourceKESSResult    EventSourceType = "kess_result"
	SourceRouteFitEvent EventSourceType = "routefit_event"
	SourceSystem        EventSourceType = "system"
)

var (
	ErrInvalidTransition     = errors.New("trade: invalid order transition")
	ErrEventSourceRequired   = errors.New("trade: event source is required")
	ErrInvalidOrderLine      = errors.New("trade: invalid order line")
	ErrNoMatchingTier        = errors.New("trade: no matching price tier")
	ErrPriceNotWholeCent     = errors.New("trade: selected price tier is not a whole cent")
	ErrReservationReleased   = errors.New("trade: order reservation was released")
	ErrPaymentAmount         = errors.New("trade: paid amount does not match order total")
	ErrPaymentReconciliation = errors.New("trade: paid order received another payment source")
)

type EventSource struct {
	Type      EventSourceType
	Reference string
}
type Tier struct {
	MinQuantity int64
	UnitPriceE4 int64
}
type OrderLineInput struct {
	SKUPublicID string
	Quantity    int64
}
type CatalogLine struct {
	SKUPublicID          string
	MerchantID, Quantity int64
	Tiers                []Tier
	OnSale               bool
}
type OrderItem struct {
	SKUPublicID                                    string
	MerchantID, Quantity, UnitPriceE4, LineTotalE4 int64
}

func BuildOrderItems(lines []CatalogLine) ([]OrderItem, int64, error) {
	if len(lines) == 0 {
		return nil, 0, ErrInvalidOrderLine
	}
	items := make([]OrderItem, 0, len(lines))
	var total int64
	for _, line := range lines {
		if line.SKUPublicID == "" || line.MerchantID <= 0 || line.Quantity <= 0 || !line.OnSale {
			return nil, 0, ErrInvalidOrderLine
		}
		price, err := tierPrice(line.Quantity, line.Tiers)
		if err != nil {
			return nil, 0, err
		}
		if price > 0 && line.Quantity > maxInt64/price {
			return nil, 0, ErrInvalidOrderLine
		}
		amount := line.Quantity * price
		if amount > 0 && total > maxInt64-amount {
			return nil, 0, ErrInvalidOrderLine
		}
		items = append(items, OrderItem{SKUPublicID: line.SKUPublicID, MerchantID: line.MerchantID, Quantity: line.Quantity, UnitPriceE4: price, LineTotalE4: amount})
		total += amount
	}
	return items, total, nil
}

func tierPrice(quantity int64, tiers []Tier) (int64, error) {
	var price, bestMinimum int64
	found := false
	seen := make(map[int64]struct{}, len(tiers))
	for _, tier := range tiers {
		if tier.MinQuantity <= 0 || tier.UnitPriceE4 < 0 {
			return 0, ErrNoMatchingTier
		}
		if _, duplicate := seen[tier.MinQuantity]; duplicate {
			return 0, ErrNoMatchingTier
		}
		seen[tier.MinQuantity] = struct{}{}
		if tier.MinQuantity <= quantity && (!found || tier.MinQuantity > bestMinimum) {
			price, bestMinimum, found = tier.UnitPriceE4, tier.MinQuantity, true
		}
	}
	if !found {
		return 0, ErrNoMatchingTier
	}
	if price%100 != 0 {
		return 0, ErrPriceNotWholeCent
	}
	return price, nil
}

func Advance(from, to OrderStatus, source EventSource) (OrderStatus, error) {
	if source.Reference == "" || (source.Type != SourceKESSResult && source.Type != SourceRouteFitEvent && source.Type != SourceSystem) {
		return "", ErrEventSourceRequired
	}
	allowed := (from == Paid && to == Submitted) || (from == Submitted && (to == Accepted || to == Rejected)) ||
		(from == Accepted && to == InFulfillment) || (from == InFulfillment && to == Shipped) ||
		(from == Shipped && to == DeliveredVerified) || (from == DeliveredVerified && to == Settled)
	if !allowed {
		return "", ErrInvalidTransition
	}
	return to, nil
}
