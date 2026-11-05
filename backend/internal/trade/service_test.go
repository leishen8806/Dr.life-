package trade

import (
	"context"
	"errors"
	"testing"
)

type stubAccounts struct{ status string }

func (s stubAccounts) Status(context.Context, int64) (string, error) { return s.status, nil }

type stubAvailability struct {
	available map[string]int64
	reserved  map[string]int64
}

func (s *stubAvailability) Available(_ context.Context, sku string) (int64, error) {
	return s.available[sku], nil
}
func (s *stubAvailability) Reserve(_ context.Context, sku string, n int64) error {
	if s.available[sku]-s.reserved[sku] < n {
		return ErrInsufficientAvailability
	}
	s.reserved[sku] += n
	return nil
}
func (s *stubAvailability) Release(_ context.Context, sku string, n int64) error {
	if s.reserved[sku] < n {
		return ErrInsufficientAvailability
	}
	s.reserved[sku] -= n
	return nil
}

type stubOrders struct{ created CreateOrder }

func (s *stubOrders) Create(_ context.Context, input CreateOrder) error {
	s.created = input
	return nil
}

func TestPlaceOrderSplitsByMerchantAndReserves(t *testing.T) {
	availability := &stubAvailability{available: map[string]int64{"A": 5, "B": 5, "C": 5}, reserved: map[string]int64{}}
	store := &stubOrders{}
	svc := NewService(store, stubAccounts{status: "active"}, availability)
	order, err := svc.Place(context.Background(), 1, []OrderLineInput{{SKUPublicID: "A", MerchantID: 10, Quantity: 2, Tiers: []Tier{{MinQuantity: 1, UnitPriceE4: 10}}}, {SKUPublicID: "B", MerchantID: 10, Quantity: 1, Tiers: []Tier{{MinQuantity: 1, UnitPriceE4: 20}}}, {SKUPublicID: "C", MerchantID: 20, Quantity: 1, Tiers: []Tier{{MinQuantity: 1, UnitPriceE4: 30}}}})
	if err != nil {
		t.Fatal(err)
	}
	if order.TotalE4 != 70 || len(order.Children) != 2 || availability.reserved["A"] != 2 {
		t.Fatalf("order=%+v reserved=%v", order, availability.reserved)
	}
}
func TestPlaceOrderRejectsFrozenAndInsufficientWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		status    string
		available int64
		want      error
	}{{"frozen", 2, ErrBuyerInactive}, {"active", 0, ErrInsufficientAvailability}} {
		t.Run(tc.status, func(t *testing.T) {
			store := &stubOrders{}
			svc := NewService(store, stubAccounts{tc.status}, &stubAvailability{available: map[string]int64{"A": tc.available}, reserved: map[string]int64{}})
			_, err := svc.Place(context.Background(), 1, []OrderLineInput{{SKUPublicID: "A", MerchantID: 1, Quantity: 1, Tiers: []Tier{{MinQuantity: 1, UnitPriceE4: 1}}}})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v", err)
			}
			if len(store.created.Children) != 0 {
				t.Fatal("wrote order")
			}
		})
	}
}

func TestReleaseReservationsReturnsEachLine(t *testing.T) {
	a := &stubAvailability{available: map[string]int64{}, reserved: map[string]int64{"A": 2, "B": 3}}
	if err := ReleaseReservations(context.Background(), a, []OrderItem{{SKUPublicID: "A", Quantity: 2}, {SKUPublicID: "B", Quantity: 3}}); err != nil {
		t.Fatal(err)
	}
	if a.reserved["A"] != 0 || a.reserved["B"] != 0 {
		t.Fatalf("reservations not released: %v", a.reserved)
	}
}
