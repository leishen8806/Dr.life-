package routefit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/logistics/routefit"
	"github.com/leishen8806/Dr.life-/backend/internal/testutil/routefitmock"
)

func TestDeliverCreatesFulfillmentWithStableIdempotencyKey(t *testing.T) {
	mock := seededMock()
	var mu sync.Mutex
	keys := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		mu.Unlock()
		if got := r.Header.Get("Authorization"); got != "Bearer test-service-token" {
			t.Errorf("Authorization=%q", got)
		}
		mock.Handler().ServeHTTP(w, r)
	}))
	defer server.Close()

	client := newClient(t, server.URL)
	cmd := testCommand()
	first, err := client.Deliver(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Deliver(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if first.FulfillmentID == "" || second.FulfillmentID != first.FulfillmentID || !second.Duplicate {
		t.Fatalf("unexpected results: %#v %#v", first, second)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) != 2 || keys[0] == "" || keys[0] != keys[1] {
		t.Fatalf("idempotency keys=%q", keys)
	}
}

func TestDeliverRetriesRetryableFailure(t *testing.T) {
	mock := seededMock()
	mock.SetFault(routefitmock.Fault{Status: http.StatusServiceUnavailable, Count: 1})
	server := httptest.NewServer(mock.Handler())
	defer server.Close()

	client := newClient(t, server.URL)
	result, err := client.Deliver(context.Background(), testCommand())
	if err != nil || result.FulfillmentID == "" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestDeliverTimeoutLooksUpExistingFulfillment(t *testing.T) {
	mock := seededMock()
	var once sync.Once
	var mu sync.Mutex
	postCount, lookupCount := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mu.Lock()
			postCount++
			mu.Unlock()
			mock.Handler().ServeHTTP(w, r)
			once.Do(func() { time.Sleep(50 * time.Millisecond) })
			return
		}
		mu.Lock()
		lookupCount++
		mu.Unlock()
		mock.Handler().ServeHTTP(w, r)
	}))
	defer server.Close()

	client, err := routefit.New(routefit.Config{BaseURL: server.URL, ServiceToken: "test-service-token", Timeout: 10 * time.Millisecond, RetryAttempts: 2, RetryInitialDelay: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Deliver(context.Background(), testCommand())
	if err != nil || result.FulfillmentID == "" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if postCount != 1 || lookupCount == 0 {
		t.Fatalf("post=%d lookup=%d, want one post and a lookup", postCount, lookupCount)
	}
}

func TestDeliverErrorsDoNotExposeServiceToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"UNAUTHORIZED","message":"test-service-token"}}`))
	}))
	defer server.Close()

	client := newClient(t, server.URL)
	_, err := client.Deliver(context.Background(), testCommand())
	if err == nil || contains(err.Error(), "test-service-token") {
		t.Fatalf("error leaked token: %v", err)
	}
	var upstream *routefit.Error
	if !errors.As(err, &upstream) || upstream.Status != http.StatusUnauthorized || upstream.Code != "UNAUTHORIZED" {
		t.Fatalf("error mapping=%#v", err)
	}
}

func TestNewRequiresServiceTokenConfiguration(t *testing.T) {
	_, err := routefit.New(routefit.Config{BaseURL: "https://routefit.invalid", Timeout: time.Second})
	if err == nil || contains(err.Error(), "test-service-token") {
		t.Fatalf("configuration error=%v", err)
	}
}

func TestDeliverDoesNotFollowRedirectOrForwardServiceToken(t *testing.T) {
	var mu sync.Mutex
	sinkCalls := 0
	sinkAuthorization := ""
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		sinkCalls++
		sinkAuthorization = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(routefit.Result{FulfillmentID: "attacker-1"})
	}))
	defer sink.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", sink.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer source.Close()

	client := newClient(t, source.URL)
	result, err := client.Deliver(context.Background(), testCommand())
	if err == nil || result.FulfillmentID != "" {
		t.Fatalf("redirect accepted: result=%#v err=%v", result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if sinkCalls != 0 || sinkAuthorization != "" {
		t.Fatalf("redirect was followed: calls=%d Authorization=%q", sinkCalls, sinkAuthorization)
	}
}

func TestDeliverAlwaysSendsNullableRequestedDeliveryWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		window, exists := body["requestedDeliveryWindow"]
		if !exists || string(window) != "null" {
			t.Fatalf("requestedDeliveryWindow=%s exists=%t, want explicit null", window, exists)
		}
		_ = json.NewEncoder(w).Encode(routefit.Result{FulfillmentID: "fulfillment-1"})
	}))
	defer server.Close()

	if _, err := newClient(t, server.URL).Deliver(context.Background(), testCommand()); err != nil {
		t.Fatal(err)
	}
}

func TestNewRejectsNonLoopbackHTTPServiceTokenEndpoint(t *testing.T) {
	_, err := routefit.New(routefit.Config{BaseURL: "http://routefit.example.com", ServiceToken: "test-service-token", Timeout: time.Second})
	if err == nil {
		t.Fatal("accepted cleartext non-loopback service token endpoint")
	}
}

func TestNewRejectsLookalikeLoopbackHTTPServiceTokenEndpoints(t *testing.T) {
	for _, baseURL := range []string{"http://localhost.evil.com", "http://127.0.0.1.evil.com"} {
		t.Run(baseURL, func(t *testing.T) {
			_, err := routefit.New(routefit.Config{BaseURL: baseURL, ServiceToken: "test-service-token", Timeout: time.Second})
			if err == nil {
				t.Fatalf("accepted cleartext lookalike loopback endpoint %q", baseURL)
			}
		})
	}
}

func TestDeliverRejectsInvalidRawJSONWithoutSendingAuthenticatedRequest(t *testing.T) {
	for name, mutate := range map[string]func(*routefit.CreateShipmentCmd){
		"delivery":                  func(cmd *routefit.CreateShipmentCmd) { cmd.Delivery = json.RawMessage(`not-json`) },
		"requested delivery window": func(cmd *routefit.CreateShipmentCmd) { cmd.RequestedDeliveryWindow = json.RawMessage(`not-json`) },
	} {
		t.Run(name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
			defer server.Close()

			cmd := testCommand()
			mutate(&cmd)
			_, err := newClient(t, server.URL).Deliver(context.Background(), cmd)
			if err == nil {
				t.Fatal("Deliver accepted invalid raw JSON")
			}
			if requests != 0 {
				t.Fatalf("sent %d authenticated request(s) after local validation failed", requests)
			}
		})
	}
}

func newClient(t *testing.T, baseURL string) *routefit.Client {
	t.Helper()
	client, err := routefit.New(routefit.Config{BaseURL: baseURL, ServiceToken: "test-service-token", Timeout: time.Second, RetryAttempts: 3, RetryInitialDelay: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func seededMock() *routefitmock.Mock {
	mock := routefitmock.New(routefitmock.Config{})
	mock.AddOwner(routefitmock.Owner{OwnerRef: "merchant-1", WarehouseIDs: []string{"warehouse-1"}})
	mock.AddSKU("merchant-1", routefitmock.SKU{ID: "sku-1", State: "active"}, "warehouse-1", 10)
	return mock
}

func testCommand() routefit.CreateShipmentCmd {
	return routefit.CreateShipmentCmd{
		ExternalOrderID: "DRL-order-1", OwnerRef: "merchant-1", WarehouseID: "warehouse-1", ServiceLevel: "S1",
		Lines:    []routefit.Line{{Ref: "line-1", SKU: "sku-1", Quantity: 1}},
		Delivery: json.RawMessage(`{"address":"test"}`), DeclaredValue: routefit.Money{AmountMinor: 100, Currency: "USD"},
	}
}

func contains(value, target string) bool {
	return len(target) > 0 && bytes.Contains([]byte(value), []byte(target))
}
