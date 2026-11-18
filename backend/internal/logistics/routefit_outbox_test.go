package logistics

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/logistics/routefit"
)

func TestClassifyDeliveryError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want DeliveryAction
	}{
		{"success", nil, DeliveryDelivered},
		{"timeout", context.DeadlineExceeded, DeliveryRetry},
		{"server", &routefit.Error{Status: http.StatusServiceUnavailable, Code: "TEMPORARY"}, DeliveryRetry},
		{"outbound hold", &routefit.Error{Status: http.StatusLocked, Code: "OWNER_OUTBOUND_HELD"}, DeliveryException},
		{"stock", &routefit.Error{Status: http.StatusConflict, Code: "INSUFFICIENT_STOCK"}, DeliveryException},
		{"invalid", errors.New("bad command"), DeliveryException},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyDeliveryError(tc.err); got != tc.want {
				t.Fatalf("ClassifyDeliveryError()=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestFulfillmentIdempotencyKeyIsStableAndOpaque(t *testing.T) {
	first := fulfillmentIdempotencyKey("DRL-order-1")
	if first != fulfillmentIdempotencyKey("DRL-order-1") || len(first) != 64 {
		t.Fatalf("key=%q is not stable", first)
	}
	if first == fulfillmentIdempotencyKey("DRL-order-2") {
		t.Fatal("different orders share an idempotency key")
	}
}

func TestDeliveryRetryUsesExplicitAction(t *testing.T) {
	if got := ClassifyDeliveryError(&routefit.Error{Status: http.StatusBadGateway, Code: "UPSTREAM"}); got != DeliveryRetry {
		t.Fatalf("5xx action=%q, want retry", got)
	}
}
