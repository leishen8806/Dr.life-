package trade

import (
	"context"
	"errors"
)

var (
	ErrBuyerInactive            = errors.New("trade: buyer account is not active")
	ErrInsufficientAvailability = errors.New("trade: insufficient availability")
)

type AccountDirectory interface {
	Status(context.Context, int64) (string, error)
}
type Availability interface {
	Available(context.Context, string) (int64, error)
	Reserve(context.Context, string, int64) error
	Release(context.Context, string, int64) error
}
type Store interface {
	Create(context.Context, CreateOrder) error
}
type ChildOrder struct {
	MerchantID, TotalE4 int64
	Lines               []OrderItem
}
type CreateOrder struct {
	BuyerAccountID, TotalE4 int64
	Children                []ChildOrder
}
type Service struct {
	store        Store
	accounts     AccountDirectory
	availability Availability
}

func NewService(store Store, accounts AccountDirectory, availability Availability) *Service {
	return &Service{store, accounts, availability}
}
func (s *Service) Place(ctx context.Context, buyer int64, lines []OrderLineInput) (CreateOrder, error) {
	status, err := s.accounts.Status(ctx, buyer)
	if err != nil {
		return CreateOrder{}, err
	}
	if status != "active" {
		return CreateOrder{}, ErrBuyerInactive
	}
	items, total, err := BuildOrderItems(lines)
	if err != nil {
		return CreateOrder{}, err
	}
	reserved := make([]OrderItem, 0, len(items))
	for _, item := range items {
		available, e := s.availability.Available(ctx, item.SKUPublicID)
		if e != nil {
			err = e
			break
		}
		if available < item.Quantity {
			err = ErrInsufficientAvailability
			break
		}
		if e = s.availability.Reserve(ctx, item.SKUPublicID, item.Quantity); e != nil {
			err = e
			break
		}
		reserved = append(reserved, item)
	}
	if err != nil {
		for _, item := range reserved {
			_ = s.availability.Release(ctx, item.SKUPublicID, item.Quantity)
		}
		return CreateOrder{}, err
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
	if err = s.store.Create(ctx, order); err != nil {
		for _, item := range reserved {
			_ = s.availability.Release(ctx, item.SKUPublicID, item.Quantity)
		}
		return CreateOrder{}, err
	}
	return order, nil
}

// ReleaseReservations releases local stock reservations when an order is
// cancelled or expires before payment. It consumes the order-line snapshot and
// therefore never needs current catalog data.
func ReleaseReservations(ctx context.Context, availability Availability, items []OrderItem) error {
	for _, item := range items {
		if item.SKUPublicID == "" || item.Quantity <= 0 {
			return ErrInvalidOrderLine
		}
		if err := availability.Release(ctx, item.SKUPublicID, item.Quantity); err != nil {
			return err
		}
	}
	return nil
}
