package catalog_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/catalog"
)

const (
	merchantID      = int64(7)
	otherMerchantID = int64(9)
	productULID     = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
)

var (
	completeName = catalog.LocalizedText{"zh": "瓷砖", "en": "Tiles", "km": "ក្បឿង"}
	validTiers   = []catalog.Tier{{MinQty: "10", UnitPriceE4: 140000}, {MinQty: "100", UnitPriceE4: 120000}}
)

type stubDirectory struct {
	sellable []int64
	statuses map[int64]catalog.MerchantStatus
}

func (d stubDirectory) SellableMerchants(context.Context) ([]int64, error) {
	return d.sellable, nil
}

func (d stubDirectory) MerchantStatus(_ context.Context, id int64) (catalog.MerchantStatus, error) {
	status, ok := d.statuses[id]
	if !ok {
		return "", catalog.ErrMerchantUnknown
	}
	return status, nil
}

type stubStore struct {
	products     []catalog.Product
	product      catalog.Product
	created      catalog.Product
	createdFor   int64
	createdWith  catalog.ProductInput
	updatedWith  catalog.ProductPatch
	updatedFor   int64
	published    string
	publishedFor int64
	productErr   error
	listCalls    int
	filter       catalog.ProductFilter
	plan         catalog.Plan
}

func (s *stubStore) ListCategories(context.Context, catalog.Plan) (catalog.CategoryPage, error) {
	return catalog.CategoryPage{Items: []catalog.Category{{PublicID: productULID, Code: "c1"}}}, nil
}

func (s *stubStore) ListProducts(_ context.Context, filter catalog.ProductFilter, plan catalog.Plan) (catalog.ProductPage, error) {
	s.listCalls++
	s.filter, s.plan = filter, plan
	items := make([]catalog.Product, 0, len(s.products))
	for _, product := range s.products {
		if filter.MerchantIDs != nil && !slices.Contains(filter.MerchantIDs, product.MerchantID) {
			continue
		}
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
	if s.productErr != nil {
		return catalog.Product{}, s.productErr
	}
	return s.product, nil
}

func (s *stubStore) CreateProduct(_ context.Context, merchantID int64, input catalog.ProductInput) (catalog.Product, error) {
	s.createdFor, s.createdWith = merchantID, input
	return s.created, nil
}

func (s *stubStore) UpdateProduct(_ context.Context, merchantID int64, _ string, patch catalog.ProductPatch) (catalog.Product, error) {
	s.updatedFor, s.updatedWith = merchantID, patch
	return s.created, nil
}

func (s *stubStore) PublishProduct(_ context.Context, merchantID int64, publicID string) (catalog.Product, error) {
	s.publishedFor, s.published = merchantID, publicID
	published := s.product
	published.Status = catalog.StatusOn
	return published, nil
}

func serviceWith(t *testing.T, store catalog.Store, directory catalog.MerchantDirectory) *catalog.Service {
	t.Helper()
	return catalog.New(store, catalog.Options{Merchants: directory})
}

func activeMerchant() stubDirectory {
	return stubDirectory{sellable: []int64{merchantID}, statuses: map[int64]catalog.MerchantStatus{merchantID: catalog.MerchantActive, otherMerchantID: catalog.MerchantFrozen}}
}

func assertValidationField(t *testing.T, err error, want string) {
	t.Helper()
	var invalid *catalog.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want *catalog.ValidationError with field %q", err, want)
	}
	if invalid.Field != want {
		t.Fatalf("field = %q, want %q", invalid.Field, want)
	}
}

// 验收标准④：停用商户的商品对买家隐藏。
func TestPublicProductsOnlyShowProductsOfSellableMerchants(t *testing.T) {
	store := &stubStore{products: []catalog.Product{
		{PublicID: productULID, MerchantID: merchantID, Status: catalog.StatusOn, Name: completeName},
		{PublicID: "01ARZ3NDEKTSV4RRFFQ69G5FAX", MerchantID: otherMerchantID, Status: catalog.StatusOn, Name: completeName},
	}}
	service := serviceWith(t, store, activeMerchant())

	page, err := service.PublicProducts(context.Background(), catalog.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].MerchantID != merchantID {
		t.Fatalf("items = %+v, want only the active merchant product", page.Items)
	}
	if page.Total != 1 {
		t.Fatalf("total = %d, want 1", page.Total)
	}
	if store.filter.Status != catalog.StatusOn {
		t.Fatalf("status filter = %q, want %q", store.filter.Status, catalog.StatusOn)
	}
	if !slices.Equal(store.filter.MerchantIDs, []int64{merchantID}) {
		t.Fatalf("merchant allowlist = %v, want [%d]", store.filter.MerchantIDs, merchantID)
	}
}

func TestPublicProductsSkipTheQueryWhenNoMerchantIsSellable(t *testing.T) {
	store := &stubStore{products: []catalog.Product{{PublicID: productULID, MerchantID: otherMerchantID, Status: catalog.StatusOn}}}
	service := serviceWith(t, store, stubDirectory{sellable: nil, statuses: map[int64]catalog.MerchantStatus{otherMerchantID: catalog.MerchantDisabled}})

	page, err := service.PublicProducts(context.Background(), catalog.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("page = %+v, want empty", page)
	}
	if store.listCalls != 0 {
		t.Fatalf("list calls = %d, want 0", store.listCalls)
	}
}

func TestPublicSearchNormalizesQueryAndForcesOnSaleScope(t *testing.T) {
	store := &stubStore{}
	service := serviceWith(t, store, activeMerchant())

	_, err := service.PublicSearch(context.Background(), catalog.SearchQuery{Query: "  tile  ", Sort: catalog.SearchSortPriceAsc, PageSize: 12})
	if err != nil {
		t.Fatal(err)
	}
	if store.search.Filter.Status != catalog.StatusOn || !slices.Equal(store.search.Filter.MerchantIDs, []int64{merchantID}) {
		t.Fatalf("search filter = %+v, want on-sale active-merchant scope", store.search.Filter)
	}
	if store.search.Query.Query != "tile" || store.search.Query.Sort != catalog.SearchSortPriceAsc || store.search.Query.PageSize != 12 {
		t.Fatalf("search query = %+v", store.search.Query)
	}
}

func TestPublicSearchRejectsInvalidPriceAndSort(t *testing.T) {
	service := serviceWith(t, &stubStore{}, activeMerchant())
	for _, query := range []catalog.SearchQuery{
		{Query: "tile", MinPriceE4: -1},
		{Query: "tile", MinPriceE4: 20, MaxPriceE4: 10},
		{Query: "tile", Sort: "advertised"},
		{Query: ""},
	} {
		if _, err := service.PublicSearch(context.Background(), query); err == nil {
			t.Fatalf("query %+v must fail", query)
		}
	}
}

// 验收标准③：商家只能改自己商品。
func TestMerchantCannotReachAnotherMerchantProduct(t *testing.T) {
	store := &stubStore{product: catalog.Product{PublicID: productULID, MerchantID: merchantID, Status: catalog.StatusDraft}}
	service := serviceWith(t, store, activeMerchant())
	ctx := context.Background()

	if _, err := service.MerchantProduct(ctx, otherMerchantID, productULID); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("read = %v, want ErrForbidden", err)
	}
	if _, err := service.UpdateProduct(ctx, otherMerchantID, productULID, catalog.ProductPatch{Unit: new("box")}); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("update = %v, want ErrForbidden", err)
	}
	if _, err := service.PublishProduct(ctx, otherMerchantID, productULID); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("publish = %v, want ErrForbidden", err)
	}

	product, err := service.MerchantProduct(ctx, merchantID, productULID)
	if err != nil {
		t.Fatal(err)
	}
	if product.PublicID != productULID {
		t.Fatalf("public id = %q", product.PublicID)
	}
}

func TestMerchantProductReportsMissingProduct(t *testing.T) {
	store := &stubStore{productErr: catalog.ErrNotFound}
	service := serviceWith(t, store, activeMerchant())

	if _, err := service.MerchantProduct(context.Background(), merchantID, productULID); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

// 验收标准①：三语不齐不能上架；验收标准②：阶梯价至少 2 档。
func TestPublishGateRequiresTrilingualNameAndTwoTiers(t *testing.T) {
	draft := catalog.Product{PublicID: productULID, MerchantID: merchantID, Status: catalog.StatusDraft, Name: completeName, Tiers: validTiers}

	store := &stubStore{product: draft}
	service := serviceWith(t, store, activeMerchant())

	published, err := service.PublishProduct(context.Background(), merchantID, productULID)
	if err != nil {
		t.Fatalf("publish = %v, want nil", err)
	}
	if published.PublicID != productULID || store.published != productULID || store.publishedFor != merchantID {
		t.Fatalf("publish did not reach the store: %+v", store)
	}

	incomplete := draft
	incomplete.Name = catalog.LocalizedText{"zh": "瓷砖", "en": "Tiles"}
	store = &stubStore{product: incomplete}
	service = serviceWith(t, store, activeMerchant())
	if _, err := service.PublishProduct(context.Background(), merchantID, productULID); err != nil {
		assertValidationField(t, err, "name.km")
	} else {
		t.Fatal("publish of an incomplete trilingual name must fail")
	}
	if store.published != "" {
		t.Fatal("publish must not reach the store when the gate fails")
	}

	singleTier := draft
	singleTier.Tiers = validTiers[:1]
	store = &stubStore{product: singleTier}
	service = serviceWith(t, store, activeMerchant())
	if _, err := service.PublishProduct(context.Background(), merchantID, productULID); err != nil {
		assertValidationField(t, err, "tiers")
	} else {
		t.Fatal("publish with a single tier must fail")
	}
}

func TestPublishRejectsAnAlreadyPublishedProduct(t *testing.T) {
	store := &stubStore{product: catalog.Product{PublicID: productULID, MerchantID: merchantID, Status: catalog.StatusOn, Name: completeName, Tiers: validTiers}}
	service := serviceWith(t, store, activeMerchant())

	if _, err := service.PublishProduct(context.Background(), merchantID, productULID); err != nil {
		assertValidationField(t, err, "status")
	} else {
		t.Fatal("publishing an on-sale product must fail")
	}
}

func TestWritesRequireAnActiveMerchant(t *testing.T) {
	store := &stubStore{created: catalog.Product{PublicID: productULID, MerchantID: merchantID}}
	service := serviceWith(t, store, activeMerchant())
	ctx := context.Background()

	if _, err := service.CreateProduct(ctx, copyInt64(otherMerchantID), catalog.ProductInput{Origin: catalog.OriginCN}); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("frozen merchant create = %v, want ErrForbidden", err)
	}
	if _, err := service.CreateProduct(ctx, 999, catalog.ProductInput{Origin: catalog.OriginCN}); !errors.Is(err, catalog.ErrMerchantUnknown) {
		t.Fatalf("unknown merchant create = %v, want ErrMerchantUnknown", err)
	}
	if store.createdFor != 0 {
		t.Fatal("create must not reach the store for a merchant that is not active")
	}
}

func TestCreateValidatesOriginSourceAndTiers(t *testing.T) {
	store := &stubStore{created: catalog.Product{PublicID: productULID, MerchantID: merchantID}}
	service := serviceWith(t, store, activeMerchant())
	ctx := context.Background()

	base := catalog.ProductInput{CategoryCode: "c1", Name: completeName, Unit: "pcs", Origin: catalog.OriginCN, Tiers: validTiers}

	if _, err := service.CreateProduct(ctx, merchantID, withOrigin(base, "xx")); err != nil {
		assertValidationField(t, err, "origin")
	} else {
		t.Fatal("unknown origin must fail")
	}
	if _, err := service.CreateProduct(ctx, merchantID, withTiers(base, validTiers[:1])); err != nil {
		assertValidationField(t, err, "tiers")
	} else {
		t.Fatal("a single tier must fail")
	}
	if _, err := service.CreateProduct(ctx, merchantID, withName(base, catalog.LocalizedText{"zh": "瓷砖", "en": "Tiles"})); err != nil {
		assertValidationField(t, err, "name.km")
	} else {
		t.Fatal("an incomplete trilingual name must fail")
	}
	if _, err := service.CreateProduct(ctx, merchantID, withSource(base, "aliexpress")); err != nil {
		assertValidationField(t, err, "source")
	} else {
		t.Fatal("unknown source must fail")
	}

	created, err := service.CreateProduct(ctx, merchantID, base)
	if err != nil {
		t.Fatal(err)
	}
	if created.PublicID != productULID || store.createdFor != merchantID {
		t.Fatalf("create did not reach the store: %+v", store)
	}
}

func TestUpdateRejectsStatusTheMerchantMayNotSet(t *testing.T) {
	store := &stubStore{product: catalog.Product{PublicID: productULID, MerchantID: merchantID, Status: catalog.StatusDraft, Name: completeName, Tiers: validTiers}}
	service := serviceWith(t, store, activeMerchant())

	on := catalog.StatusOn
	if _, err := service.UpdateProduct(context.Background(), merchantID, productULID, catalog.ProductPatch{Status: &on}); err != nil {
		assertValidationField(t, err, "status")
	} else {
		t.Fatal("a merchant must not set the on status directly")
	}

	off := catalog.StatusOff
	if _, err := service.UpdateProduct(context.Background(), merchantID, productULID, catalog.ProductPatch{Status: &off}); err != nil {
		t.Fatalf("draft to off = %v, want nil", err)
	}
	if store.updatedWith.Status == nil || *store.updatedWith.Status != catalog.StatusOff {
		t.Fatalf("store patch = %+v", store.updatedWith)
	}
}

func TestUpdateKeepsAnOnSaleProductPublishable(t *testing.T) {
	on := catalog.StatusOn
	store := &stubStore{product: catalog.Product{PublicID: productULID, MerchantID: merchantID, Status: on, Name: completeName, Tiers: validTiers}}
	service := serviceWith(t, store, activeMerchant())

	incomplete := catalog.LocalizedText{"zh": "瓷砖", "en": "Tiles"}
	if _, err := service.UpdateProduct(context.Background(), merchantID, productULID, catalog.ProductPatch{Name: incomplete}); err != nil {
		assertValidationField(t, err, "name.km")
	} else {
		t.Fatal("an on-sale product must keep a complete trilingual name")
	}

	if _, err := service.UpdateProduct(context.Background(), merchantID, productULID, catalog.ProductPatch{Name: completeName, Tiers: validTiers[:1]}); err != nil {
		assertValidationField(t, err, "tiers")
	} else {
		t.Fatal("an on-sale product must keep at least two tiers")
	}
}

func TestMerchantProductsAlwaysScopeToTheAuthenticatedMerchant(t *testing.T) {
	store := &stubStore{products: []catalog.Product{
		{PublicID: productULID, MerchantID: merchantID, Status: catalog.StatusDraft},
		{PublicID: "01ARZ3NDEKTSV4RRFFQ69G5FAX", MerchantID: otherMerchantID, Status: catalog.StatusDraft},
	}}
	service := serviceWith(t, store, activeMerchant())

	page, err := service.MerchantProducts(context.Background(), merchantID, catalog.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].MerchantID != merchantID {
		t.Fatalf("items = %+v", page.Items)
	}
	if store.filter.MerchantID != merchantID {
		t.Fatalf("merchant filter = %d, want %d", store.filter.MerchantID, merchantID)
	}
	if store.filter.MerchantIDs != nil {
		t.Fatal("merchant listing must not use the buyer-side allowlist")
	}
}

func TestMerchantProductsRejectAnUnknownStatusFilter(t *testing.T) {
	service := serviceWith(t, &stubStore{}, activeMerchant())

	if _, err := service.MerchantProducts(context.Background(), merchantID, catalog.ListQuery{Status: "archived"}); err != nil {
		assertValidationField(t, err, "status")
	} else {
		t.Fatal("an unknown status filter must fail")
	}
}

func copyInt64(value int64) int64 { return value }

func withOrigin(input catalog.ProductInput, origin string) catalog.ProductInput {
	input.Origin = origin
	return input
}

func withTiers(input catalog.ProductInput, tiers []catalog.Tier) catalog.ProductInput {
	input.Tiers = tiers
	return input
}

func withName(input catalog.ProductInput, name catalog.LocalizedText) catalog.ProductInput {
	input.Name = name
	return input
}

func withSource(input catalog.ProductInput, source string) catalog.ProductInput {
	input.Source = source
	return input
}

// 验收：与 CHECK 约束对应的非法取值必须在服务端拦成 400（invalid_argument），
// 不能落到数据库的 check_violation 报 500。
func TestCreateRejectsIllegalLeadDaysAndVolume(t *testing.T) {
	store := &stubStore{created: catalog.Product{PublicID: productULID, MerchantID: merchantID}}
	service := serviceWith(t, store, activeMerchant())
	ctx := context.Background()
	base := catalog.ProductInput{CategoryCode: "c1", Name: completeName, Unit: "pcs", Origin: catalog.OriginCN, Tiers: validTiers}

	negative := -1
	withNegativeDays := base
	withNegativeDays.LeadDays = &negative
	if _, err := service.CreateProduct(ctx, merchantID, withNegativeDays); err != nil {
		assertValidationField(t, err, "lead_days")
	} else {
		t.Fatal("a negative lead time must fail before reaching the database")
	}

	for _, volume := range []string{"-1", "abc", "1.23456", "1e3", "1.2.3", "999999999"} {
		withBadVolume := base
		withBadVolume.VolumeM3 = volume
		if _, err := service.CreateProduct(ctx, merchantID, withBadVolume); err != nil {
			assertValidationField(t, err, "vol_m3")
		} else {
			t.Fatalf("volume %q must fail before reaching the database", volume)
		}
	}

	withVolume := base
	withVolume.VolumeM3 = "0.0125"
	if _, err := service.CreateProduct(ctx, merchantID, withVolume); err != nil {
		t.Fatalf("a plain decimal volume = %v, want nil", err)
	}
}

// 验收：PATCH 走与建表 CHECK 一致的服务端校验（unit 非空、lead_days >= 0、vol_m3 十进制）。
func TestUpdateRejectsBlankUnitAndIllegalNumbers(t *testing.T) {
	store := &stubStore{
		product: catalog.Product{PublicID: productULID, MerchantID: merchantID, Status: catalog.StatusDraft, Name: completeName, Tiers: validTiers},
		created: catalog.Product{PublicID: productULID, MerchantID: merchantID},
	}
	service := serviceWith(t, store, activeMerchant())
	ctx := context.Background()

	blank := "   "
	if _, err := service.UpdateProduct(ctx, merchantID, productULID, catalog.ProductPatch{Unit: &blank}); err != nil {
		assertValidationField(t, err, "unit")
	} else {
		t.Fatal("a blank unit must fail before reaching the database")
	}

	negative := -1
	if _, err := service.UpdateProduct(ctx, merchantID, productULID, catalog.ProductPatch{LeadDays: &negative}); err != nil {
		assertValidationField(t, err, "lead_days")
	} else {
		t.Fatal("a negative lead time must fail before reaching the database")
	}

	badVolume := "-1"
	if _, err := service.UpdateProduct(ctx, merchantID, productULID, catalog.ProductPatch{VolumeM3: &badVolume}); err != nil {
		assertValidationField(t, err, "vol_m3")
	} else {
		t.Fatal("a negative volume must fail before reaching the database")
	}

	cleared := ""
	if _, err := service.UpdateProduct(ctx, merchantID, productULID, catalog.ProductPatch{VolumeM3: &cleared}); err != nil {
		t.Fatalf("clearing the volume = %v, want nil", err)
	}
}
