package catalog

import "time"

// 三语语言键；设计文档第一章约定 jsonb 文本为 {"zh":"…","en":"…","km":"…"}。
const (
	LangZH = "zh"
	LangEN = "en"
	LangKM = "km"
)

// LocalizedText 是多语言文本。草稿允许缺语言，上架前必须三语齐全。
type LocalizedText map[string]string

// ProductStatus 是商品状态（设计文档第三节 product.status）。
type ProductStatus string

const (
	StatusDraft    ProductStatus = "draft"
	StatusReview   ProductStatus = "review"
	StatusOn       ProductStatus = "on"
	StatusOff      ProductStatus = "off"
	StatusRejected ProductStatus = "rejected"
)

// Valid 报告状态是否在数据库 CHECK 允许的取值内。
func (s ProductStatus) Valid() bool {
	switch s {
	case StatusDraft, StatusReview, StatusOn, StatusOff, StatusRejected:
		return true
	default:
		return false
	}
}

// MerchantSettable 报告商家接口能否直接写入该状态。
// on 只能由上架接口流转，review/rejected 属平台审核（T-01 不做）。
func (s ProductStatus) MerchantSettable() bool {
	return s == StatusDraft || s == StatusOff
}

// MerchantStatus 是 iam.merchant.status 在 catalog 侧的最小只读投影
// （active/frozen/disabled，与 00003_iam_tables.sql 的 CHECK 一致）。
type MerchantStatus string

const (
	MerchantActive   MerchantStatus = "active"
	MerchantFrozen   MerchantStatus = "frozen"
	MerchantDisabled MerchantStatus = "disabled"
)

// 列表分页默认值与上限（与 openapi.yaml 的 Page/PageSize 参数一致）。
const (
	DefaultPage     = 1
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// 产地 origin（设计文档第三节）。
const (
	OriginCN = "cn"
	OriginLC = "lc"
)

// 来源 source（设计文档第三节）。
const (
	SourceManual = "manual"
	SourceAmazon = "amazon"
	SourceShopee = "shopee"
	SourceA1688  = "a1688"
	SourceCSV    = "csv"
)

// Tier 是阶梯价档位。MinQty 为十进制字符串（对应 numeric(18,3)），
// UnitPriceE4 为 bigint 万分之一美元；全链路不出现 float64。
type Tier struct {
	MinQty      string
	UnitPriceE4 int64
}

type AttributeType string

const (
	AttributeTypeEnum   AttributeType = "enum"
	AttributeTypeText   AttributeType = "text"
	AttributeTypeNumber AttributeType = "number"
)

type AttributeValue struct {
	Code string
	Name LocalizedText
}

type CategoryAttribute struct {
	Key            string
	Name           LocalizedText
	Type           AttributeType
	Unit           string
	Required       bool
	IsSKUDimension bool
	Values         []AttributeValue
	Sort           int
}

type SKU struct {
	PublicID        string
	AttributeValues map[string]string
	MerchantSKUCode string
	Status          ProductStatus
	Unit            string
	Tiers           []Tier
}

type SKUInput struct {
	AttributeValues map[string]string
	MerchantSKUCode string
	Unit            string
	Tiers           []Tier
}

// Category 是商品分类（catalog.category）。
type Category struct {
	PublicID    string
	Code        string
	Name        LocalizedText
	Enabled     bool
	DefaultZone string
	Sort        int
	IsCustom    bool
}

// Product 是商品（catalog.product）。
type Product struct {
	ID           int64
	PublicID     string
	MerchantID   int64
	CategoryID   int64
	CategoryCode string
	Name         LocalizedText
	Spec         LocalizedText
	Unit         string
	Origin       string
	LeadDays     *int
	VolumeM3     string
	Status       ProductStatus
	Source       string
	SourceRef    string
	StdScore     *int
	UpdatedAt    time.Time
	Tiers        []Tier
	SKUs         []SKU
}

// ProductInput 是商家创建商品的输入。
type ProductInput struct {
	CategoryCode string
	Name         LocalizedText
	Spec         LocalizedText
	Unit         string
	Origin       string
	LeadDays     *int
	VolumeM3     string
	Source       string
	SourceRef    string
	Tiers        []Tier
	SKUs         []SKUInput
}

// ProductPatch 是商家修改自己商品的增量输入；指针字段为 nil 表示不改。
type ProductPatch struct {
	CategoryCode *string
	Name         LocalizedText
	Spec         LocalizedText
	Unit         *string
	Origin       *string
	LeadDays     *int
	VolumeM3     *string
	SourceRef    *string
	Status       *ProductStatus
	Tiers        []Tier
	SKUs         *[]SKUInput
}

// ListQuery 是列表接口的分页、排序与白名单过滤参数。
type ListQuery struct {
	Page     int
	PageSize int
	Sort     string
	Status   string
	Category string
}

// ProductFilter 是列表查询的服务端强制范围，由 Service 决定，客户端无法覆盖。
type ProductFilter struct {
	MerchantID  int64
	MerchantIDs []int64
	Status      ProductStatus
}

// Plan 是已由 Service 校验并规范化的分页与排序计划。
type Plan struct {
	Page     int
	PageSize int
	Sort     string
	Category string
}

// CategoryPage 与 ProductPage 是分页结果。
type CategoryPage struct {
	Items                 []Category
	Page, PageSize, Total int
}

type ProductPage struct {
	Items                 []Product
	Page, PageSize, Total int
}
