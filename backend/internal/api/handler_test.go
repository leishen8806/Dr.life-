package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	api "github.com/leishen8806/Dr.life-/backend/internal/api"
	"github.com/leishen8806/Dr.life-/backend/internal/catalog"
	"github.com/leishen8806/Dr.life-/backend/internal/iam"
	"github.com/leishen8806/Dr.life-/backend/internal/iam/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/server"
)

type fakeLogin struct {
	input  iam.LoginInput
	result iam.LoginResult
	err    error
}

type fakeAccountLinks struct {
	candidates []iam.AccountLinkCandidate
	link       iam.AccountLink
	err        error
	actor      string
	buyer      string
	linkID     string
}

type fakeAccountProfiles struct {
	profile iam.AccountProfile
	devices []iam.AccountDevice
	account string
	lang    string
	err     error
}

func (f *fakeAccountProfiles) Me(_ context.Context, account string) (iam.AccountProfile, error) {
	f.account = account
	return f.profile, f.err
}

func (f *fakeAccountProfiles) SetLanguage(_ context.Context, account, lang string) error {
	f.account, f.lang = account, lang
	return f.err
}

func (f *fakeAccountProfiles) Devices(_ context.Context, account, _ string) ([]iam.AccountDevice, error) {
	f.account = account
	return f.devices, f.err
}

func (f *fakeAccountLinks) CandidateBuyers(_ context.Context, actor string) ([]iam.AccountLinkCandidate, error) {
	f.actor = actor
	return f.candidates, f.err
}

func (f *fakeAccountLinks) Link(_ context.Context, actor, buyer string) (iam.AccountLink, error) {
	f.actor, f.buyer = actor, buyer
	return f.link, f.err
}

func (f *fakeAccountLinks) Unlink(_ context.Context, actor, linkID string) error {
	f.actor, f.linkID = actor, linkID
	return f.err
}

func (f *fakeLogin) Login(_ context.Context, input iam.LoginInput) (iam.LoginResult, error) {
	f.input = input
	return f.result, f.err
}

func accountProfileHandler(profiles api.AccountProfileManagement, auth gateway.Authenticator) http.Handler {
	return server.NewHandlerWithGateway(nil, server.GatewayOptions{
		Limiter: gateway.NewMemoryLimiter(), IPRules: permissiveRules(), AccountRules: permissiveRules(), Authenticator: auth, AccountProfiles: profiles,
	})
}

func TestMeProfileLanguageAndDevicesExposeOnlySafeFields(t *testing.T) {
	now := time.Date(2026, 10, 29, 3, 0, 0, 0, time.UTC)
	profiles := &fakeAccountProfiles{
		profile: iam.AccountProfile{PublicID: accountID, Role: "buyer", Lang: ptr("zh"), Status: "active", LangRegisteredAt: &now},
		devices: []iam.AccountDevice{{PublicID: merchantULID, FirstSeenAt: now.Add(-time.Hour), LastSeenAt: now, IsCurrent: true}},
	}
	handler := accountProfileHandler(profiles, stubAuth{identity: gateway.Identity{AccountID: accountID, SessionID: "session", Role: gateway.RoleBuyer}})

	me := serve(t, handler, httptest.NewRequest(http.MethodGet, "/v1/buyer/me", nil))
	if me.Code != http.StatusOK || profiles.account != accountID {
		t.Fatalf("me status/account=%d/%q body=%s", me.Code, profiles.account, me.Body.String())
	}
	assertJSONKeys(t, me.Body.Bytes(), "public_id", "role", "lang", "needs_language", "status")
	if strings.Contains(me.Body.String(), "telegram") || strings.Contains(me.Body.String(), "phone") {
		t.Fatalf("me response leaked personal data: %s", me.Body.String())
	}

	devices := serve(t, handler, httptest.NewRequest(http.MethodGet, "/v1/buyer/me/devices", nil))
	if devices.Code != http.StatusOK {
		t.Fatalf("devices status=%d body=%s", devices.Code, devices.Body.String())
	}
	var deviceEnvelope struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(devices.Body.Bytes(), &deviceEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(deviceEnvelope.Data.Items) != 1 || deviceEnvelope.Data.Items[0]["is_current"] != true {
		t.Fatalf("devices=%s", devices.Body.String())
	}
	assertMapKeys(t, deviceEnvelope.Data.Items[0], "public_id", "first_seen_at", "last_seen_at", "is_current")
	if strings.Contains(devices.Body.String(), "device_id") || strings.Contains(devices.Body.String(), "token") {
		t.Fatalf("devices response leaked sensitive data: %s", devices.Body.String())
	}

	for _, language := range []string{"zh", "en", "km"} {
		request := httptest.NewRequest(http.MethodPut, "/v1/buyer/me/language", strings.NewReader(`{"lang":"`+language+`"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "language-register-"+language)
		response := serve(t, handler, request)
		if response.Code != http.StatusOK || profiles.account != accountID || profiles.lang != language {
			t.Fatalf("language %q status/account/lang=%d/%q/%q body=%s", language, response.Code, profiles.account, profiles.lang, response.Body.String())
		}
	}
}

func TestMeProfileRejectsInvalidLanguageAndUnauthorizedOrWrongAudience(t *testing.T) {
	profiles := &fakeAccountProfiles{err: iam.ErrInvalidLanguage}
	handler := accountProfileHandler(profiles, stubAuth{identity: gateway.Identity{AccountID: accountID, Role: gateway.RoleBuyer}})
	for _, language := range []string{"fr", "", "ZH"} {
		request := httptest.NewRequest(http.MethodPut, "/v1/buyer/me/language", strings.NewReader(`{"lang":"`+language+`"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "language-invalid-"+language)
		response := serve(t, handler, request)
		if response.Code != http.StatusBadRequest || decodeErrorCode(t, response) != 1002 {
			t.Fatalf("language %q status/body=%d/%s", language, response.Code, response.Body.String())
		}
	}
	if response := serve(t, accountProfileHandler(profiles, nil), httptest.NewRequest(http.MethodGet, "/v1/buyer/me", nil)); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d body=%s", response.Code, response.Body.String())
	}
	if response := serve(t, handler, httptest.NewRequest(http.MethodGet, "/v1/merchant/me", nil)); response.Code != http.StatusForbidden {
		t.Fatalf("wrong audience status=%d body=%s", response.Code, response.Body.String())
	}
}

func ptr(value string) *string { return &value }

func assertJSONKeys(t *testing.T, raw []byte, keys ...string) {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	assertMapKeys(t, envelope.Data, keys...)
}

func assertMapKeys(t *testing.T, values map[string]any, want ...string) {
	t.Helper()
	if len(values) != len(want) {
		t.Fatalf("keys=%v want=%v", values, want)
	}
	for _, key := range want {
		if _, ok := values[key]; !ok {
			t.Fatalf("keys=%v missing=%q", values, key)
		}
	}
}

func TestTelegramAuthHandlerContract(t *testing.T) {
	fake := &fakeLogin{result: iam.LoginResult{AccessToken: "access", AccessExpiresIn: 900, RefreshToken: "refresh", RefreshExpiresIn: 2592000, Account: iam.LoginAccount{PublicID: "account", Role: "buyer", Status: "active"}, NeedsLanguage: true, IsNewAccount: true}}
	h := api.Handler{Login: fake}
	req := httptest.NewRequest(http.MethodPost, "/v1/public/auth/telegram", strings.NewReader(`{"end":"buyer","init_data":"signed","device_id":"device:1"}`))
	req.RemoteAddr = "198.51.100.2:1234"
	w := httptest.NewRecorder()
	httpx.RequestID(http.HandlerFunc(h.PostTelegramAuth)).ServeHTTP(w, req)
	if w.Code != http.StatusOK || fake.input.End != telegram.Buyer || fake.input.IP != "198.51.100.2" || fake.input.DeviceID != "device:1" {
		t.Fatalf("status/input=%d %+v body=%s", w.Code, fake.input, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != true || body["request_id"] == "" {
		t.Fatalf("body=%v", body)
	}
}

func TestTelegramAuthHandlerRejectsInvalidAndUnifiesErrors(t *testing.T) {
	for _, body := range []string{`{}`, `{"end":"admin","init_data":"x"}`, `{"end":"buyer","init_data":"x","extra":true}`, `{"end":"buyer","init_data":"` + strings.Repeat("x", telegram.MaxInitDataBytes+1) + `"}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/public/auth/telegram", strings.NewReader(body))
		httpx.RequestID(http.HandlerFunc((api.Handler{Login: &fakeLogin{}}).PostTelegramAuth)).ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":1002`) {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
	fake := &fakeLogin{err: httpx.NewAppError(httpx.CodeTelegramLoginFailed)}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/public/auth/telegram", strings.NewReader(`{"end":"buyer","init_data":"contains-private-detail"}`))
	httpx.RequestID(http.HandlerFunc((api.Handler{Login: fake}).PostTelegramAuth)).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "private-detail") || !strings.Contains(w.Body.String(), `"code":2005`) {
		t.Fatalf("body=%s", w.Body.String())
	}
}

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

func (s *stubStore) CategoryAttributes(context.Context, string) ([]catalog.CategoryAttribute, error) {
	return nil, nil
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

func accountLinkHandler(links api.AccountLinkManagement, auth gateway.Authenticator) http.Handler {
	return server.NewHandlerWithGateway(nil, server.GatewayOptions{
		Limiter:       gateway.NewMemoryLimiter(),
		IPRules:       permissiveRules(),
		AccountRules:  permissiveRules(),
		Authenticator: auth,
		AccountLinks:  links,
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

type fakeLedgerReader struct {
	walletOwner int64
	financial   ledger.FinancialCenterFilter
}

func (f *fakeLedgerReader) ListAdminTransactions(context.Context, int) ([]ledger.AdminTransaction, error) {
	return []ledger.AdminTransaction{{TxnNo: "TXN-1", AmountE4: 100}}, nil
}
func (f *fakeLedgerReader) GetAdminTransaction(context.Context, string) (ledger.AdminTransaction, bool, error) {
	return ledger.AdminTransaction{TxnNo: "TXN-1", AmountE4: 100}, true, nil
}
func (f *fakeLedgerReader) ListAccountReconciliations(context.Context) ([]ledger.AdminAccountReconciliation, error) {
	return nil, nil
}
func (f *fakeLedgerReader) ListAccountPostings(context.Context, int64, int) ([]ledger.AdminPosting, error) {
	return nil, nil
}
func (f *fakeLedgerReader) ListWalletTransactions(_ context.Context, q ledger.WalletTransactionQuery) (ledger.WalletTransactionPage, error) {
	f.walletOwner = q.OwnerAccountID
	return ledger.WalletTransactionPage{Items: []ledger.WalletTransaction{{TxnNo: "TXN-OWN", AmountE4: 1200, Kind: "txn"}}}, nil
}
func (f *fakeLedgerReader) GetWalletTransaction(context.Context, int64, string) (ledger.WalletTransaction, bool, error) {
	return ledger.WalletTransaction{TxnNo: "TXN-OWN", AmountE4: 1200, Kind: "txn"}, true, nil
}
func (f *fakeLedgerReader) FinancialCenter(_ context.Context, filter ledger.FinancialCenterFilter) (ledger.FinancialCenterReport, error) {
	f.financial = filter
	return ledger.FinancialCenterReport{}, nil
}

type fakeAccountDirectory struct{ account iam.AccountResolution }

func (f fakeAccountDirectory) Resolve(context.Context, string) (iam.AccountResolution, bool, error) {
	return f.account, true, nil
}

type fakeAdminPermissions struct{ allowed, scoped bool }

func (f fakeAdminPermissions) Can(context.Context, string, string, string) (bool, error) {
	return f.allowed, nil
}
func (f fakeAdminPermissions) HasDataScope(context.Context, string) (bool, error) {
	return f.scoped, nil
}

func ledgerHandler(auth gateway.Authenticator, reader api.LedgerReader, directory api.AccountDirectory, permissions api.AdminPermissionChecker) http.Handler {
	return server.NewHandlerWithGateway(nil, server.GatewayOptions{AppEnv: "dev", Limiter: gateway.NewMemoryLimiter(), IPRules: permissiveRules(), AccountRules: permissiveRules(), Authenticator: auth, Ledger: reader, Directory: directory, AdminPermissions: permissions})
}

func TestLedgerReadFailsClosedAndWalletNeverLeaksInternalFields(t *testing.T) {
	admin := ledgerHandler(stubAuth{identity: gateway.Identity{AccountID: "admin", Role: gateway.RoleAdmin}}, &fakeLedgerReader{}, nil, nil)
	if got := serve(t, admin, httptest.NewRequest(http.MethodGet, "/v1/admin/ledger/txns", nil)); got.Code != http.StatusForbidden {
		t.Fatalf("unwired admin permission status=%d body=%s", got.Code, got.Body.String())
	}

	reader := &fakeLedgerReader{}
	buyer := ledgerHandler(stubAuth{identity: gateway.Identity{AccountID: accountID, Role: gateway.RoleBuyer}}, reader, fakeAccountDirectory{account: iam.AccountResolution{ID: 41, Role: "buyer", Status: "active"}}, nil)
	response := serve(t, buyer, httptest.NewRequest(http.MethodGet, "/v1/buyer/wallet/transactions", nil))
	if response.Code != http.StatusOK || reader.walletOwner != 41 {
		t.Fatalf("wallet status=%d owner=%d body=%s", response.Code, reader.walletOwner, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "idem_key") || strings.Contains(response.Body.String(), "actor_id") {
		t.Fatalf("wallet response leaked internal fields: %s", response.Body.String())
	}

	staff := ledgerHandler(stubAuth{identity: gateway.Identity{AccountID: accountID, Role: gateway.RoleMerchant}}, &fakeLedgerReader{}, fakeAccountDirectory{account: iam.AccountResolution{ID: 41, Role: "merchant_staff", Status: "active"}}, nil)
	if got := serve(t, staff, httptest.NewRequest(http.MethodGet, "/v1/merchant/wallet/transactions", nil)); got.Code != http.StatusForbidden {
		t.Fatalf("merchant staff without fin view status=%d", got.Code)
	}
}

func TestFinancialCenterUsesAuthorizedLedgerReader(t *testing.T) {
	reader := &fakeLedgerReader{}
	handler := ledgerHandler(stubAuth{identity: gateway.Identity{AccountID: "admin", Role: gateway.RoleAdmin}}, reader, nil, fakeAdminPermissions{allowed: true})
	response := serve(t, handler, httptest.NewRequest(http.MethodGet, "/v1/admin/financial-center?from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z&merchant_limit=25", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if reader.financial.From != time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) || reader.financial.To != time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) || reader.financial.MerchantLimit != 25 {
		t.Fatalf("filter=%+v", reader.financial)
	}
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

// 验收：候选账号只从 IAM 服务取回 public_id，HTTP 层绝不返回 Telegram 编号或其他个人资料。
func TestMerchantAccountLinkCandidatesExposeOnlyBuyerPublicID(t *testing.T) {
	links := &fakeAccountLinks{candidates: []iam.AccountLinkCandidate{{AccountID: otherULID}}}
	handler := accountLinkHandler(links, merchantAuth())

	recorder := serve(t, handler, httptest.NewRequest(http.MethodGet, "/v1/merchant/me/link-candidates", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if links.actor != accountID {
		t.Fatalf("candidate actor = %q, want current account %q", links.actor, accountID)
	}
	var body struct {
		Data struct {
			Items []struct {
				PublicID string `json:"public_id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Items) != 1 || body.Data.Items[0].PublicID != otherULID {
		t.Fatalf("candidates = %+v", body.Data.Items)
	}
	if strings.Contains(recorder.Body.String(), "telegram") || strings.Contains(recorder.Body.String(), "phone") {
		t.Fatalf("candidate response leaked personal data: %s", recorder.Body.String())
	}
}

// 验收：关联和解除永远以网关注入的当前商家账号为主体，不能由请求体冒充其他账号。
func TestMerchantAccountLinksUseCurrentIdentity(t *testing.T) {
	links := &fakeAccountLinks{link: iam.AccountLink{PublicID: merchantULID, BuyerAccountID: otherULID, MerchantAccountID: accountID}}
	handler := accountLinkHandler(links, merchantAuth())

	create := httptest.NewRequest(http.MethodPost, "/v1/merchant/me/links", strings.NewReader(`{"buyer_account_id":"`+otherULID+`"}`))
	create.Header.Set("Content-Type", "application/json")
	create.Header.Set("Idempotency-Key", "account-link-create-1")
	recorder := serve(t, handler, create)
	if recorder.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if links.actor != accountID || links.buyer != otherULID {
		t.Fatalf("link input = actor=%q buyer=%q", links.actor, links.buyer)
	}

	remove := httptest.NewRequest(http.MethodDelete, "/v1/merchant/me/links/"+strings.ToLower(merchantULID), nil)
	remove.Header.Set("Content-Type", "application/json")
	remove.Header.Set("Idempotency-Key", "account-link-delete-1")
	recorder = serve(t, handler, remove)
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if links.actor != accountID || links.linkID != merchantULID {
		t.Fatalf("unlink input = actor=%q link_id=%q", links.actor, links.linkID)
	}
}

// 验收：买家不能使用商家账号关联路径；该规则必须由网关而不是客户端按钮保证。
func TestMerchantAccountLinksRejectBuyerRole(t *testing.T) {
	links := &fakeAccountLinks{}
	auth := stubAuth{identity: gateway.Identity{AccountID: accountID, Role: gateway.RoleBuyer}}
	handler := accountLinkHandler(links, auth)

	recorder := serve(t, handler, httptest.NewRequest(http.MethodGet, "/v1/merchant/me/link-candidates", nil))
	if recorder.Code != http.StatusForbidden || decodeErrorCode(t, recorder) != 2002 {
		t.Fatalf("candidate status/body = %d/%s", recorder.Code, recorder.Body.String())
	}
}

func TestMerchantAccountLinkDomainErrorsAreSafeHTTPResponses(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		method string
		path   string
		body   string
		want   int
	}{
		{name: "link forbidden", err: iam.ErrAccountLinkForbidden, method: http.MethodPost, path: "/v1/merchant/me/links", body: `{"buyer_account_id":"` + otherULID + `"}`, want: http.StatusForbidden},
		{name: "link exists", err: iam.ErrAccountLinkExists, method: http.MethodPost, path: "/v1/merchant/me/links", body: `{"buyer_account_id":"` + otherULID + `"}`, want: http.StatusConflict},
		{name: "link absent", err: iam.ErrAccountLinkNotFound, method: http.MethodDelete, path: "/v1/merchant/me/links/" + strings.ToLower(merchantULID), want: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := accountLinkHandler(&fakeAccountLinks{err: test.err}, merchantAuth())
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.Header.Set("Idempotency-Key", "account-link-error-1")
			request.Header.Set("Content-Type", "application/json")
			recorder := serve(t, handler, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
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

	patch("test-patch-null-spec", `{"spec":null}`)
	if store.patch.Spec == nil {
		t.Fatal("an explicit null spec must be a clear signal, not an absent field")
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

// 公开商品列表不得暴露供应商内部字段（source_ref / std_score）；
// 商家自己的列表仍须保留这些字段，供商家核对来源与标准分。
func TestPublicProductListHidesSupplierFields(t *testing.T) {
	score := 88
	store := &stubStore{products: []catalog.Product{{
		PublicID:   merchantULID,
		MerchantID: merchantID,
		Status:     catalog.StatusOn,
		Name:       completeName,
		SourceRef:  "supplier-ref-1",
		StdScore:   &score,
	}}}

	public := catalogHandler(store, activeDirectory(), nil)
	recorder := serve(t, public, httptest.NewRequest(http.MethodGet, "/v1/public/products", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("public status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	item := firstProductItem(t, recorder)
	for _, field := range []string{"source_ref", "std_score"} {
		if _, leaked := item[field]; leaked {
			t.Fatalf("public product leaked %s: %s", field, recorder.Body.String())
		}
	}

	merchant := catalogHandler(store, activeDirectory(), merchantAuth())
	recorder = serve(t, merchant, httptest.NewRequest(http.MethodGet, "/v1/merchant/products", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("merchant status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	item = firstProductItem(t, recorder)
	for _, field := range []string{"source_ref", "std_score"} {
		if _, present := item[field]; !present {
			t.Fatalf("merchant product lost %s: %s", field, recorder.Body.String())
		}
	}
}

func firstProductItem(t *testing.T, recorder *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var body struct {
		Data struct {
			Items []map[string]json.RawMessage `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode product list: %v body=%s", err, recorder.Body.String())
	}
	if len(body.Data.Items) != 1 {
		t.Fatalf("items = %d, want 1; body=%s", len(body.Data.Items), recorder.Body.String())
	}
	return body.Data.Items[0]
}
