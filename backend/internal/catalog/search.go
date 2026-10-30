package catalog

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// SearchProducts searches the existing full-text and trgm indexes. The cursor
// is an opaque offset token; search is public read-only data and short-lived
// HTTP caching limits how long a page can be reused.
func (s *PgStore) SearchProducts(ctx context.Context, filter ProductFilter, query SearchQuery) (SearchPage, error) {
	offset, err := searchOffset(query.Cursor)
	if err != nil {
		return SearchPage{}, invalid("cursor", "invalid cursor")
	}
	where, args := searchWhere(filter, query)
	countSQL := `WITH matches AS (` + searchMatchesSQL + `) SELECT count(*) FROM matches p JOIN catalog.category c ON c.id=p.category_id` + where
	var total int
	if err := s.db.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return SearchPage{}, err
	}
	order := searchOrder(query.Sort)
	args = append(args, query.PageSize, offset)
	rows, err := s.db.Query(ctx, `WITH matches AS (`+searchMatchesSQL+`) SELECT `+productColumns+` FROM matches p JOIN catalog.category c ON c.id=p.category_id`+where+` ORDER BY `+order+` LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return SearchPage{}, err
	}
	defer rows.Close()
	items, err := scanProducts(rows)
	if err != nil {
		return SearchPage{}, err
	}
	if err := attachTiers(ctx, s.db, items); err != nil {
		return SearchPage{}, err
	}
	page := SearchPage{Items: items, Recommendations: []Product{}}
	if offset+len(items) < total {
		page.NextCursor = encodeSearchOffset(offset + len(items))
	}
	return page, nil
}

func (s *PgStore) SuggestProducts(ctx context.Context, filter ProductFilter, prefix string) ([]SearchSuggestion, error) {
	where, args := productWhere(filter, Plan{})
	args = append(args, strings.ToLower(prefix)+"%")
	rows, err := s.db.Query(ctx, `SELECT DISTINCT coalesce(p.name->>'zh', p.name->>'en', p.name->>'km') FROM catalog.product p JOIN catalog.category c ON c.id=p.category_id`+where+` AND lower(coalesce(p.name->>'zh','') || ' ' || coalesce(p.name->>'en','') || ' ' || coalesce(p.name->>'km','')) LIKE $`+strconv.Itoa(len(args))+` ORDER BY 1 LIMIT 10`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]SearchSuggestion, 0, 10)
	for rows.Next() {
		var item SearchSuggestion
		if err := rows.Scan(&item.Text); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PgStore) LogSearch(ctx context.Context, query string, resultN int) error {
	_, err := s.db.Exec(ctx, `INSERT INTO catalog.search_log(q,result_n) VALUES($1,$2)`, truncateSearchQuery(query), resultN)
	return err
}

func truncateSearchQuery(query string) string {
	if utf8.RuneCountInString(query) <= MaxSearchQueryLength {
		return query
	}
	return string([]rune(query)[:MaxSearchQueryLength])
}

const searchMatchesSQL = `
 SELECT p.*, greatest(ts_rank_cd(p.search_text, plainto_tsquery('simple', $1)), similarity(coalesce(p.name->>'zh','') || ' ' || coalesce(p.name->>'en','') || ' ' || coalesce(p.name->>'km',''), $1)) AS search_rank,
 (SELECT min(unit_price_e4) FROM catalog.product_tier t WHERE t.product_id=p.id) AS min_price_e4
 FROM catalog.product p
 WHERE p.search_text @@ plainto_tsquery('simple', $1)
    OR (coalesce(p.name->>'zh','') || ' ' || coalesce(p.name->>'en','') || ' ' || coalesce(p.name->>'km','')) % $1
	OR coalesce(p.name->>'zh','') ILIKE $1 || '%'
	OR coalesce(p.name->>'en','') ILIKE $1 || '%'
	OR coalesce(p.name->>'km','') ILIKE $1 || '%'
    OR EXISTS (SELECT 1 FROM catalog.search_synonym s, unnest(s.terms) term WHERE lower(term)=lower($1) AND EXISTS (SELECT 1 FROM unnest(s.terms) synonym WHERE lower(coalesce(p.name->>'zh','') || ' ' || coalesce(p.name->>'en','') || ' ' || coalesce(p.name->>'km','')) LIKE '%' || lower(synonym) || '%'))`

func searchWhere(filter ProductFilter, query SearchQuery) (string, []any) {
	where, args := productWhere(filter, Plan{Category: query.Category})
	args = append([]any{query.Query}, args...)
	// productWhere placeholders begin after the query supplied by searchMatchesSQL.
	for i := len(args) - 1; i >= 1; i-- {
		where = strings.ReplaceAll(where, "$"+strconv.Itoa(i), "$"+strconv.Itoa(i+1))
	}
	if query.MinPriceE4 > 0 {
		args = append(args, query.MinPriceE4)
		where += " AND p.min_price_e4 >= $" + strconv.Itoa(len(args))
	}
	if query.MaxPriceE4 > 0 {
		args = append(args, query.MaxPriceE4)
		where += " AND p.min_price_e4 <= $" + strconv.Itoa(len(args))
	}
	return where, args
}

func searchOrder(sort SearchSort) string {
	switch sort {
	case SearchSortPriceAsc:
		return "p.min_price_e4 ASC NULLS LAST, p.id"
	case SearchSortPriceDesc:
		return "p.min_price_e4 DESC NULLS LAST, p.id DESC"
	case SearchSortNewest:
		return "p.created_at DESC, p.id DESC"
	default:
		return "p.search_rank DESC, p.id DESC"
	}
}

func encodeSearchOffset(value int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(value)))
}
func searchOffset(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(string(raw))
	if err != nil || value < 0 {
		return 0, errors.New("invalid")
	}
	return value, nil
}
