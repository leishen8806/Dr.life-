package kess

import (
	"errors"
	"testing"
)

func TestAmountUsesE4ButKESSReceivesWholeUSDCents(t *testing.T) {
	formatted, err := FormatAmount(123400, "USD")
	if err != nil || formatted != "12.34" {
		t.Fatalf("FormatAmount = %q, %v; want 12.34", formatted, err)
	}
	parsed, err := ParseAmount("12.34", "USD")
	if err != nil || parsed != 123400 {
		t.Fatalf("ParseAmount = %d, %v; want 123400", parsed, err)
	}
}

func TestAmountRejectsSubCentKHRAndOverflow(t *testing.T) {
	for _, test := range []struct {
		name     string
		amount   int64
		currency string
	}{
		{name: "sub-cent", amount: 123401, currency: "USD"},
		{name: "zero", amount: 0, currency: "USD"},
		{name: "khr", amount: 40000000, currency: "KHR"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := FormatAmount(test.amount, test.currency); !errors.Is(err, ErrInvalidAmount) {
				t.Fatalf("FormatAmount error = %v, want ErrInvalidAmount", err)
			}
		})
	}
	for _, raw := range []string{"0.00", "12.345", "922337203685477.59"} {
		if _, err := ParseAmount(raw, "USD"); !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("ParseAmount(%q) error = %v, want ErrInvalidAmount", raw, err)
		}
	}
}
