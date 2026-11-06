package trade

import (
	"errors"
	"testing"
)

func TestSplitLinesSnapshotsTierPriceAndTotals(t *testing.T) {
	items, total, err := BuildOrderItems([]OrderLineInput{
		{SKUPublicID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", MerchantID: 10, Quantity: 12, Tiers: []Tier{{MinQuantity: 1, UnitPriceE4: 120}, {MinQuantity: 10, UnitPriceE4: 100}}},
		{SKUPublicID: "01ARZ3NDEKTSV4RRFFQ69G5FAX", MerchantID: 10, Quantity: 2, Tiers: []Tier{{MinQuantity: 1, UnitPriceE4: 50}}},
		{SKUPublicID: "01ARZ3NDEKTSV4RRFFQ69G5FAY", MerchantID: 20, Quantity: 3, Tiers: []Tier{{MinQuantity: 1, UnitPriceE4: 70}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1510 {
		t.Fatalf("total=%d, want 1510", total)
	}
	if len(items) != 3 || items[0].UnitPriceE4 != 100 || items[0].LineTotalE4 != 1200 {
		t.Fatalf("items=%+v", items)
	}
}

func TestAdvanceOnlyAllowsContractTransitionsAndRequiresSource(t *testing.T) {
	if _, err := Advance(AwaitingPayment, Paid, EventSource{}); !errors.Is(err, ErrEventSourceRequired) {
		t.Fatalf("missing source error=%v", err)
	}
	statuses := []OrderStatus{AwaitingPayment, Paid, Submitted, Accepted, InFulfillment, Shipped, DeliveredVerified, Settled, Rejected, Cancelled, DeliveryFailed, Returning, Returned, Exception}
	allowed := map[[2]OrderStatus]bool{
		{AwaitingPayment, Paid}: true, {AwaitingPayment, Cancelled}: true,
		{Paid, Submitted}: true, {Submitted, Accepted}: true, {Submitted, Rejected}: true,
		{Accepted, InFulfillment}: true, {InFulfillment, Shipped}: true,
		{Shipped, DeliveredVerified}: true, {DeliveredVerified, Settled}: true,
	}
	for _, from := range statuses {
		for _, to := range statuses {
			got, err := Advance(from, to, EventSource{Type: SourceSystem, Reference: "job-1"})
			if allowed[[2]OrderStatus{from, to}] {
				if err != nil || got != to {
					t.Fatalf("Advance(%q,%q)=%q,%v", from, to, got, err)
				}
			} else if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("Advance(%q,%q) error=%v, want ErrInvalidTransition", from, to, err)
			}
		}
	}
}
