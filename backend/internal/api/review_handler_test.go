package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/catalog"
	"github.com/leishen8806/Dr.life-/backend/internal/iam"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/server"
	"github.com/leishen8806/Dr.life-/backend/internal/trade"
)

type reviewManagementStub struct {
	filter       catalog.ProductFilter
	plan         catalog.Plan
	page         catalog.ProductPage
	product      catalog.Product
	audit        catalog.ReviewAudit
	reason       string
	approveCalls int
	rejectCalls  int
}

func (s *reviewManagementStub) Pending(_ context.Context, filter catalog.ProductFilter, plan catalog.Plan) (catalog.ProductPage, error) {
	s.filter, s.plan = filter, plan
	return s.page, nil
}
func (s *reviewManagementStub) Approve(_ context.Context, _ string, audit catalog.ReviewAudit) (catalog.Product, error) {
	s.approveCalls++
	s.audit = audit
	return s.product, nil
}
func (s *reviewManagementStub) Reject(_ context.Context, _ string, reason string, audit catalog.ReviewAudit) (catalog.Product, error) {
	s.rejectCalls++
	s.reason, s.audit = reason, audit
	return s.product, nil
}

func TestAdminCatalogReviewRequiresPermissionAndScopesQueue(t *testing.T) {
	stub := &reviewManagementStub{page: catalog.ProductPage{Page: 1, PageSize: 20, Items: []catalog.Product{}}}
	auth := stubAuth{identity: gateway.Identity{AccountID: "admin-public", Role: gateway.RoleAdmin}}
	newHandler := func(allowed bool) http.Handler {
		return server.NewHandlerWithGateway(nil, server.GatewayOptions{
			AppEnv: "dev", Limiter: gateway.NewMemoryLimiter(), IPRules: permissiveRules(), AccountRules: permissiveRules(),
			Authenticator: auth, AdminPermissions: fakeAdminPermissions{allowed: allowed}, CatalogReview: stub,
		})
	}
	request := func(handler http.Handler) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/catalog/products?merchant_id=42&submitted_from=2026-01-01T00:00:00Z", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	if got := request(newHandler(false)); got.Code != http.StatusForbidden {
		t.Fatalf("denied status=%d body=%s", got.Code, got.Body.String())
	}
	if got := request(newHandler(true)); got.Code != http.StatusOK {
		t.Fatalf("allowed status=%d body=%s", got.Code, got.Body.String())
	}
	if stub.filter.Status != catalog.StatusReview || stub.filter.MerchantID != 42 || stub.filter.SubmittedFrom == nil {
		t.Fatalf("filter=%+v", stub.filter)
	}
}

func TestAdminCatalogRejectPassesReasonAndActorToCatalog(t *testing.T) {
	stub := &reviewManagementStub{product: catalog.Product{PublicID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Status: catalog.StatusRejected}}
	handler := server.NewHandlerWithGateway(nil, server.GatewayOptions{
		AppEnv: "dev", Limiter: gateway.NewMemoryLimiter(), IPRules: permissiveRules(), AccountRules: permissiveRules(),
		Authenticator:    stubAuth{identity: gateway.Identity{AccountID: "admin-public", Role: gateway.RoleAdmin}},
		AdminPermissions: fakeAdminPermissions{allowed: true}, CatalogReview: stub,
		Directory: fakeAccountDirectory{account: iam.AccountResolution{ID: 7}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/catalog/products/01arz3ndektsv4rrffq69g5fav/reject", strings.NewReader(`{"reason":"missing dimensions"}`))
	req.Header.Set("Idempotency-Key", "review-1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK || stub.reason != "missing dimensions" || stub.audit.ActorID != 7 || stub.audit.ActorRole != "admin" {
		t.Fatalf("status=%d reason=%q audit=%+v body=%s", w.Code, stub.reason, stub.audit, w.Body.String())
	}
}

func TestAdminCatalogReviewWritesRequireEditPermission(t *testing.T) {
	stub := &reviewManagementStub{product: catalog.Product{PublicID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Status: catalog.StatusOn}}
	editAllowed := false
	handler := server.NewHandlerWithGateway(nil, server.GatewayOptions{
		AppEnv: "dev", Limiter: gateway.NewMemoryLimiter(), IPRules: permissiveRules(), AccountRules: permissiveRules(),
		Authenticator:    stubAuth{identity: gateway.Identity{AccountID: "admin-public", Role: gateway.RoleAdmin}},
		AdminPermissions: fakeAdminPermissions{allowed: true, editAllowed: &editAllowed}, CatalogReview: stub,
	})
	approve := httptest.NewRecorder()
	approveReq := httptest.NewRequest(http.MethodPost, "/v1/admin/catalog/products/01arz3ndektsv4rrffq69g5fav/approve", nil)
	approveReq.Header.Set("Content-Type", "application/json")
	approveReq.Header.Set("Idempotency-Key", "review-permission-1")
	handler.ServeHTTP(approve, approveReq)
	if approve.Code != http.StatusForbidden {
		t.Fatalf("view-only approve status=%d body=%s", approve.Code, approve.Body.String())
	}
	reject := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/catalog/products/01arz3ndektsv4rrffq69g5fav/reject", strings.NewReader(`{"reason":"missing dimensions"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "review-permission-2")
	handler.ServeHTTP(reject, req)
	if reject.Code != http.StatusForbidden {
		t.Fatalf("view-only reject status=%d body=%s", reject.Code, reject.Body.String())
	}
}

func TestAdminCatalogApproveReplaysSameIdempotencyKey(t *testing.T) {
	stub := &reviewManagementStub{product: catalog.Product{PublicID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Status: catalog.StatusOn}}
	handler := server.NewHandlerWithGateway(nil, server.GatewayOptions{
		AppEnv: "dev", Limiter: gateway.NewMemoryLimiter(), IPRules: permissiveRules(), AccountRules: permissiveRules(),
		Authenticator:    stubAuth{identity: gateway.Identity{AccountID: "admin-public", Role: gateway.RoleAdmin}},
		AdminPermissions: fakeAdminPermissions{allowed: true}, CatalogReview: stub,
		Directory:   fakeAccountDirectory{account: iam.AccountResolution{ID: 7}},
		Idempotency: gateway.IdempotencyOptions{Store: gateway.NewMemoryIdempotencyStore()},
	})
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/admin/catalog/products/01arz3ndektsv4rrffq69g5fav/approve", nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "review-replay-1")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	first, second := request(), request()
	if first.Code != http.StatusOK || second.Code != http.StatusOK || first.Body.String() != second.Body.String() || stub.approveCalls != 1 {
		t.Fatalf("first=%d second=%d calls=%d first_body=%s second_body=%s", first.Code, second.Code, stub.approveCalls, first.Body.String(), second.Body.String())
	}
}

type adminOrderStub struct {
	order trade.BuyerOrderView
}

func (s *adminOrderStub) AdminOrders(context.Context, string, int) ([]trade.BuyerOrderView, string, error) {
	return []trade.BuyerOrderView{s.order}, "next-order", nil
}
func (s *adminOrderStub) AdminOrderDetail(context.Context, string) (trade.BuyerOrderView, error) {
	return s.order, nil
}

func TestAdminOrdersRequiresDedicatedReadPermission(t *testing.T) {
	orders := &adminOrderStub{order: trade.BuyerOrderView{PublicID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Status: "submitted", ShippingAddress: trade.AddressSnapshot{Phone: "+85512345678"}}}
	newHandler := func(allowed bool) http.Handler {
		return server.NewHandlerWithGateway(nil, server.GatewayOptions{
			AppEnv: "dev", Limiter: gateway.NewMemoryLimiter(), IPRules: permissiveRules(), AccountRules: permissiveRules(),
			Authenticator:    stubAuth{identity: gateway.Identity{AccountID: "admin-public", Role: gateway.RoleAdmin}},
			AdminPermissions: fakeAdminPermissions{allowed: allowed}, AdminOrders: orders,
		})
	}
	denied := httptest.NewRecorder()
	newHandler(false).ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/v1/admin/orders", nil))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("denied status=%d body=%s", denied.Code, denied.Body.String())
	}
	allowed := httptest.NewRecorder()
	newHandler(true).ServeHTTP(allowed, httptest.NewRequest(http.MethodGet, "/v1/admin/orders", nil))
	if allowed.Code != http.StatusOK || !strings.Contains(allowed.Body.String(), orders.order.PublicID) {
		t.Fatalf("allowed status=%d body=%s", allowed.Code, allowed.Body.String())
	}
	detail := httptest.NewRecorder()
	newHandler(true).ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/v1/admin/orders/01arz3ndektsv4rrffq69g5fav", nil))
	if detail.Code != http.StatusOK || strings.Contains(detail.Body.String(), orders.order.ShippingAddress.Phone) || !strings.Contains(detail.Body.String(), "****5678") {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
}
