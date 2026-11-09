package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/api/gen"
	"github.com/leishen8806/Dr.life-/backend/internal/iam"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/trade"
)

type orderTestAuth struct{ role gateway.Role }

func (a orderTestAuth) Authenticate(*http.Request) (gateway.Identity, bool, error) {
	return gateway.Identity{AccountID: "01ARZ3NDEKTSV4RRFFQ69G5FA1", Role: a.role}, true, nil
}

type orderTestDirectory struct{ merchantID *int64 }

func (d orderTestDirectory) Resolve(context.Context, string) (iam.AccountResolution, bool, error) {
	return iam.AccountResolution{ID: 7, Role: "buyer", MerchantID: d.merchantID, Status: "active"}, true, nil
}

type orderTestService struct {
	createCalls, paymentCalls, cancelCalls int
	merchantID                             int64
	createErr, paymentErr, cancelErr       error
	buyer                                  trade.BuyerOrderView
	merchant                               trade.MerchantOrderView
}

func (s *orderTestService) BuyerOrders(context.Context, int64, string, int) ([]trade.BuyerOrderView, string, error) {
	return []trade.BuyerOrderView{s.buyer}, "", nil
}
func (s *orderTestService) PlaceForDelivery(_ context.Context, _ int64, _ string, _ string, _ []trade.OrderLineInput) (trade.CreateOrder, error) {
	s.createCalls++
	if s.createErr != nil {
		return trade.CreateOrder{}, s.createErr
	}
	return trade.CreateOrder{PublicID: s.buyer.PublicID}, nil
}
func (s *orderTestService) BuyerOrderDetail(context.Context, int64, string) (trade.BuyerOrderView, error) {
	return s.buyer, nil
}
func (s *orderTestService) Cancel(context.Context, int64, string) error {
	s.cancelCalls++
	return s.cancelErr
}
func (s *orderTestService) CreatePayment(context.Context, int64, string) (trade.PaymentResult, error) {
	s.paymentCalls++
	if s.paymentErr != nil {
		return trade.PaymentResult{}, s.paymentErr
	}
	return trade.PaymentResult{PaymentNo: "DRL-ORDER-1", PaymentLink: "https://pay.example.test/1", ExpiresAt: time.Unix(1_800_000_000, 0)}, nil
}
func (s *orderTestService) MerchantOrders(_ context.Context, merchantID int64, _ string, _ int) ([]trade.MerchantOrderView, string, error) {
	s.merchantID = merchantID
	return []trade.MerchantOrderView{s.merchant}, "", nil
}
func (s *orderTestService) MerchantOrderDetail(context.Context, int64, string) (trade.MerchantOrderView, error) {
	return s.merchant, nil
}

func orderEndpoint(role gateway.Role, endpoint http.Handler) http.Handler {
	return httpx.RequestID(gateway.Authenticate(orderTestAuth{role: role}, "")(endpoint))
}

func TestCreateBuyerOrderRejectsClientControlledPriceAndMerchant(t *testing.T) {
	service := &orderTestService{}
	h := Handler{Orders: service, Directory: orderTestDirectory{}}
	req := httptest.NewRequest(http.MethodPost, "/v1/buyer/orders", strings.NewReader(`{"items":[{"sku_public_id":"01ARZ3NDEKTSV4RRFFQ69G5FA2","quantity":1,"unit_price_e4":100}],"shipping_address_public_id":"01ARZ3NDEKTSV4RRFFQ69G5FA3","merchant_id":9}`))
	w := httptest.NewRecorder()
	orderEndpoint(gateway.RoleBuyer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.CreateBuyerOrder(w, r, gen.CreateBuyerOrderParams{IdempotencyKey: "order-1"})
	})).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || service.createCalls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, service.createCalls, w.Body.String())
	}
}

func TestCreateBuyerOrderRejectsFractionalQuantity(t *testing.T) {
	service := &orderTestService{}
	h := Handler{Orders: service, Directory: orderTestDirectory{}}
	req := httptest.NewRequest(http.MethodPost, "/v1/buyer/orders", strings.NewReader(`{"items":[{"sku_public_id":"01ARZ3NDEKTSV4RRFFQ69G5FA2","quantity":1.5}],"shipping_address_public_id":"01ARZ3NDEKTSV4RRFFQ69G5FA3"}`))
	w := httptest.NewRecorder()
	orderEndpoint(gateway.RoleBuyer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.CreateBuyerOrder(w, r, gen.CreateBuyerOrderParams{IdempotencyKey: "order-2"})
	})).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || service.createCalls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, service.createCalls, w.Body.String())
	}
}

func TestMerchantSuborderMasksPhoneAndUsesMerchantScope(t *testing.T) {
	merchantID := int64(19)
	service := &orderTestService{merchant: trade.MerchantOrderView{PublicID: "child", ParentPublicID: "parent", Status: string(trade.AwaitingPayment), GoodsAmountE4: 100, PayableAmountE4: 100, ShippingQuoteExpiresAt: time.Unix(1_800_000_000, 0), ShippingAddress: trade.AddressSnapshot{PublicID: "address", RecipientName: "Buyer", AddressText: "Phnom Penh", Phone: "+85512345678"}, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0)}}
	h := Handler{Orders: service, Directory: orderTestDirectory{merchantID: &merchantID}}
	req := httptest.NewRequest(http.MethodGet, "/v1/merchant/suborders", nil)
	w := httptest.NewRecorder()
	orderEndpoint(gateway.RoleMerchant, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ListMerchantSuborders(w, r, gen.ListMerchantSubordersParams{})
	})).ServeHTTP(w, req)
	if w.Code != http.StatusOK || service.merchantID != merchantID || !strings.Contains(w.Body.String(), `"phone":"****5678"`) || strings.Contains(w.Body.String(), "+85512345678") {
		t.Fatalf("status=%d merchant=%d body=%s", w.Code, service.merchantID, w.Body.String())
	}
}

func TestMerchantSuborderMasksShortPhoneCompletely(t *testing.T) {
	merchantID := int64(19)
	service := &orderTestService{merchant: trade.MerchantOrderView{PublicID: "child", ParentPublicID: "parent", Status: string(trade.AwaitingPayment), GoodsAmountE4: 100, PayableAmountE4: 100, ShippingAddress: trade.AddressSnapshot{Phone: "123"}}}
	h := Handler{Orders: service, Directory: orderTestDirectory{merchantID: &merchantID}}
	req := httptest.NewRequest(http.MethodGet, "/v1/merchant/suborders", nil)
	w := httptest.NewRecorder()
	orderEndpoint(gateway.RoleMerchant, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ListMerchantSuborders(w, r, gen.ListMerchantSubordersParams{})
	})).ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"phone":"****"`) || strings.Contains(w.Body.String(), `"phone":"123"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestConfirmingPaymentBlocksCancellation(t *testing.T) {
	service := &orderTestService{cancelErr: trade.ErrPaymentConfirming}
	h := Handler{Orders: service, Directory: orderTestDirectory{}}
	req := httptest.NewRequest(http.MethodPost, "/v1/buyer/orders/order/cancel", nil)
	w := httptest.NewRecorder()
	orderEndpoint(gateway.RoleBuyer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.CancelBuyerOrder(w, r, "order", gen.CancelBuyerOrderParams{IdempotencyKey: "cancel-1"})
	})).ServeHTTP(w, req)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":3005`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestExpiredShippingQuoteBlocksPayment(t *testing.T) {
	service := &orderTestService{paymentErr: trade.ErrShippingQuoteExpired}
	h := Handler{Orders: service, Directory: orderTestDirectory{}}
	req := httptest.NewRequest(http.MethodPost, "/v1/buyer/orders/order/payments", nil)
	w := httptest.NewRecorder()
	orderEndpoint(gateway.RoleBuyer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.CreateBuyerOrderPayment(w, r, "order", gen.CreateBuyerOrderPaymentParams{IdempotencyKey: "payment-quote-expired"})
	})).ServeHTTP(w, req)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":3006`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestMissingShippingAddressIsNotInternalError(t *testing.T) {
	service := &orderTestService{createErr: trade.ErrAddressNotFound}
	h := Handler{Orders: service, Directory: orderTestDirectory{}}
	req := httptest.NewRequest(http.MethodPost, "/v1/buyer/orders", strings.NewReader(`{"items":[{"sku_public_id":"01ARZ3NDEKTSV4RRFFQ69G5FA2","quantity":1}],"shipping_address_public_id":"missing"}`))
	w := httptest.NewRecorder()
	orderEndpoint(gateway.RoleBuyer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.CreateBuyerOrder(w, r, gen.CreateBuyerOrderParams{IdempotencyKey: "missing-address"})
	})).ServeHTTP(w, req)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"code":1003`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestOrderWritesReplayOneResultPerIdempotencyKey(t *testing.T) {
	service := &orderTestService{buyer: trade.BuyerOrderView{PublicID: "01ARZ3NDEKTSV4RRFFQ69G5FA4", Status: string(trade.AwaitingPayment), PaymentStatus: "pending", GoodsAmountE4: 100, PayableAmountE4: 100, ShippingAddress: trade.AddressSnapshot{PublicID: "01ARZ3NDEKTSV4RRFFQ69G5FA3"}, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0)}}
	h := Handler{Orders: service, Directory: orderTestDirectory{}}
	store := gateway.NewMemoryIdempotencyStore()
	wrap := func(endpoint http.Handler) http.Handler {
		return orderEndpoint(gateway.RoleBuyer, gateway.RequireIdempotency(nil)(gateway.BindIdempotency(gateway.IdempotencyOptions{Store: store})(endpoint)))
	}
	cases := []struct {
		name, path, body string
		status           int
		endpoint         http.Handler
		calls            func() int
	}{
		{"create", "/v1/buyer/orders", `{"items":[{"sku_public_id":"01ARZ3NDEKTSV4RRFFQ69G5FA2","quantity":1}],"shipping_address_public_id":"01ARZ3NDEKTSV4RRFFQ69G5FA3"}`, http.StatusCreated, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.CreateBuyerOrder(w, r, gen.CreateBuyerOrderParams{IdempotencyKey: r.Header.Get("Idempotency-Key")})
		}), func() int { return service.createCalls }},
		{"payment", "/v1/buyer/orders/order/payments", "", http.StatusCreated, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.CreateBuyerOrderPayment(w, r, "order", gen.CreateBuyerOrderPaymentParams{IdempotencyKey: r.Header.Get("Idempotency-Key")})
		}), func() int { return service.paymentCalls }},
		{"cancel", "/v1/buyer/orders/order/cancel", "", http.StatusOK, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.CancelBuyerOrder(w, r, "order", gen.CancelBuyerOrderParams{IdempotencyKey: r.Header.Get("Idempotency-Key")})
		}), func() int { return service.cancelCalls }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := wrap(tc.endpoint)
			for i := 0; i < 2; i++ {
				req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
				req.Header.Set("Idempotency-Key", tc.name+"-1")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				if w.Code != tc.status {
					t.Fatalf("attempt=%d status=%d body=%s", i, w.Code, w.Body.String())
				}
			}
			if tc.calls() != 1 {
				t.Fatalf("calls=%d", tc.calls())
			}
		})
	}
}
