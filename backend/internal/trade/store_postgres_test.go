package trade

import "testing"

func TestPostgresStoreImplementsStore(t *testing.T) {
	var _ Store = (*PostgresStore)(nil)
	var _ EventStore = (*PostgresStore)(nil)
}

func TestPaymentStatusProjectionCoversEveryIntentState(t *testing.T) {
	for input, want := range map[string]string{
		"PENDING": "pending", "WAITING": "pending", "UNKNOWN": "confirming", "REVIEW": "confirming",
		"SUCCESS": "succeeded", "FAILED": "failed", "CLOSED": "failed", "REFUND": "refunded", "": "pending",
	} {
		if got := projectPaymentStatus(input); got != want {
			t.Errorf("%s => %s, want %s", input, got, want)
		}
	}
}

func TestOrderPageLimitMatchesOpenAPI(t *testing.T) {
	if pageLimit(0) != 20 || pageLimit(101) != 100 || pageLimit(7) != 7 {
		t.Fatal("order page limit differs from OpenAPI default or bounds")
	}
}
