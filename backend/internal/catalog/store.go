package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

// rowQuerier 是 catalog 读取所需的最小数据库能力；*pgxpool.Pool 与 pgx.Tx 都满足。
type rowQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// pool 在 rowQuerier 之上额外提供事务能力；只有 *pgxpool.Pool 满足这一层。
type pool interface {
	rowQuerier
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// PgStore 是 Store 的 PostgreSQL 实现，只读写 catalog schema（AGENTS.md 规则 8）。
type PgStore struct {
	db pool
}

var _ Store = (*PgStore)(nil)

func NewPgStore(db pool) *PgStore {
	return &PgStore{db: db}
}

func (s *PgStore) CategoryAttributes(ctx context.Context, categoryCode string) ([]CategoryAttribute, error) {
	rows, err := s.db.Query(ctx, `SELECT a.key, a.name, a.type, coalesce(a.unit,''), a.required, a.is_sku_dimension, a.value_json, a.sort FROM catalog.category_attribute a JOIN catalog.category c ON c.id=a.category_id WHERE c.code=$1 ORDER BY a.sort,a.id`, categoryCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	attributes := []CategoryAttribute{}
	for rows.Next() {
		var a CategoryAttribute
		var name, values []byte
		if err := rows.Scan(&a.Key, &name, &a.Type, &a.Unit, &a.Required, &a.IsSKUDimension, &values, &a.Sort); err != nil {
			return nil, err
		}
		if err := decodeLocalized(name, &a.Name); err != nil {
			return nil, err
		}
		if len(values) != 0 {
			if err := json.Unmarshal(values, &a.Values); err != nil {
				return nil, fmt.Errorf("catalog: decode attribute values: %w", err)
			}
		}
		attributes = append(attributes, a)
	}
	return attributes, rows.Err()
}

// productColumns 是列表与单条读取共用的投影。
// numeric 一律用 trim_scale 转 text 出库，金额/体积永不经由 float64。
const productColumns = `p.id, p.public_id, p.merchant_id, p.category_id, c.code, p.name, p.spec, p.unit,
	p.origin, p.lead_days, trim_scale(p.vol_m3)::text, p.status, p.source, p.source_ref, p.std_score, p.updated_at`

// productSortSQL 是排序白名单到 SQL 片段的映射：客户端输入永不拼接进 SQL。
var productSortSQL = map[string]string{
	"":            "p.updated_at DESC, p.id DESC",
	"updated_at":  "p.updated_at, p.id",
	"-updated_at": "p.updated_at DESC, p.id DESC",
	"created_at":  "p.created_at, p.id",
	"-created_at": "p.created_at DESC, p.id DESC",
}

// ListCategories 只返回启用分类，排序固定为 sort、code（与 openapi 描述一致）。
func (s *PgStore) ListCategories(ctx context.Context, plan Plan) (CategoryPage, error) {
	var total int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM catalog.category WHERE enabled`).Scan(&total); err != nil {
		return CategoryPage{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT public_id, code, name, enabled, coalesce(default_zone, ''), sort, is_custom
		FROM catalog.category
		WHERE enabled
		ORDER BY sort, code
		LIMIT $1 OFFSET $2`, plan.PageSize, offset(plan))
	if err != nil {
		return CategoryPage{}, err
	}
	defer rows.Close()

	items := make([]Category, 0, plan.PageSize)
	for rows.Next() {
		var (
			category Category
			name     []byte
		)
		if err := rows.Scan(&category.PublicID, &category.Code, &name, &category.Enabled, &category.DefaultZone, &category.Sort, &category.IsCustom); err != nil {
			return CategoryPage{}, err
		}
		if err := decodeLocalized(name, &category.Name); err != nil {
			return CategoryPage{}, err
		}
		items = append(items, category)
	}
	if err := rows.Err(); err != nil {
		return CategoryPage{}, err
	}
	return CategoryPage{Items: items, Page: plan.Page, PageSize: plan.PageSize, Total: total}, nil
}

func (s *PgStore) ListProducts(ctx context.Context, filter ProductFilter, plan Plan) (ProductPage, error) {
	page := ProductPage{Items: []Product{}, Page: plan.Page, PageSize: plan.PageSize}
	if filter.MerchantIDs != nil && len(filter.MerchantIDs) == 0 {
		// 可售商户为空时不必查库（Service 也会短路，这里是存储层兜底）。
		return page, nil
	}
	where, args := productWhere(filter, plan)
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM catalog.product p JOIN catalog.category c ON c.id = p.category_id`+where, args...).Scan(&page.Total); err != nil {
		return ProductPage{}, err
	}
	order := productSortSQL[plan.Sort]
	if order == "" {
		order = productSortSQL[""]
	}
	query := `SELECT ` + productColumns + ` FROM catalog.product p JOIN catalog.category c ON c.id = p.category_id` + where +
		` ORDER BY ` + order + ` LIMIT $` + strconv.Itoa(len(args)+1) + ` OFFSET $` + strconv.Itoa(len(args)+2)
	rows, err := s.db.Query(ctx, query, append(append([]any{}, args...), plan.PageSize, offset(plan))...)
	if err != nil {
		return ProductPage{}, err
	}
	defer rows.Close()

	items, err := scanProducts(rows)
	if err != nil {
		return ProductPage{}, err
	}
	if err := attachTiers(ctx, s.db, items); err != nil {
		return ProductPage{}, err
	}
	page.Items = items
	return page, nil
}

func (s *PgStore) Product(ctx context.Context, publicID string) (Product, error) {
	product, err := productByPublicID(ctx, s.db, publicID)
	if err != nil {
		return Product{}, err
	}
	return refreshTiers(ctx, s.db, product)
}

func (s *PgStore) CreateProduct(ctx context.Context, merchantID int64, input ProductInput) (Product, error) {
	publicID, err := ids.NewULID()
	if err != nil {
		return Product{}, err
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Product{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	categoryID, err := categoryIDByCode(ctx, tx, input.CategoryCode)
	if err != nil {
		return Product{}, err
	}
	var productID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO catalog.product (public_id, merchant_id, category_id, name, spec, unit, origin, lead_days, vol_m3, status, source, source_ref)
		VALUES ($1, $2, $3, $4::text::jsonb, $5::text::jsonb, $6, $7, $8, $9::text::numeric, $10, $11, $12)
		RETURNING id`,
		publicID, merchantID, categoryID, localizedJSON(input.Name), nullableLocalizedJSON(input.Spec), input.Unit,
		input.Origin, input.LeadDays, nullableText(input.VolumeM3), string(StatusDraft), sourceOrDefault(input.Source), nullableText(input.SourceRef),
	).Scan(&productID); err != nil {
		return Product{}, err
	}
	if input.SKUs == nil {
		if err := replaceTiers(ctx, tx, productID, input.Tiers); err != nil {
			return Product{}, err
		}
		if err := createDefaultSKU(ctx, tx, productID, input.Tiers); err != nil {
			return Product{}, err
		}
	} else if err := replaceSKUs(ctx, tx, productID, input.SKUs); err != nil {
		return Product{}, err
	}
	product, err := productByID(ctx, tx, productID)
	if err != nil {
		return Product{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Product{}, err
	}
	return product, nil
}

func (s *PgStore) UpdateProduct(ctx context.Context, merchantID int64, publicID string, patch ProductPatch) (Product, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Product{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 先锁行再校验：发布与编辑并发时，校验依据与最终写入必须是同一个快照。
	locked, err := lockProduct(ctx, tx, merchantID, publicID)
	if err != nil {
		return Product{}, err
	}
	effectiveStatus := locked.Status
	if patch.Status != nil {
		effectiveStatus = *patch.Status
	}
	if effectiveStatus == StatusOn {
		name := locked.Name
		if patch.Name != nil {
			name = patch.Name
		}
		if err := ValidateLocalized("name", name); err != nil {
			return Product{}, err
		}
		tiers := locked.Tiers
		if patch.Tiers != nil {
			tiers = patch.Tiers
		}
		if err := ValidateTiers(tiers); err != nil {
			return Product{}, err
		}
	}

	sets := []string{"updated_at = now()"}
	args := []any{}
	// add 只接受本函数内的常量片段，占位符由参数个数推导，客户端输入永不进入 SQL 文本。
	add := func(value any, fragment string) {
		args = append(args, value)
		sets = append(sets, strings.Replace(fragment, "?", "$"+strconv.Itoa(len(args)), 1))
	}
	if patch.CategoryCode != nil {
		categoryID, err := categoryIDByCode(ctx, tx, *patch.CategoryCode)
		if err != nil {
			return Product{}, err
		}
		add(categoryID, "category_id = ?")
	}
	if patch.Name != nil {
		add(localizedJSON(patch.Name), "name = ?::text::jsonb")
	}
	if patch.Spec != nil && len(patch.Spec) == 0 {
		// 空对象表示清空规格；非空对象整体替换。
		add(nil, "spec = ?::text::jsonb")
	} else if patch.Spec != nil {
		add(localizedJSON(patch.Spec), "spec = ?::text::jsonb")
	}
	if patch.Unit != nil {
		add(*patch.Unit, "unit = ?")
	}
	if patch.Origin != nil {
		add(*patch.Origin, "origin = ?")
	}
	if patch.LeadDays != nil {
		add(*patch.LeadDays, "lead_days = ?")
	}
	if patch.VolumeM3 != nil {
		add(nullableText(*patch.VolumeM3), "vol_m3 = ?::text::numeric")
	}
	if patch.SourceRef != nil {
		add(nullableText(*patch.SourceRef), "source_ref = ?")
	}
	if patch.Status != nil {
		add(string(*patch.Status), "status = ?")
	}
	args = append(args, publicID, merchantID)
	var productID int64
	err = tx.QueryRow(ctx, `UPDATE catalog.product SET `+strings.Join(sets, ", ")+
		` WHERE public_id = $`+strconv.Itoa(len(args)-1)+` AND merchant_id = $`+strconv.Itoa(len(args))+` RETURNING id`, args...).Scan(&productID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	if err != nil {
		return Product{}, err
	}
	if patch.Tiers != nil {
		if err := replaceTiers(ctx, tx, productID, patch.Tiers); err != nil {
			return Product{}, err
		}
		if err := replaceDefaultSKUTiers(ctx, tx, productID, patch.Tiers); err != nil {
			return Product{}, err
		}
	} else if patch.SKUs != nil {
		if err := replaceSKUs(ctx, tx, productID, *patch.SKUs); err != nil {
			return Product{}, err
		}
	}
	product, err := productByID(ctx, tx, productID)
	if err != nil {
		return Product{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Product{}, err
	}
	return product, nil
}

// PublishProduct 把商品置为在售。归属与上架门槛在 Service 预检，
// 这里在同一事务内锁行并重新校验，避免校验后、写入前被并发 PATCH 改坏。
func (s *PgStore) PublishProduct(ctx context.Context, merchantID int64, publicID string) (Product, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Product{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	locked, err := lockProduct(ctx, tx, merchantID, publicID)
	if err != nil {
		return Product{}, err
	}
	if err := ValidateProductForPublish(locked, locked.Tiers); err != nil {
		return Product{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE catalog.product SET status = $1, updated_at = now()
		WHERE id = $2`, string(StatusOn), locked.ID); err != nil {
		return Product{}, err
	}
	product, err := productByID(ctx, tx, locked.ID)
	if err != nil {
		return Product{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Product{}, err
	}
	return product, nil
}

// lockProduct 以 merchant_id 为范围锁住商品行，并在同一事务里读取阶梯价。
// 调用方必须持有 tx，锁在提交/回滚时释放。
func lockProduct(ctx context.Context, tx pgx.Tx, merchantID int64, publicID string) (Product, error) {
	var productID int64
	err := tx.QueryRow(ctx, `
		SELECT id FROM catalog.product
		WHERE public_id = $1 AND merchant_id = $2
		FOR UPDATE`, publicID, merchantID).Scan(&productID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	if err != nil {
		return Product{}, err
	}
	return productByID(ctx, tx, productID)
}

func productWhere(filter ProductFilter, plan Plan) (string, []any) {
	clauses := []string{"true"}
	args := []any{}
	if filter.MerchantIDs != nil {
		args = append(args, filter.MerchantIDs)
		clauses = append(clauses, "p.merchant_id = ANY($"+strconv.Itoa(len(args))+")")
	}
	if filter.MerchantID != 0 {
		args = append(args, filter.MerchantID)
		clauses = append(clauses, "p.merchant_id = $"+strconv.Itoa(len(args)))
	}
	if filter.Status != "" {
		args = append(args, string(filter.Status))
		clauses = append(clauses, "p.status = $"+strconv.Itoa(len(args)))
	}
	if plan.Category != "" {
		args = append(args, plan.Category)
		clauses = append(clauses, "c.code = $"+strconv.Itoa(len(args)))
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func productByPublicID(ctx context.Context, q rowQuerier, publicID string) (Product, error) {
	product, err := scanProduct(q.QueryRow(ctx, `SELECT `+productColumns+`
		FROM catalog.product p JOIN catalog.category c ON c.id = p.category_id
		WHERE p.public_id = $1`, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	if err != nil {
		return Product{}, err
	}
	return product, nil
}

func productByID(ctx context.Context, q rowQuerier, productID int64) (Product, error) {
	product, err := scanProduct(q.QueryRow(ctx, `SELECT `+productColumns+`
		FROM catalog.product p JOIN catalog.category c ON c.id = p.category_id
		WHERE p.id = $1`, productID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	if err != nil {
		return Product{}, err
	}
	return refreshTiers(ctx, q, product)
}

func scanProducts(rows pgx.Rows) ([]Product, error) {
	items := make([]Product, 0, 16)
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, product)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func scanProduct(row pgx.Row) (Product, error) {
	var (
		product                Product
		name, spec             []byte
		origin, status, source string
		volumeM3, sourceRef    *string
	)
	if err := row.Scan(&product.ID, &product.PublicID, &product.MerchantID, &product.CategoryID, &product.CategoryCode,
		&name, &spec, &product.Unit, &origin, &product.LeadDays, &volumeM3, &status, &source, &sourceRef,
		&product.StdScore, &product.UpdatedAt); err != nil {
		return Product{}, err
	}
	if err := decodeLocalized(name, &product.Name); err != nil {
		return Product{}, err
	}
	if err := decodeLocalized(spec, &product.Spec); err != nil {
		return Product{}, err
	}
	product.Origin = origin
	product.Status = ProductStatus(status)
	product.Source = source
	if volumeM3 != nil {
		product.VolumeM3 = *volumeM3
	}
	if sourceRef != nil {
		product.SourceRef = *sourceRef
	}
	return product, nil
}

// refreshTiers 读取单条商品的阶梯价。
func refreshTiers(ctx context.Context, q rowQuerier, product Product) (Product, error) {
	tiers, err := listTiers(ctx, q, []int64{product.ID})
	if err != nil {
		return Product{}, err
	}
	product.Tiers = tiers[product.ID]
	if product.Tiers == nil {
		product.Tiers = []Tier{}
	}
	return refreshSKUs(ctx, q, product)
}

func refreshSKUs(ctx context.Context, q rowQuerier, product Product) (Product, error) {
	rows, err := q.Query(ctx, `SELECT id, public_id, attribute_values, coalesce(merchant_sku_code, ''), status, coalesce(unit, '') FROM catalog.sku WHERE product_id = $1 ORDER BY id`, product.ID)
	if err != nil {
		return Product{}, err
	}
	defer rows.Close()
	product.SKUs = []SKU{}
	for rows.Next() {
		var id int64
		var raw []byte
		var sku SKU
		var status string
		if err := rows.Scan(&id, &sku.PublicID, &raw, &sku.MerchantSKUCode, &status, &sku.Unit); err != nil {
			return Product{}, err
		}
		if err := json.Unmarshal(raw, &sku.AttributeValues); err != nil {
			return Product{}, fmt.Errorf("catalog: decode sku attributes: %w", err)
		}
		sku.Status = ProductStatus(status)
		tierRows, err := q.Query(ctx, `SELECT trim_scale(min_qty)::text, unit_price_e4 FROM catalog.sku_tier WHERE sku_id = $1 ORDER BY min_qty`, id)
		if err != nil {
			return Product{}, err
		}
		for tierRows.Next() {
			var tier Tier
			if err := tierRows.Scan(&tier.MinQty, &tier.UnitPriceE4); err != nil {
				tierRows.Close()
				return Product{}, err
			}
			sku.Tiers = append(sku.Tiers, tier)
		}
		if err := tierRows.Err(); err != nil {
			tierRows.Close()
			return Product{}, err
		}
		tierRows.Close()
		product.SKUs = append(product.SKUs, sku)
	}
	if err := rows.Err(); err != nil {
		return Product{}, err
	}
	return product, nil
}

// attachTiers 批量补齐列表结果里的阶梯价（一次查询，避免 N+1）。
func attachTiers(ctx context.Context, q rowQuerier, products []Product) error {
	if len(products) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(products))
	for _, product := range products {
		ids = append(ids, product.ID)
	}
	tiers, err := listTiers(ctx, q, ids)
	if err != nil {
		return err
	}
	for i := range products {
		if list, ok := tiers[products[i].ID]; ok {
			products[i].Tiers = list
		} else {
			products[i].Tiers = []Tier{}
		}
		product, err := refreshSKUs(ctx, q, products[i])
		if err != nil {
			return err
		}
		products[i] = product
	}
	return nil
}

func listTiers(ctx context.Context, q rowQuerier, productIDs []int64) (map[int64][]Tier, error) {
	tiers := make(map[int64][]Tier, len(productIDs))
	if len(productIDs) == 0 {
		return tiers, nil
	}
	rows, err := q.Query(ctx, `
		SELECT product_id, trim_scale(min_qty)::text, unit_price_e4
		FROM catalog.product_tier
		WHERE product_id = ANY($1)
		ORDER BY product_id, min_qty`, productIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			productID int64
			tier      Tier
		)
		if err := rows.Scan(&productID, &tier.MinQty, &tier.UnitPriceE4); err != nil {
			return nil, err
		}
		tiers[productID] = append(tiers[productID], tier)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tiers, nil
}

// replaceTiers 整体替换阶梯价：起订量经 ParseDecimal 规范化，(product_id, min_qty) 唯一约束兜底。
func replaceTiers(ctx context.Context, tx pgx.Tx, productID int64, tiers []Tier) error {
	if _, err := tx.Exec(ctx, `DELETE FROM catalog.product_tier WHERE product_id = $1`, productID); err != nil {
		return err
	}
	for _, tier := range tiers {
		quantity, err := ParseDecimal(tier.MinQty, 3)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO catalog.product_tier (product_id, min_qty, unit_price_e4)
			VALUES ($1, $2::text::numeric, $3)`, productID, quantity, tier.UnitPriceE4); err != nil {
			return err
		}
	}
	return nil
}

func createDefaultSKU(ctx context.Context, tx pgx.Tx, productID int64, tiers []Tier) error {
	publicID, err := ids.NewULID()
	if err != nil {
		return err
	}
	var skuID int64
	if err := tx.QueryRow(ctx, `INSERT INTO catalog.sku (public_id, product_id, attribute_values, status) VALUES ($1, $2, '{}'::jsonb, 'off') RETURNING id`, publicID, productID).Scan(&skuID); err != nil {
		return err
	}
	return replaceSKUTiers(ctx, tx, skuID, tiers)
}

func replaceDefaultSKUTiers(ctx context.Context, tx pgx.Tx, productID int64, tiers []Tier) error {
	var skuID int64
	err := tx.QueryRow(ctx, `SELECT id FROM catalog.sku WHERE product_id = $1 AND attribute_values = '{}'::jsonb FOR UPDATE`, productID).Scan(&skuID)
	if err != nil {
		return err
	}
	return replaceSKUTiers(ctx, tx, skuID, tiers)
}

func replaceSKUTiers(ctx context.Context, tx pgx.Tx, skuID int64, tiers []Tier) error {
	if _, err := tx.Exec(ctx, `DELETE FROM catalog.sku_tier WHERE sku_id = $1`, skuID); err != nil {
		return err
	}
	for _, tier := range tiers {
		quantity, err := ParseDecimal(tier.MinQty, 3)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO catalog.sku_tier (sku_id, min_qty, unit_price_e4) VALUES ($1, $2::text::numeric, $3)`, skuID, quantity, tier.UnitPriceE4); err != nil {
			return err
		}
	}
	return nil
}

func replaceSKUs(ctx context.Context, tx pgx.Tx, productID int64, skus []SKUInput) error {
	if _, err := tx.Exec(ctx, `DELETE FROM catalog.sku WHERE product_id=$1`, productID); err != nil {
		return err
	}
	for _, sku := range skus {
		publicID, err := ids.NewULID()
		if err != nil {
			return err
		}
		values, err := json.Marshal(sku.AttributeValues)
		if err != nil {
			return err
		}
		var skuID int64
		if err := tx.QueryRow(ctx, `INSERT INTO catalog.sku (public_id,product_id,attribute_values,merchant_sku_code,unit,status) VALUES ($1,$2,$3::jsonb,NULLIF($4,''),NULLIF($5,''),'on') RETURNING id`, publicID, productID, string(values), sku.MerchantSKUCode, sku.Unit).Scan(&skuID); err != nil {
			return err
		}
		if err := replaceSKUTiers(ctx, tx, skuID, sku.Tiers); err != nil {
			return err
		}
	}
	return nil
}

func categoryIDByCode(ctx context.Context, q rowQuerier, code string) (int64, error) {
	var id int64
	err := q.QueryRow(ctx, `SELECT id FROM catalog.category WHERE code = $1 AND enabled`, code).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, invalid("category_code", "unknown or disabled category")
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

func decodeLocalized(raw []byte, target *LocalizedText) error {
	if len(raw) == 0 {
		return nil
	}
	decoded := LocalizedText{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("catalog: decode localized text: %w", err)
	}
	*target = decoded
	return nil
}

func localizedJSON(text LocalizedText) string {
	encoded, err := json.Marshal(text)
	if err != nil || len(text) == 0 {
		return "{}"
	}
	return string(encoded)
}

// nullableLocalizedJSON 把空的三语文本映射为 NULL（spec 可空）。
func nullableLocalizedJSON(text LocalizedText) *string {
	if len(text) == 0 {
		return nil
	}
	encoded := localizedJSON(text)
	return &encoded
}

func nullableText(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func sourceOrDefault(source string) string {
	if source == "" {
		return SourceManual
	}
	return source
}

func offset(plan Plan) int {
	page, size := plan.Page, plan.PageSize
	if page <= 0 {
		page = DefaultPage
	}
	if size <= 0 {
		size = DefaultPageSize
	}
	return (page - 1) * size
}
