package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/catalog"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/server"
)

const (
	merchantULID = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	otherULID    = "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	accountID    = "01ARZ3NDEKTSV4RRFFQ69G5FB0"
	merchantID   = int64(7)
)

var completeName = catalog.LocalizedText{"zh": "瓷砖", "en": "Tiles", "km": "ក្បឿង"}

// stubDirectory 同时实现 catalog.MerchantDirectory 与 api.MerchantResolver。
type stubDirectory struct {
	sellable []int64
	statuses map[int64]catalog.MerchantStatus
	accounts map[string]int64
}

func (d stubDirectory) SellableMerchants(context.Context) ([]int64, error) { return d.sellable, nil }

func (d stubDirectory) MerchantStatus(_ context.Context, id int64) (catalog.MerchantStatus, error) {
	status, ok := d.statuses[id]
	if !ok {
		return "", catalog.ErrMerchantUnknown
	}
	return status, nil
}

func (d stubDirectory) MerchantID(_ context.Context, account string) (int64, bool, error) {
	id, ok := d.accounts[account]
	return id, ok, nil
}

// stubStore 只实现 catalog.Store，用于断言接口层透传的分页/排序/过滤参数。
type stubStore struct {
	products []catalog.Product
	product  catalog.Product
	created  catalog.Product
	patch    catalog.ProductPatch
	patched  bool
	plan     catalog.Plan
	filter   catalog.ProductFilter
	lists    int
}

func (s *stubStore) ListCategories(_ context.Context, plan catalog.Plan) (catalog.CategoryPage, error) {
	s.plan = plan
	items := []catalog.Category{{PublicID: otherULID, Code: "c1", Name: completeName, Enabled: true, Sort: 1}}
	return catalog.CategoryPage{Items: items, Page: plan.Page, PageSize: plan.PageSize, Total: len(items)}, nil
}

func (s *stubStore) ListProducts(_ context.Context, filter catalog.ProductFilter, plan catalog.Plan) (catalog.ProductPage, error) {
	s.lists++
	s.plan, s.filter = plan, filter
	items := make([]catalog.Product, 0, len(s.products))
	for _, product := range s.products {
		if filter.MerchantID != 0 && product.MerchantID != filter.MerchantID {
			continue
		}
		if filter.Status != "" && product.Status != filter.Status {
			continue
		}
		items = append(items, product)
	}
	return catalog.ProductPage{Items: items, Page: plan.Page, PageSize: plan.PageSize, Total: len(items)}, nil
}

func (s *stubStore) Product(context.Context, string) (catalog.Product, error) {
	if s.product.PublicID == "" {
		return catalog.Product{}, catalog.ErrNotFound
	}
	return s.product, nil
}

func (s *stubStore) CreateProduct(context.Context, int64, catalog.ProductInput) (catalog.Product, error) {
	return s.created, nil
}

func (s *stubStore) UpdateProduct(_ context.Context, _ int64, _ string, patch catalog.ProductPatch) (catalog.Product, error) {
	s.patch, s.patched = patch, true
	return s.created, nil
}

func (s *stubStore) PublishProduct(context.Context, int64, string) (catalog.Product, error) {
	return s.created, nil
}

type stubAuth struct {
	identity gateway.Identity
}

func (a stubAuth) Authenticate(*http.Request) (gateway.Identity, bool, error) {
	return a.identity, true, nil
}

func permissiveRules() gateway.RuleSet {
	rule := gateway.Rule{Limit: 100000, Window: time.Minute}
	return gateway.RuleSet{Default: rule, Login: rule, Withdrawal: rule, AI: rule}
}

func catalogHandler(store catalog.Store, directory stubDirectory, auth gateway.Authenticator) http.Handler {
	service := catalog.New(store, catalog.Options{Merchants: directory})
	return server.NewHandlerWithGateway(nil, server.GatewayOptions{
		Limiter:       gateway.NewMemoryLimiter(),
		IPRules:       permissiveRules(),
		AccountRules:  permissiveRules(),
		Authenticator: auth,
		Catalog:       service,
		Merchants:     directory,
	})
}

func activeDirectory() stubDirectory {
	return stubDirectory{
		sellable: []int64{merchantID},
		statuses: map[int64]catalog.MerchantStatus{merchantID: catalog.MerchantActive},
		accounts: map[string]int64{accountID: merchantID},
	}
}

func merchantAuth() gateway.Authenticator {
	return stubAuth{identity: gateway.Identity{AccountID: accountID, Role: gateway.RoleMerchant}}
}

func serve(t *testing.T, handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) int {
	t.Helper()
	var body struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error envelope: %v body=%s", err, recorder.Body.String())
	}
	return body.Error.Code
}

// 验收：列表接口分页与排序透传，并且只返回当前商户的商品。
func TestMerchantListPaginatesAndForwardsSorting(t *testing.T) {
	store := &stubStore{products: []catalog.Product{
		{PublicID: merchantULID, MerchantID: merchantID, Status: catalog.StatusDraft, Name: completeName},
		{PublicID: otherULID, MerchantID: 999, Status: catalog.StatusDraft, Name: completeName},
	}}
	handler := catalogHandler(store, activeDirectory(), merchantAuth())

	request := httptest.NewRequest(http.MethodGet, "/v1/merchant/products?page=2&page_size=5&sort=-updated_at", nil)
	recorder := serve(t, handler, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if store.plan.Page != 2 || store.plan.PageSize != 5 || store.plan.Sort != "-updated_at" {
		t.Fatalf("plan = %+v, want page=2 page_size=5 sort=-updated_at", store.plan)
	}
	if store.filter.MerchantID != merchantID {
		t.Fatalf("merchant filter = %d, want %d", store.filter.MerchantID, merchantID)
	}

	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			Items []struct {
				PublicID string `json:"public_id"`
			} `json:"items"`
			Page     int `json:"page"`
			PageSize int `json:"page_size"`
			Total    int `json:"total"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.Data.Page != 2 || body.Data.PageSize != 5 || body.Data.Total != 1 {
		t.Fatalf("envelope = %+v", body)
	}
	if len(body.Data.Items) != 1 || body.Data.Items[0].PublicID != merchantULID {
		t.Fatalf("items = %+v, want only the authenticated merchant product", body.Data.Items)
	}
	if body.RequestID == "" {
		t.Fatal("request_id must be echoed")
	}
}

func TestMerchantListRejectsAnUnknownSort(t *testing.T) {
	handler := catalogHandler(&stubStore{}, activeDirectory(), merchantAuth())

	recorder := serve(t, handler, httptest.NewRequest(http.MethodGet, "/v1/merchant/products?sort=price", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	if code := decodeErrorCode(t, recorder); code != 1002 {
		t.Fatalf("code = %d, want 1002", code)
	}
}

// 验收：未登录访问商户接口按网关既有约定返回 401。
func TestMerchantListRequiresAuthentication(t *testing.T) {
	handler := catalogHandler(&stubStore{}, activeDirectory(), nil)

	recorder := serve(t, handler, httptest.NewRequest(http.MethodGet, "/v1/merchant/products", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", recorder.Code, recorder.Body.String())
	}
	if code := decodeErrorCode(t, recorder); code != 2001 {
		t.Fatalf("code = %d, want 2001", code)
	}
}

// 验收：角色与路径不匹配一律 403。
func TestMerchantListRejectsABuyerRole(t *testing.T) {
	auth := stubAuth{identity: gateway.Identity{AccountID: accountID, Role: gateway.RoleBuyer}}
	handler := catalogHandler(&stubStore{}, activeDirectory(), auth)

	recorder := serve(t, handler, httptest.NewRequest(http.MethodGet, "/v1/merchant/products", nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
	if code := decodeErrorCode(t, recorder); code != 2002 {
		t.Fatalf("code = %d, want 2002", code)
	}
}

// 验收：账号不是商户（解析不到商户 id）时不得泄漏产品，按越权处理。
func TestMerchantListRejectsAnAccountWithoutAMerchant(t *testing.T) {
	directory := activeDirectory()
	directory.accounts = map[string]int64{}
	handler := catalogHandler(&stubStore{}, directory, merchantAuth())

	recorder := serve(t, handler, httptest.NewRequest(http.MethodGet, "/v1/merchant/products", nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
	if code := decodeErrorCode(t, recorder); code != 2002 {
		t.Fatalf("code = %d, want 2002", code)
	}
}

func TestMerchantProductReportsNotFound(t *testing.T) {
	handler := catalogHandler(&stubStore{}, activeDirectory(), merchantAuth())

	recorder := serve(t, handler, httptest.NewRequest(http.MethodGet, "/v1/merchant/products/"+strings.ToLower(merchantULID), nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", recorder.Code, recorder.Body.String())
	}
	if code := decodeErrorCode(t, recorder); code != 1003 {
		t.Fatalf("code = %d, want 1003", code)
	}
}

// 验收：写接口必须带幂等键，缺失时按 1002 拒绝。
func TestCreateProductRequiresAnIdempotencyKey(t *testing.T) {
	handler := catalogHandler(&stubStore{}, activeDirectory(), merchantAuth())

	request := httptest.NewRequest(http.MethodPost, "/v1/merchant/products", strings.NewReader(validCreateBody()))
	request.Header.Set("Content-Type", "application/json")
	recorder := serve(t, handler, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	if code := decodeErrorCode(t, recorder); code != 1002 {
		t.Fatalf("code = %d, want 1002", code)
	}
}

func TestCreateProductReturnsCreatedEnvelope(t *testing.T) {
	store := &stubStore{created: catalog.Product{PublicID: merchantULID, MerchantID: merchantID, Status: catalog.StatusDraft, Name: completeName}}
	handler := catalogHandler(store, activeDirectory(), merchantAuth())

	request := httptest.NewRequest(http.MethodPost, "/v1/merchant/products", strings.NewReader(validCreateBody()))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "test-create-0001")
	recorder := serve(t, handler, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			PublicID string `json:"public_id"`
			Status   string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.Data.PublicID != merchantULID || body.Data.Status != string(catalog.StatusDraft) {
		t.Fatalf("envelope = %+v", body)
	}
}

// 验收：公开浏览接口无需登录即可访问。
func TestPublicProductListIsOpen(t *testing.T) {
	store := &stubStore{products: []catalog.Product{{PublicID: merchantULID, MerchantID: merchantID, Status: catalog.StatusOn, Name: completeName}}}
	handler := catalogHandler(store, activeDirectory(), nil)

	recorder := serve(t, handler, httptest.NewRequest(http.MethodGet, "/v1/public/products?category=c1", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if store.filter.Status != catalog.StatusOn || store.plan.Category != "c1" {
		t.Fatalf("filter/plan = %+v/%+v", store.filter, store.plan)
	}
}

// 网关（unsafePath）拒绝含大写字母的路径，所以 public_id 在 URL 里必须是小写
// （见 handler.publicIDFromPath）。这里钉住该约束，避免以后有人直接拼规范大写 ULID。
func TestUppercasePublicIDInPathIsRejectedByTheGateway(t *testing.T) {
	handler := catalogHandler(&stubStore{}, activeDirectory(), merchantAuth())

	recorder := serve(t, handler, httptest.NewRequest(http.MethodGet, "/v1/merchant/products/"+merchantULID, nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
	if code := decodeErrorCode(t, recorder); code != 2002 {
		t.Fatalf("code = %d, want 2002", code)
	}
}
func validCreateBody() string {
	return `{"category_code":"c1","name":{"zh":"瓷砖","en":"Tiles","km":"ក្បឿង"},"origin":"cn","unit":"pcs",` +
		`"tiers":[{"min_qty":"10","unit_price_e4":140000},{"min_qty":"100","unit_price_e4":120000}]}`
}

// 验收：PATCH {"spec":{}} 表示「清空规格」（存储层写 NULL），必须与「字段缺省」区分开；
// 否则显式的空对象会写成一个三语全空的对象，永远无法清空。
func TestUpdateDistinguishesEmptySpecFromAbsentSpec(t *testing.T) {
	store := &stubStore{
		product: catalog.Product{PublicID: merchantULID, MerchantID: merchantID, Status: catalog.StatusDraft, Name: completeName},
		created: catalog.Product{PublicID: merchantULID, MerchantID: merchantID, Status: catalog.StatusDraft, Name: completeName},
	}
	handler := catalogHandler(store, activeDirectory(), merchantAuth())
	path := "/v1/merchant/products/" + strings.ToLower(merchantULID)

	patch := func(key, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", key)
		recorder := serve(t, handler, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
		}
		if !store.patched {
			t.Fatal("the update never reached the store")
		}
		return recorder
	}

	patch("test-patch-empty-spec", `{"spec":{}}`)
	if store.patch.Spec == nil {
		t.Fatal("an explicit empty spec must be a clear signal, not an absent field")
	}
	if len(store.patch.Spec) != 0 {
		t.Fatalf("spec patch = %+v, want an empty map so the store writes NULL", store.patch.Spec)
	}

	patch("test-patch-absent-spec", `{"unit":"pcs"}`)
	if store.patch.Spec != nil {
		t.Fatalf("an absent spec must stay nil, got %+v", store.patch.Spec)
	}

	patch("test-patch-partial-spec", `{"spec":{"zh":"600x600"}}`)
	if len(store.patch.Spec) == 0 || store.patch.Spec["zh"] != "600x600" {
		t.Fatalf("spec patch = %+v, want the supplied spec", store.patch.Spec)
	}
}

// 验收：非法数值必须在服务端拦下并映射成 400/1002，不能落到数据库 CHECK 违反后变 500
// （审查发现 CMP-92 #1）。
func TestCreateRejectsIllegalNumbers(t *testing.T) {
	handler := catalogHandler(&stubStore{}, activeDirectory(), merchantAuth())

	cases := []struct {
		name string
		body string
	}{
		{"negative_lead_days", strings.Replace(validCreateBody(), `"origin":"cn"`, `"origin":"cn","lead_days":-1`, 1)},
		{"negative_volume", strings.Replace(validCreateBody(), `"origin":"cn"`, `"origin":"cn","vol_m3":"-1"`, 1)},
		{"non_decimal_volume", strings.Replace(validCreateBody(), `"origin":"cn"`, `"origin":"cn","vol_m3":"abc"`, 1)},
		{"oversized_volume", strings.Replace(validCreateBody(), `"origin":"cn"`, `"origin":"cn","vol_m3":"123456789"`, 1)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/merchant/products", strings.NewReader(testCase.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "test-create-invalid-"+testCase.name)
			recorder := serve(t, handler, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
			}
			if code := decodeErrorCode(t, recorder); code != 1002 {
				t.Fatalf("code = %d, want 1002", code)
			}
		})
	}
}

// 验收：PATCH 的非法数值同样在服务端拦下，unit 空白必须拒绝（与 OpenAPI minLength:1 对齐，
// 审查发现 CMP-92 #1 与 #6）；非法请求不得触及存储层。
func TestUpdateRejectsIllegalNumbers(t *testing.T) {
	store := &stubStore{product: catalog.Product{PublicID: merchantULID, MerchantID: merchantID, Status: catalog.StatusDraft, Name: completeName}}
	handler := catalogHandler(store, activeDirectory(), merchantAuth())
	path := "/v1/merchant/products/" + strings.ToLower(merchantULID)

	cases := []struct {
		name string
		body string
	}{
		{"negative_lead_days", `{"lead_days":-1}`},
		{"negative_volume", `{"vol_m3":"-1"}`},
		{"non_decimal_volume", `{"vol_m3":"1.2.3"}`},
		{"blank_unit", `{"unit":""}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store.patched = false
			request := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(testCase.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "test-patch-invalid-"+testCase.name)
			recorder := serve(t, handler, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
			}
			if code := decodeErrorCode(t, recorder); code != 1002 {
				t.Fatalf("code = %d, want 1002", code)
			}
			if store.patched {
				t.Fatal("an invalid patch must never reach the store")
			}
		})
	}
}
