package kess

import "testing"

func TestValidateOrderRejectsMismatchedChannelFacts(t *testing.T) {
	intent := Intent{TradeNo: "DRL-01", ExpectedSellerCode: "seller", AmountE4: 123400, Currency: "USD"}
	valid := ChannelOrder{TradeNo: "DRL-01", SellerCode: "seller", AmountE4: 123400, Currency: "USD", Status: "WAITING", PaymentURL: "https://devwebpayment.kesspay.io/pay/checkout"}
	for name, mutate := range map[string]func(*ChannelOrder){
		"merchant": func(order *ChannelOrder) { order.SellerCode = "other" },
		"order":    func(order *ChannelOrder) { order.TradeNo = "DRL-other" },
		"amount":   func(order *ChannelOrder) { order.AmountE4 += 100 },
		"currency": func(order *ChannelOrder) { order.Currency = "KHR" },
	} {
		t.Run(name, func(t *testing.T) {
			order := valid
			mutate(&order)
			if _, err := validateOrder(intent, order, true); err != ErrInvalidChannel {
				t.Fatalf("validateOrder error = %v, want %v", err, ErrInvalidChannel)
			}
		})
	}
}

func TestValidateOrderRejectsInvalidSuccessAndNonSandboxLink(t *testing.T) {
	intent := Intent{TradeNo: "DRL-01", ExpectedSellerCode: "seller", AmountE4: 123400, Currency: "USD"}
	for _, order := range []ChannelOrder{
		{TradeNo: "DRL-01", SellerCode: "seller", AmountE4: 123400, Currency: "USD", Status: "SUCCESS"},
		{TradeNo: "DRL-01", SellerCode: "seller", AmountE4: 123400, Currency: "USD", Status: "WAITING", PaymentURL: "http://devwebpayment.kesspay.io/pay/x"},
		{TradeNo: "DRL-01", SellerCode: "seller", AmountE4: 123400, Currency: "USD", Status: "WAITING", PaymentURL: "https://devwebpayment.kesspay.io/not-pay/x"},
		{TradeNo: "DRL-01", SellerCode: "seller", AmountE4: 123400, Currency: "USD", Status: "WAITING", PaymentURL: "https://user:pass@devwebpayment.kesspay.io/pay/x"},
	} {
		if _, err := validateOrder(intent, order, true); err != ErrInvalidChannel {
			t.Fatalf("validateOrder(%+v) error = %v, want %v", order, err, ErrInvalidChannel)
		}
	}
}

func TestNestedDataMustBeValidatedIndependentlyOfTopLevelSignature(t *testing.T) {
	fields := map[string]string{"code": "0", "message": "ok"}
	fields["sign"] = SignScalars(fields, "test-key")
	if !VerifyScalars(fields, "test-key") {
		t.Fatal("test setup: top-level signature should verify")
	}
	intent := Intent{TradeNo: "DRL-01", ExpectedSellerCode: "seller", AmountE4: 123400, Currency: "USD"}
	tamperedNestedData := ChannelOrder{TradeNo: "DRL-01", SellerCode: "seller", AmountE4: 999900, Currency: "USD", Status: "SUCCESS", TransactionID: "channel-1"}
	if _, err := validateOrder(intent, tamperedNestedData, false); err != ErrInvalidChannel {
		t.Fatalf("nested data accepted from top-level signature: %v", err)
	}
}

func TestAmountPrecisionDoesNotExchangeCurrencies(t *testing.T) {
	for _, tc := range []struct {
		currency string
		amount   int64
		want     string
	}{
		{"USD", 100, "0.01"}, {"USD", 123400, "12.34"},
	} {
		got, err := FormatAmount(tc.amount, tc.currency)
		if err != nil || got != tc.want {
			t.Fatalf("FormatAmount(%d, %s) = %q, %v; want %q", tc.amount, tc.currency, got, err, tc.want)
		}
		back, err := ParseAmount(got, tc.currency)
		if err != nil || back != tc.amount {
			t.Fatalf("ParseAmount(%q, %s) = %d, %v; want %d", got, tc.currency, back, err, tc.amount)
		}
	}
	for _, tc := range []struct{ raw, currency string }{{"1.2", "USD"}, {"1", "USD"}, {"1.00", "KHR"}, {"0.01", "KHR"}, {"0.001", "USD"}} {
		if _, err := ParseAmount(tc.raw, tc.currency); err != ErrInvalidAmount {
			t.Fatalf("ParseAmount(%q, %s) error = %v, want %v", tc.raw, tc.currency, err, ErrInvalidAmount)
		}
	}
}
