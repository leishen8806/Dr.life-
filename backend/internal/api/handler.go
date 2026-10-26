package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/leishen8806/Dr.life-/backend/internal/api/gen"
	"github.com/leishen8806/Dr.life-/backend/internal/catalog"
	"github.com/leishen8806/Dr.life-/backend/internal/iam"
	"github.com/leishen8806/Dr.life-/backend/internal/iam/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

// maxBodyBytes 限制请求体大小，避免超大 JSON 进入内存。
const maxBodyBytes = 1 << 20

// MerchantResolver 把网关身份（iam.account.public_id）解析为商户内部 id。
// 跨模块读取由平台侧适配器实现，catalog 模块只消费这个端口（AGENTS.md 规则 8）。
type MerchantResolver interface {
	// MerchantID 返回账号对应的商户 id；账号不是商户、已停用或不存在时 found = false。
	MerchantID(ctx context.Context, accountID string) (merchantID int64, found bool, err error)
}

type TelegramLogin interface {
	Login(context.Context, iam.LoginInput) (iam.LoginResult, error)
}

// Handler 实现 gen.ServerInterface。
// 零值 Handler 只提供 /v1/public/ping；商品接口在依赖未注入时返回 500，
// 这样 server.NewHandler(nil) 与契约测试的既有行为保持不变。
type Handler struct {
	Catalog   *catalog.Service
	Merchants MerchantResolver
	Logger    *slog.Logger
	Login     TelegramLogin
	ClientIP  gateway.ClientIPResolver
}

func (Handler) GetPublicPing(w http.ResponseWriter, r *http.Request) {
	httpx.WriteOK(w, r, map[string]bool{"pong": true})
}

func (h Handler) PostTelegramAuth(w http.ResponseWriter, r *http.Request) {
	if h.Login == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return
	}
	var request gen.TelegramAuthRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, telegram.MaxInitDataBytes+1024))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&request)
	var trailing any
	trailingErr := decoder.Decode(&trailing)
	if err != nil || trailingErr != io.EOF || request.InitData == "" || len(request.InitData) > telegram.MaxInitDataBytes || !telegram.End(request.End).Valid() {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
		return
	}
	device := ""
	if request.DeviceId != nil {
		device = *request.DeviceId
	}
	result, err := h.Login.Login(r.Context(), iam.LoginInput{End: telegram.End(request.End), InitData: request.InitData, DeviceID: device, IP: h.ClientIP.Resolve(r)})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, result)
}

func (h Handler) ListPublicCategories(w http.ResponseWriter, r *http.Request, params gen.ListPublicCategoriesParams) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	page, err := service.PublicCategories(r.Context(), listQuery(params.Page, params.PageSize, params.Sort, "", ""))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, categoryListData(page))
}

func (h Handler) ListPublicProducts(w http.ResponseWriter, r *http.Request, params gen.ListPublicProductsParams) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	page, err := service.PublicProducts(r.Context(), listQuery(params.Page, params.PageSize, params.Sort, "", derefString(params.Category)))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, publicProductListData(page))
}

func (h Handler) ListMerchantProducts(w http.ResponseWriter, r *http.Request, params gen.ListMerchantProductsParams) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	merchantID, ok := h.merchantID(w, r)
	if !ok {
		return
	}
	var status string
	if params.Status != nil {
		status = string(*params.Status)
	}
	page, err := service.MerchantProducts(r.Context(), merchantID, listQuery(params.Page, params.PageSize, params.Sort, status, ""))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, productListData(page))
}

func (h Handler) GetMerchantProduct(w http.ResponseWriter, r *http.Request, publicID gen.PublicID) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	merchantID, ok := h.merchantID(w, r)
	if !ok {
		return
	}
	product, err := service.MerchantProduct(r.Context(), merchantID, publicIDFromPath(publicID))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, toGenProduct(product))
}

func (h Handler) CreateMerchantProduct(w http.ResponseWriter, r *http.Request, _ gen.CreateMerchantProductParams) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	merchantID, ok := h.merchantID(w, r)
	if !ok {
		return
	}
	body, ok := decodeJSON[gen.CreateMerchantProductJSONRequestBody](w, r)
	if !ok {
		return
	}
	product, err := service.CreateProduct(r.Context(), merchantID, productInput(body))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	// 创建成功返回 201；envelope 与成功响应 schema 一致。
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(gen.ProductSuccessResponse{Ok: true, Data: toGenProduct(product), RequestId: httpx.RequestIDFromContext(r.Context())})
}

func (h Handler) UpdateMerchantProduct(w http.ResponseWriter, r *http.Request, publicID gen.PublicID, _ gen.UpdateMerchantProductParams) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	merchantID, ok := h.merchantID(w, r)
	if !ok {
		return
	}
	body, ok := decodeJSON[gen.UpdateMerchantProductJSONRequestBody](w, r)
	if !ok {
		return
	}
	product, err := service.UpdateProduct(r.Context(), merchantID, publicIDFromPath(publicID), productPatch(body))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, toGenProduct(product))
}

func (h Handler) PublishMerchantProduct(w http.ResponseWriter, r *http.Request, publicID gen.PublicID, _ gen.PublishMerchantProductParams) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	merchantID, ok := h.merchantID(w, r)
	if !ok {
		return
	}
	product, err := service.PublishProduct(r.Context(), merchantID, publicIDFromPath(publicID))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, toGenProduct(product))
}

// publicIDFromPath 归一化路径中的 public_id。
// 网关的 unsafePath（internal/platform/gateway/access.go）拒绝含大写字母的路径，
// 而 ULID 的规范形式是大写；Crockford Base32 大小写等价，所以 URL 用小写，
// 这里还原成数据库里的规范大写形式再交给服务层查询。
func publicIDFromPath(publicID string) string {
	return strings.ToUpper(publicID)
}
func (h Handler) catalogService(w http.ResponseWriter, r *http.Request) (*catalog.Service, bool) {
	if h.Catalog == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInternalError))
		return nil, false
	}
	return h.Catalog, true
}

// merchantID 解析当前登录商户；身份由网关注入，未登录或角色不符在网关层已拦截。
func (h Handler) merchantID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	identity, ok := gateway.IdentityFromContext(r.Context())
	if !ok || identity.AccountID == "" {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
		return 0, false
	}
	if h.Merchants == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInternalError))
		return 0, false
	}
	merchantID, found, err := h.Merchants.MerchantID(r.Context(), identity.AccountID)
	if err != nil {
		h.writeError(w, r, err)
		return 0, false
	}
	if !found {
		// 账号不是商户或已停用：统一按越权处理，不泄漏账号是否存在。
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodePermissionDenied))
		return 0, false
	}
	return merchantID, true
}

// writeError 把领域错误映射为 1002/1003/2001/2002/5001；未识别错误按内部错误处理并记录日志。
func (h Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	code := httpx.CodeInternalError
	var invalid *catalog.ValidationError
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		code = httpx.CodeResourceNotFound
	case errors.Is(err, catalog.ErrForbidden), errors.Is(err, catalog.ErrMerchantUnknown):
		code = httpx.CodePermissionDenied
	case errors.As(err, &invalid):
		code = httpx.CodeInvalidArgument
	}
	if code == httpx.CodeInternalError && h.Logger != nil {
		h.Logger.ErrorContext(r.Context(), "catalog request failed", "error", err, "request_id", httpx.RequestIDFromContext(r.Context()))
	}
	httpx.WriteError(w, r, httpx.NewAppError(code))
}

func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var body T
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&body); err != nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
		return body, false
	}
	return body, true
}

func listQuery(page, pageSize *int, sort *string, status, category string) catalog.ListQuery {
	query := catalog.ListQuery{Status: status, Category: category}
	if page != nil {
		query.Page = *page
	}
	if pageSize != nil {
		query.PageSize = *pageSize
	}
	if sort != nil {
		query.Sort = *sort
	}
	return query
}

func productInput(body gen.ProductCreateRequest) catalog.ProductInput {
	input := catalog.ProductInput{
		CategoryCode: body.CategoryCode,
		Name:         fromGenLocalized(body.Name),
		Spec:         fromGenLocalizedPtr(body.Spec),
		Unit:         body.Unit,
		Origin:       string(body.Origin),
		LeadDays:     body.LeadDays,
		VolumeM3:     derefString(body.VolM3),
		Tiers:        fromGenTiers(body.Tiers),
	}
	if body.Source != nil {
		input.Source = string(*body.Source)
	}
	if body.SourceRef != nil {
		input.SourceRef = *body.SourceRef
	}
	return input
}

func productPatch(body gen.ProductUpdateRequest) catalog.ProductPatch {
	patch := catalog.ProductPatch{
		CategoryCode: body.CategoryCode,
		Unit:         body.Unit,
		LeadDays:     body.LeadDays,
		SourceRef:    body.SourceRef,
	}
	if body.Origin != nil {
		origin := string(*body.Origin)
		patch.Origin = &origin
	}
	if body.VolM3 != nil {
		volume := *body.VolM3
		patch.VolumeM3 = &volume
	}
	if body.Status != nil {
		status := catalog.ProductStatus(*body.Status)
		patch.Status = &status
	}
	if body.Name != nil {
		patch.Name = fromGenLocalized(*body.Name)
	}
	if body.Spec != nil {
		patch.Spec = localizedPatch(*body.Spec)
	}
	if body.Tiers != nil {
		patch.Tiers = fromGenTiers(*body.Tiers)
	}
	return patch
}

func productListData(page catalog.ProductPage) gen.ProductListData {
	items := make([]gen.Product, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, toGenProduct(item))
	}
	return gen.ProductListData{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total}
}

// publicProductListData 是匿名浏览的投影：供应商来源与内部标准分不属于买家可见信息。
// 商家列表/详情继续使用 productListData，保留这些字段供商家核对。
func publicProductListData(page catalog.ProductPage) gen.ProductListData {
	items := make([]gen.Product, 0, len(page.Items))
	for _, item := range page.Items {
		product := toGenProduct(item)
		product.SourceRef = nil
		product.StdScore = nil
		items = append(items, product)
	}
	return gen.ProductListData{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total}
}

func categoryListData(page catalog.CategoryPage) gen.CategoryListData {
	items := make([]gen.Category, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, toGenCategory(item))
	}
	return gen.CategoryListData{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total}
}

func toGenCategory(category catalog.Category) gen.Category {
	return gen.Category{
		Code:        category.Code,
		DefaultZone: optionalString(category.DefaultZone),
		Enabled:     category.Enabled,
		Name:        toGenLocalized(category.Name),
		PublicId:    category.PublicID,
		Sort:        category.Sort,
	}
}

func toGenProduct(product catalog.Product) gen.Product {
	tiers := make(gen.ProductTierList, 0, len(product.Tiers))
	for _, tier := range product.Tiers {
		tiers = append(tiers, gen.ProductTier{MinQty: tier.MinQty, UnitPriceE4: tier.UnitPriceE4})
	}
	source := product.Source
	if source == "" {
		source = catalog.SourceManual
	}
	return gen.Product{
		CategoryCode: product.CategoryCode,
		LeadDays:     product.LeadDays,
		Name:         toGenLocalized(product.Name),
		Origin:       gen.ProductOrigin(product.Origin),
		PublicId:     product.PublicID,
		Source:       gen.ProductSource(source),
		SourceRef:    optionalString(product.SourceRef),
		Spec:         toGenLocalizedPtr(product.Spec),
		Status:       gen.ProductStatus(product.Status),
		StdScore:     product.StdScore,
		Tiers:        tiers,
		Unit:         product.Unit,
		UpdatedAt:    product.UpdatedAt,
		VolM3:        optionalString(product.VolumeM3),
	}
}

func toGenLocalized(text catalog.LocalizedText) gen.LocalizedText {
	return gen.LocalizedText{Zh: text[catalog.LangZH], En: text[catalog.LangEN], Km: text[catalog.LangKM]}
}

func toGenLocalizedPtr(text catalog.LocalizedText) *gen.LocalizedText {
	if len(text) == 0 {
		return nil
	}
	localized := toGenLocalized(text)
	return &localized
}

func fromGenLocalized(text gen.LocalizedText) catalog.LocalizedText {
	return catalog.LocalizedText{catalog.LangZH: text.Zh, catalog.LangEN: text.En, catalog.LangKM: text.Km}
}

func fromGenLocalizedPtr(text *gen.LocalizedText) catalog.LocalizedText {
	if text == nil {
		return nil
	}
	return fromGenLocalized(*text)
}

// localizedPatch 映射 PATCH 请求里的三语字段：三个语种全为空白时返回空对象，
// 服务层据此把该列写成 NULL（清空）；任一语种有内容时按原文保留，
// 完整性仍由服务端在发布闸门校验。字段缺席（nil）不走这里，保持"不修改"。
func localizedPatch(text gen.LocalizedText) catalog.LocalizedText {
	localized := fromGenLocalized(text)
	for _, value := range localized {
		if strings.TrimSpace(value) != "" {
			return localized
		}
	}
	return catalog.LocalizedText{}
}

func fromGenTiers(tiers gen.ProductTierList) []catalog.Tier {
	converted := make([]catalog.Tier, 0, len(tiers))
	for _, tier := range tiers {
		converted = append(converted, catalog.Tier{MinQty: tier.MinQty, UnitPriceE4: tier.UnitPriceE4})
	}
	return converted
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
