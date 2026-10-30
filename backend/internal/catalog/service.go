package catalog

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

// Store 是 catalog 自己 schema 的持久化边界。
type Store interface {
	ListCategories(ctx context.Context, plan Plan) (CategoryPage, error)
	ListProducts(ctx context.Context, filter ProductFilter, plan Plan) (ProductPage, error)
	Product(ctx context.Context, publicID string) (Product, error)
	CreateProduct(ctx context.Context, merchantID int64, input ProductInput) (Product, error)
	UpdateProduct(ctx context.Context, merchantID int64, publicID string, patch ProductPatch) (Product, error)
	PublishProduct(ctx context.Context, merchantID int64, publicID string) (Product, error)
	SearchProducts(ctx context.Context, filter ProductFilter, query SearchQuery) (SearchPage, error)
	SuggestProducts(ctx context.Context, filter ProductFilter, prefix string) ([]SearchSuggestion, error)
	LogSearch(ctx context.Context, query string, resultN int) error
}

// MerchantDirectory 是 iam 商户状态在 catalog 侧的只读端口。
// 跨模块查询由适配层完成（AGENTS.md 规则 8：模块只访问自己的 schema）。
type MerchantDirectory interface {
	// SellableMerchants 返回对买家可见（可售）的商户 id；验收标准④依赖它。
	SellableMerchants(ctx context.Context) ([]int64, error)
	// MerchantStatus 返回单个商户状态；未注册返回 ErrMerchantUnknown。
	MerchantStatus(ctx context.Context, merchantID int64) (MerchantStatus, error)
}

// Options 是 Service 的依赖注入项。
type Options struct {
	Merchants           MerchantDirectory
	RecommendationLimit int
}

// Service 实现商品目录的用例层：服务端范围、状态机与三语校验都在这里。
type Service struct {
	store               Store
	merchants           MerchantDirectory
	recommendationLimit int
}

func New(store Store, options Options) *Service {
	limit := options.RecommendationLimit
	if limit <= 0 {
		limit = 8
	}
	return &Service{store: store, merchants: options.Merchants, recommendationLimit: limit}
}

// allowedSorts 是排序白名单；列名永不由客户端直接拼接进 SQL。
var allowedSorts = map[string]struct{}{
	"":            {},
	"updated_at":  {},
	"-updated_at": {},
	"created_at":  {},
	"-created_at": {},
}

func normalisePlan(query ListQuery) (Plan, error) {
	plan := Plan{Page: query.Page, PageSize: query.PageSize, Sort: query.Sort, Category: query.Category}
	if plan.Page <= 0 {
		plan.Page = DefaultPage
	}
	if plan.PageSize <= 0 {
		plan.PageSize = DefaultPageSize
	}
	if plan.PageSize > MaxPageSize {
		plan.PageSize = MaxPageSize
	}
	if _, ok := allowedSorts[plan.Sort]; !ok {
		return Plan{}, invalid("sort", "unsupported sort field")
	}
	return plan, nil
}

func (s *Service) directory() (MerchantDirectory, error) {
	if s.merchants == nil {
		return nil, errors.New("catalog: merchant directory is not configured")
	}
	return s.merchants, nil
}

// PublicCategories 返回买家可见的分类（仅启用项，按 sort、code 排序）。
func (s *Service) PublicCategories(ctx context.Context, query ListQuery) (CategoryPage, error) {
	plan, err := normalisePlan(query)
	if err != nil {
		return CategoryPage{}, err
	}
	return s.store.ListCategories(ctx, plan)
}

// PublicProducts 实现验收标准④：只返回在售商品，且商户可售名单由服务端注入。
func (s *Service) PublicProducts(ctx context.Context, query ListQuery) (ProductPage, error) {
	plan, err := normalisePlan(query)
	if err != nil {
		return ProductPage{}, err
	}
	directory, err := s.directory()
	if err != nil {
		return ProductPage{}, err
	}
	sellable, err := directory.SellableMerchants(ctx)
	if err != nil {
		return ProductPage{}, err
	}
	page := ProductPage{Items: []Product{}, Page: plan.Page, PageSize: plan.PageSize}
	if len(sellable) == 0 {
		// 没有可售商户时不必查库；状态过滤由服务端强制为 on。
		return page, nil
	}
	return s.store.ListProducts(ctx, ProductFilter{MerchantIDs: sellable, Status: StatusOn}, plan)
}

func (s *Service) PublicSearch(ctx context.Context, query SearchQuery) (SearchPage, error) {
	query.Query = strings.TrimSpace(query.Query)
	if query.Query == "" {
		return SearchPage{}, invalid("q", "search query is required")
	}
	if utf8.RuneCountInString(query.Query) > MaxSearchQueryLength {
		return SearchPage{}, invalid("q", "search query is too long")
	}
	if query.MinPriceE4 < 0 || query.MaxPriceE4 < 0 || (query.MaxPriceE4 > 0 && query.MinPriceE4 > query.MaxPriceE4) {
		return SearchPage{}, invalid("price", "invalid price range")
	}
	if query.PageSize <= 0 {
		query.PageSize = DefaultPageSize
	}
	if query.PageSize > MaxPageSize {
		query.PageSize = MaxPageSize
	}
	if query.Sort == "" {
		query.Sort = SearchSortRelevance
	}
	switch query.Sort {
	case SearchSortRelevance, SearchSortPriceAsc, SearchSortPriceDesc, SearchSortNewest:
	default:
		return SearchPage{}, invalid("sort", "unsupported search sort")
	}
	directory, err := s.directory()
	if err != nil {
		return SearchPage{}, err
	}
	sellable, err := directory.SellableMerchants(ctx)
	if err != nil {
		return SearchPage{}, err
	}
	if len(sellable) == 0 {
		return SearchPage{Items: []Product{}, Recommendations: []Product{}}, nil
	}
	page, err := s.store.SearchProducts(ctx, ProductFilter{MerchantIDs: sellable, Status: StatusOn}, query)
	if err != nil {
		return SearchPage{}, err
	}
	if len(page.Items) == 0 {
		plan := Plan{Page: DefaultPage, PageSize: s.recommendationLimit, Sort: "-updated_at", Category: query.Category}
		recommended, err := s.store.ListProducts(ctx, ProductFilter{MerchantIDs: sellable, Status: StatusOn}, plan)
		if err != nil {
			return SearchPage{}, err
		}
		page.Recommendations = recommended.Items
		if query.Category == "" {
			page.RecommendationReason = RecommendationRecentAll
		} else {
			page.RecommendationReason = RecommendationRecentInCategory
		}
	}
	_ = s.store.LogSearch(ctx, query.Query, len(page.Items))
	return page, nil
}

func (s *Service) PublicSuggestions(ctx context.Context, prefix string) ([]SearchSuggestion, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return []SearchSuggestion{}, nil
	}
	if utf8.RuneCountInString(prefix) > MaxSearchQueryLength {
		return nil, invalid("q", "search query is too long")
	}
	directory, err := s.directory()
	if err != nil {
		return nil, err
	}
	sellable, err := directory.SellableMerchants(ctx)
	if err != nil {
		return nil, err
	}
	if len(sellable) == 0 {
		return []SearchSuggestion{}, nil
	}
	return s.store.SuggestProducts(ctx, ProductFilter{MerchantIDs: sellable, Status: StatusOn}, prefix)
}

// MerchantProducts 的商家范围由服务端强制，客户端无法扩大。
func (s *Service) MerchantProducts(ctx context.Context, merchantID int64, query ListQuery) (ProductPage, error) {
	if query.Status != "" && !ProductStatus(query.Status).Valid() {
		return ProductPage{}, invalid("status", "unknown product status filter")
	}
	plan, err := normalisePlan(query)
	if err != nil {
		return ProductPage{}, err
	}
	return s.store.ListProducts(ctx, ProductFilter{MerchantID: merchantID, Status: ProductStatus(query.Status)}, plan)
}

// MerchantProduct 实现验收标准③：只能读自己商品。
func (s *Service) MerchantProduct(ctx context.Context, merchantID int64, publicID string) (Product, error) {
	product, err := s.store.Product(ctx, publicID)
	if err != nil {
		return Product{}, err
	}
	if product.MerchantID != merchantID {
		return Product{}, ErrForbidden
	}
	return product, nil
}

// CreateProduct 校验三语、产地、来源与阶梯价后写库；冻结/停用商户不得创建。
func (s *Service) CreateProduct(ctx context.Context, merchantID int64, input ProductInput) (Product, error) {
	if err := s.requireActiveMerchant(ctx, merchantID); err != nil {
		return Product{}, err
	}
	if strings.TrimSpace(input.CategoryCode) == "" {
		return Product{}, invalid("category_code", "category is required")
	}
	if err := ValidateLocalized("name", input.Name); err != nil {
		return Product{}, err
	}
	if err := validateOrigin(input.Origin); err != nil {
		return Product{}, err
	}
	if err := validateSource(input.Source); err != nil {
		return Product{}, err
	}
	if strings.TrimSpace(input.Unit) == "" {
		return Product{}, invalid("unit", "unit is required")
	}
	if err := validateLeadDays(input.LeadDays); err != nil {
		return Product{}, err
	}
	if err := validateVolume(input.VolumeM3); err != nil {
		return Product{}, err
	}
	if err := ValidateTiers(input.Tiers); err != nil {
		return Product{}, err
	}
	return s.store.CreateProduct(ctx, merchantID, input)
}

// UpdateProduct 实现验收标准③，并保证在售商品改动后仍可发布（三语齐全、至少两档）。
func (s *Service) UpdateProduct(ctx context.Context, merchantID int64, publicID string, patch ProductPatch) (Product, error) {
	if patch.Status != nil && !patch.Status.MerchantSettable() {
		return Product{}, invalid("status", "merchants may only set draft or off")
	}
	product, err := s.store.Product(ctx, publicID)
	if err != nil {
		return Product{}, err
	}
	if product.MerchantID != merchantID {
		return Product{}, ErrForbidden
	}
	if err := s.requireActiveMerchant(ctx, merchantID); err != nil {
		return Product{}, err
	}
	if patch.Origin != nil {
		if err := validateOrigin(*patch.Origin); err != nil {
			return Product{}, err
		}
	}
	if patch.Unit != nil && strings.TrimSpace(*patch.Unit) == "" {
		return Product{}, invalid("unit", "unit is required")
	}
	if err := validateLeadDays(patch.LeadDays); err != nil {
		return Product{}, err
	}
	if patch.VolumeM3 != nil {
		if err := validateVolume(*patch.VolumeM3); err != nil {
			return Product{}, err
		}
	}
	effectiveStatus := product.Status
	if patch.Status != nil {
		effectiveStatus = *patch.Status
	}
	if effectiveStatus == StatusOn {
		name := product.Name
		if patch.Name != nil {
			name = patch.Name
		}
		if err := ValidateLocalized("name", name); err != nil {
			return Product{}, err
		}
		tiers := product.Tiers
		if patch.Tiers != nil {
			tiers = patch.Tiers
		}
		if err := ValidateTiers(tiers); err != nil {
			return Product{}, err
		}
	} else if patch.Tiers != nil {
		if err := ValidateTiers(patch.Tiers); err != nil {
			return Product{}, err
		}
	}
	return s.store.UpdateProduct(ctx, merchantID, publicID, patch)
}

// PublishProduct 实现验收标准①②的上架闸门：校验失败时不得触及存储。
func (s *Service) PublishProduct(ctx context.Context, merchantID int64, publicID string) (Product, error) {
	product, err := s.store.Product(ctx, publicID)
	if err != nil {
		return Product{}, err
	}
	if product.MerchantID != merchantID {
		return Product{}, ErrForbidden
	}
	if err := s.requireActiveMerchant(ctx, merchantID); err != nil {
		return Product{}, err
	}
	if err := ValidateProductForPublish(product, product.Tiers); err != nil {
		return Product{}, err
	}
	return s.store.PublishProduct(ctx, merchantID, publicID)
}

func (s *Service) requireActiveMerchant(ctx context.Context, merchantID int64) error {
	directory, err := s.directory()
	if err != nil {
		return err
	}
	status, err := directory.MerchantStatus(ctx, merchantID)
	if err != nil {
		return err
	}
	if status != MerchantActive {
		return ErrForbidden
	}
	return nil
}

func validateOrigin(origin string) error {
	switch origin {
	case OriginCN, OriginLC:
		return nil
	default:
		return invalid("origin", "origin must be cn or lc")
	}
}

// validateSource 允许留空（数据库默认 manual），但显式传入时必须合法。
func validateSource(source string) error {
	switch source {
	case "", SourceManual, SourceAmazon, SourceShopee, SourceA1688, SourceCSV:
		return nil
	default:
		return invalid("source", "unknown product source")
	}
}
