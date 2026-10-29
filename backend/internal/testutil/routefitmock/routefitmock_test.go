package routefitmock

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newMock(t *testing.T) *Mock {
	t.Helper()
	m := New(Config{CallbackSecret: "test-secret", QuoteTTL: time.Hour})
	m.AddOwner(Owner{OwnerRef: "merchant-1", CustomerID: "owner-1", WarehouseIDs: []string{"wh-1"}})
	m.AddSKU("merchant-1", SKU{ID: "sku-1", MerchantSKUCode: "sku-code", Name: "Widget", Unit: "each", State: "active"}, "wh-1", 10)
	return m
}

func request(t *testing.T, h http.Handler, method, path, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	r := httptest.NewRequest(method, path, reader)
	if key != "" {
		r.Header.Set("Idempotency-Key", "0123456789abcdef-"+key)
	}
	r.Header.Set("If-Match", "1")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func fulfillmentBody() map[string]any {
	return map[string]any{"ownerRef": "merchant-1", "externalOrderId": "DRL-order-1", "externalRevision": 1, "warehouseId": "wh-1", "lines": []any{map[string]any{"lineRef": "line-1", "skuId": "sku-1", "quantity": 2}}, "delivery": map[string]any{"address": "Street 1", "contactName": "Buyer", "phone": "+85512345678", "landmark": "near market"}, "serviceLevel": "S1", "paymentTerms": "PLATFORM_PREPAID", "declaredValue": map[string]any{"amountMinor": 1234, "currency": "USD"}, "sourcePlatform": "drlife"}
}

func TestHTTPReadEndpointsAndQuote(t *testing.T) {
	m := newMock(t)
	h := m.Handler()
	for _, path := range []string{"/api/platform/v1/owners/merchant-1", "/api/platform/v1/owners/merchant-1/skus", "/api/platform/v1/owners/merchant-1/inventory?skuIds=sku-1"} {
		if got := request(t, h, http.MethodGet, path, "", nil).Code; got != http.StatusOK {
			t.Fatalf("%s: got %d", path, got)
		}
	}
	w := request(t, h, http.MethodPost, "/api/platform/v1/quotes", "", map[string]any{"ownerRef": "merchant-1", "warehouseId": "wh-1", "lines": []any{map[string]any{"skuId": "sku-1", "quantity": 2}}, "delivery": map[string]any{"address": "Street 1", "contactName": "Buyer", "phone": "+85512345678"}, "serviceLevel": "S1"})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "quoteId") {
		t.Fatalf("quote: %d %s", w.Code, w.Body.String())
	}
}

func TestFulfillmentIdempotencyStockRevisionAndHold(t *testing.T) {
	m := newMock(t)
	h := m.Handler()
	body := fulfillmentBody()
	first := request(t, h, http.MethodPost, "/api/platform/v1/fulfillments", "idem-1", body)
	if first.Code != http.StatusCreated || !strings.Contains(first.Body.String(), `"duplicate":false`) {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	duplicate := request(t, h, http.MethodPost, "/api/platform/v1/fulfillments", "idem-1", body)
	if duplicate.Code != http.StatusCreated || !strings.Contains(duplicate.Body.String(), `"duplicate":true`) {
		t.Fatalf("duplicate: %d %s", duplicate.Code, duplicate.Body.String())
	}
	if w := request(t, h, http.MethodGet, "/api/platform/v1/fulfillments?externalOrderId=DRL-order-1", "", nil); w.Code != http.StatusOK {
		t.Fatalf("lookup: %d %s", w.Code, w.Body.String())
	}
	body["externalOrderId"] = "DRL-order-too-many"
	body["lines"] = []any{map[string]any{"lineRef": "line-1", "skuId": "sku-1", "quantity": 99}}
	if w := request(t, h, http.MethodPost, "/api/platform/v1/fulfillments", "idem-2", body); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "INSUFFICIENT_STOCK") {
		t.Fatalf("stock: %d %s", w.Code, w.Body.String())
	}
	hold := request(t, h, http.MethodPut, "/api/platform/v1/owners/merchant-1/outbound-hold", "", map[string]any{"state": "held", "reasonCode": "ARREARS", "note": "test"})
	if hold.Code != http.StatusOK {
		t.Fatalf("hold: %d", hold.Code)
	}
	body["externalOrderId"] = "DRL-order-held"
	body["lines"] = []any{map[string]any{"lineRef": "line-1", "skuId": "sku-1", "quantity": 1}}
	if w := request(t, h, http.MethodPost, "/api/platform/v1/fulfillments", "idem-3", body); w.Code != http.StatusLocked || !strings.Contains(w.Body.String(), "OWNER_OUTBOUND_HELD") {
		t.Fatalf("held: %d %s", w.Code, w.Body.String())
	}
}

func TestCancelEvidenceAndStatement(t *testing.T) {
	m := newMock(t)
	h := m.Handler()
	w := request(t, h, http.MethodPost, "/api/platform/v1/fulfillments", "idem-1", fulfillmentBody())
	var created struct {
		FulfillmentID string `json:"fulfillmentId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if w := request(t, h, http.MethodPost, "/api/platform/v1/fulfillments/"+created.FulfillmentID+"/actions/cancel", "cancel-1", map[string]any{"reasonCode": "BUYER_CANCELLED", "note": "test"}); w.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
	}
	m.AddEvidence(created.FulfillmentID, Evidence{ID: "e-1", Kind: "PHOTO", SHA256: "abc", CapturedAt: time.Now().UTC()})
	if w := request(t, h, http.MethodGet, "/api/platform/v1/fulfillments/"+created.FulfillmentID+"/evidence/e-1/download-url", "", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "expiresAt") {
		t.Fatalf("evidence: %d %s", w.Code, w.Body.String())
	}
	m.AddFeeLine(FeeLine{ID: "fee-1", Version: 1, OwnerRef: "merchant-1", FeeCode: "base", Direction: "CHARGE", Amount: Money{AmountMinor: 125, Currency: "USD"}, ServiceDate: "2026-10-28"})
	if w := request(t, h, http.MethodGet, "/api/platform/v1/statements/2026-10-28", "", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "statementVersion") {
		t.Fatalf("statement: %d %s", w.Code, w.Body.String())
	}
}

func TestEventDeliveryAndFaultInjection(t *testing.T) {
	m := newMock(t)
	var calls int
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-RouteFit-Signature") == "" {
			t.Error("missing signature")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer callback.Close()
	e := Event{ID: "evt-1", Type: "fulfillment.accepted", Aggregate: Aggregate{Type: "fulfillment", ID: "f-1", Version: 1}, OccurredAt: time.Now().UTC(), Payload: json.RawMessage(`{"fulfillmentId":"f-1"}`)}
	if err := m.Deliver(context.Background(), callback.URL, e, DeliveryOptions{Duplicate: true}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("X-RouteFit-Signature"), "0000") {
			t.Error("expected bad signature")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer bad.Close()
	if err := m.Deliver(context.Background(), bad.URL, e, DeliveryOptions{InvalidSignature: true, ExpiredTimestamp: true, Delay: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	m.SetFault(Fault{Status: http.StatusServiceUnavailable, Count: 1})
	if w := request(t, m.Handler(), http.MethodGet, "/api/platform/v1/owners/merchant-1", "", nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("fault: %d", w.Code)
	}
}

func TestEventOrderingRetryTimeoutAndScenarioAPI(t *testing.T) {
	m := newMock(t)
	created := request(t, m.Handler(), http.MethodPost, "/api/platform/v1/fulfillments", "idem-1", fulfillmentBody())
	var result struct {
		FulfillmentID string `json:"fulfillmentId"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	accepted, err := m.AdvanceFulfillment(result.FulfillmentID, "accepted")
	if err != nil {
		t.Fatal(err)
	}
	shipped, err := m.AdvanceFulfillment(result.FulfillmentID, "shipped")
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	var attempts int
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var event Event
		_ = json.NewDecoder(r.Body).Decode(&event)
		order = append(order, event.ID)
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer callback.Close()
	if err := m.DeliverMany(context.Background(), callback.URL, []Event{accepted, shipped}, DeliveryOptions{Reverse: true, Retry: true}); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || len(order) != 3 || order[0] != shipped.ID || order[1] != shipped.ID || order[2] != accepted.ID {
		t.Fatalf("attempts=%d order=%v", attempts, order)
	}
	if err := m.Deliver(context.Background(), callback.URL, accepted, DeliveryOptions{Delay: 30 * time.Millisecond, Timeout: time.Millisecond}); err == nil {
		t.Fatal("timeout injection unexpectedly succeeded")
	}
}

// These cases intentionally pin the RouteFit shared-contract details which are
// easy for an in-memory substitute to accidentally simplify away.
func TestRouteFitErrorETagAndIdempotencyContract(t *testing.T) {
	m := newMock(t)
	h := m.Handler()
	missing := request(t, h, http.MethodPost, "/api/platform/v1/fulfillments", "", fulfillmentBody())
	if missing.Code != http.StatusPreconditionRequired || !strings.Contains(missing.Body.String(), `"error"`) || !strings.Contains(missing.Body.String(), "IDEMPOTENCY_KEY_REQUIRED") {
		t.Fatalf("missing idempotency: %d %s", missing.Code, missing.Body.String())
	}
	first := request(t, h, http.MethodPost, "/api/platform/v1/fulfillments", "same", fulfillmentBody())
	if first.Code != http.StatusCreated {
		t.Fatal(first.Body.String())
	}
	replay := request(t, h, http.MethodPost, "/api/platform/v1/fulfillments", "same", fulfillmentBody())
	if replay.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("missing replay header: %v", replay.Header())
	}
	changed := fulfillmentBody()
	changed["externalOrderId"] = "DRL-other"
	if w := request(t, h, http.MethodPost, "/api/platform/v1/fulfillments", "same", changed); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "IDEMPOTENCY_KEY_REUSED") {
		t.Fatalf("reused key: %d %s", w.Code, w.Body.String())
	}
	noETag := httptest.NewRequest(http.MethodPut, "/api/platform/v1/owners/merchant-1/outbound-hold", strings.NewReader(`{"state":"held"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, noETag)
	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing etag: %d %s", w.Code, w.Body.String())
	}
	good := request(t, h, http.MethodPut, "/api/platform/v1/owners/merchant-1/outbound-hold", "", map[string]any{"state": "held"})
	if good.Code != http.StatusOK || good.Header().Get("ETag") != `"2"` {
		t.Fatalf("etag response: %d %v", good.Code, good.Header())
	}
}

func TestRouteFitEventsForCancelInventoryAndFees(t *testing.T) {
	m := newMock(t)
	w := request(t, m.Handler(), http.MethodPost, "/api/platform/v1/fulfillments", "event", fulfillmentBody())
	var created struct{ FulfillmentID string `json:"fulfillmentId"` }
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil { t.Fatal(err) }
	_ = request(t, m.Handler(), http.MethodPost, "/api/platform/v1/fulfillments/"+created.FulfillmentID+"/actions/cancel", "event-cancel", map[string]any{})
	m.AddFeeLine(FeeLine{ID:"fee-1", OwnerRef:"merchant-1", FeeCode:"base", Amount:Money{AmountMinor:1,Currency:"USD"}})
	if err := m.AdjustFeeLine("fee-1", Money{AmountMinor:2,Currency:"USD"}); err != nil { t.Fatal(err) }
	got := map[string]bool{}
	for _, e := range m.Events() { got[e.Type] = true }
	for _, want := range []string{"fulfillment.cancelled", "inventory.changed", "fee.posted", "fee.adjusted"} {
		if !got[want] { t.Errorf("missing event %s: %#v", want, got) }
	}
}
