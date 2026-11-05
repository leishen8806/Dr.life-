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
	if _, err := Advance(AwaitingPayment, Submitted, EventSource{Type: SourceSystem, Reference: "job-1"}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("skipped state error=%v", err)
	}
	if got, err := Advance(AwaitingPayment, Paid, EventSource{Type: SourceKESSResult, Reference: "kess-1"}); err != nil || got != Paid {
		t.Fatalf("Advance()=%q,%v", got, err)
	}
}
