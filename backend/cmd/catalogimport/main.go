// Command catalogimport loads the reviewed catalog taxonomy and SKU templates.
// It is intentionally idempotent: CSV identifiers map deterministically to
// catalog.category.code, and repeated input updates the same rows.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

const reviewSuffix = "（待审）"

type localized struct{ ZH, EN, KM string }
type enumValue struct {
	Code string `json:"code"`
	ZH   string `json:"zh"`
	EN   string `json:"en"`
	KM   string `json:"km"`
}
type categoryRow struct {
	SourceID, ParentID string
	Level, Sort        int
	Name               localized
	KMPendingReview    bool
}
type attributeRow struct {
	CategoryID, Key string
	Name            localized
	Type, Unit      string
	Required, SKU   bool
	Values          []enumValue
	KMPendingReview bool
}
type report struct {
	CategoriesInserted int      `json:"categories_inserted"`
	CategoriesUpdated  int      `json:"categories_updated"`
	AttributesInserted int      `json:"attributes_inserted"`
	AttributesUpdated  int      `json:"attributes_updated"`
	Errors             []string `json:"errors"`
}

func main() {
	var dsn, categoriesPath, attributesPath, reportPath string
	flag.StringVar(&dsn, "database-url", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL (or DATABASE_URL)")
	flag.StringVar(&categoriesPath, "categories", "data/catalog/categories.csv", "categories CSV path")
	flag.StringVar(&attributesPath, "attributes", "data/catalog/sku_attributes.csv", "SKU attributes CSV path")
	flag.StringVar(&reportPath, "report", "-", "JSON difference report path, or - for stdout")
	flag.Parse()
	if dsn == "" {
		fatal(errors.New("database-url or DATABASE_URL is required"))
	}
	r, err := importFiles(context.Background(), dsn, categoriesPath, attributesPath)
	if err != nil {
		fatal(err)
	}
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		fatal(err)
	}
	if reportPath == "-" {
		fmt.Println(string(encoded))
	} else if err := os.WriteFile(reportPath, append(encoded, '\n'), 0o600); err != nil {
		fatal(err)
	}
	if len(r.Errors) > 0 {
		os.Exit(1)
	}
}

func importFiles(ctx context.Context, dsn, categoriesPath, attributesPath string) (report, error) {
	categories, err := readCategories(categoriesPath)
	if err != nil {
		return report{}, err
	}
	attributes, err := readAttributes(attributesPath)
	if err != nil {
		return report{}, err
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return report{}, err
	}
	defer pool.Close()
	return importCatalog(ctx, pool, categories, attributes)
}

func importCatalog(ctx context.Context, db *pgxpool.Pool, categories []categoryRow, attributes []attributeRow) (report, error) {
	known := make(map[string]struct{}, len(categories))
	for _, c := range categories {
		known[c.SourceID] = struct{}{}
	}
	for _, c := range categories {
		if (c.Level == 1 && c.ParentID != "") || (c.Level > 1 && c.ParentID == "") {
			return report{}, fmt.Errorf("category %s: parent_id conflicts with level", c.SourceID)
		}
		if c.ParentID != "" {
			if _, ok := known[c.ParentID]; !ok {
				return report{}, fmt.Errorf("category %s: unknown parent_id %s", c.SourceID, c.ParentID)
			}
		}
	}
	r := report{}
	tx, err := db.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer tx.Rollback(ctx)
	for _, c := range categories {
		code := categoryCode(c.SourceID)
		name, _ := json.Marshal(map[string]string{"zh": c.Name.ZH, "en": c.Name.EN, "km": c.Name.KM})
		var existed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM catalog.category WHERE code = $1)`, code).Scan(&existed); err != nil {
			return r, err
		}
		if existed {
			_, err = tx.Exec(ctx, `UPDATE catalog.category SET name=$2::jsonb, sort=$3, enabled=true, is_custom=false, km_pending_review=$4, updated_at=now() WHERE code=$1`, code, string(name), c.Sort, c.KMPendingReview)
			r.CategoriesUpdated++
		} else {
			publicID, idErr := ids.NewULID()
			if idErr != nil {
				return r, idErr
			}
			_, err = tx.Exec(ctx, `INSERT INTO catalog.category (public_id, code, name, enabled, sort, is_custom, km_pending_review) VALUES ($1,$2,$3::jsonb,true,$4,false,$5)`, publicID, code, string(name), c.Sort, c.KMPendingReview)
			r.CategoriesInserted++
		}
		if err != nil {
			return r, err
		}
	}
	for line, a := range attributes {
		if a.SKU && a.Type != "enum" {
			r.Errors = append(r.Errors, fmt.Sprintf("attributes row %d: sku_variant requires type enum", line+2))
			continue
		}
		if a.Type != "enum" && len(a.Values) != 0 {
			r.Errors = append(r.Errors, fmt.Sprintf("attributes row %d: non-enum values are not allowed", line+2))
			continue
		}
		categoryID := categoryCode(a.CategoryID)
		var id int64
		if err := tx.QueryRow(ctx, `SELECT id FROM catalog.category WHERE code=$1`, categoryID).Scan(&id); err != nil {
			r.Errors = append(r.Errors, fmt.Sprintf("attributes row %d: unknown category_id %s", line+2, a.CategoryID))
			continue
		}
		name, _ := json.Marshal(map[string]string{"zh": a.Name.ZH, "en": a.Name.EN, "km": a.Name.KM})
		var values any
		if a.Type == "enum" {
			values, _ = json.Marshal(a.Values)
		}
		var existed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM catalog.category_attribute WHERE category_id=$1 AND key=$2)`, id, a.Key).Scan(&existed); err != nil {
			return r, err
		}
		_, err := tx.Exec(ctx, `INSERT INTO catalog.category_attribute (category_id,key,name,type,unit,required,is_sku_dimension,value_json,km_pending_review,sort) VALUES ($1,$2,$3::jsonb,$4,NULLIF($5,''),$6,$7,$8::text::jsonb,$9,0) ON CONFLICT (category_id,key) DO UPDATE SET name=excluded.name,type=excluded.type,unit=excluded.unit,required=excluded.required,is_sku_dimension=excluded.is_sku_dimension,value_json=excluded.value_json,km_pending_review=excluded.km_pending_review,updated_at=now()`, id, a.Key, string(name), a.Type, a.Unit, a.Required, a.SKU, values, a.KMPendingReview)
		if err != nil {
			return r, fmt.Errorf("attributes row %d: %w", line+2, err)
		}
		if existed {
			r.AttributesUpdated++
		} else {
			r.AttributesInserted++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return r, err
	}
	return r, nil
}

func readCategories(path string) ([]categoryRow, error) {
	records, err := readCSV(path, []string{"id", "parent_id", "level", "zh", "en", "km", "sort", "source"})
	if err != nil {
		return nil, err
	}
	rows := make([]categoryRow, 0, len(records))
	for line, record := range records {
		level, err := strconv.Atoi(strings.TrimSpace(record[2]))
		if err != nil {
			return nil, fmt.Errorf("categories row %d: invalid level", line+2)
		}
		sort, err := strconv.Atoi(strings.TrimSpace(record[6]))
		if err != nil {
			return nil, fmt.Errorf("categories row %d: invalid sort", line+2)
		}
		km, pending := stripReview(record[5])
		if strings.TrimSpace(record[0]) == "" || strings.TrimSpace(record[3]) == "" || strings.TrimSpace(record[4]) == "" || km == "" {
			return nil, fmt.Errorf("categories row %d: trilingual id/name required", line+2)
		}
		rows = append(rows, categoryRow{strings.TrimSpace(record[0]), strings.TrimSpace(record[1]), level, sort, localized{strings.TrimSpace(record[3]), strings.TrimSpace(record[4]), km}, pending})
	}
	return rows, nil
}

func readAttributes(path string) ([]attributeRow, error) {
	records, err := readCSV(path, []string{"category_id", "key", "zh", "en", "km", "type", "unit", "required", "sku_variant", "values_zh", "values_en", "values_km", "source"})
	if err != nil {
		return nil, err
	}
	rows := make([]attributeRow, 0, len(records))
	for line, record := range records {
		row, err := parseAttribute(record, line+2)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseAttribute(record []string, line int) (attributeRow, error) {
	if len(record) != 13 {
		return attributeRow{}, fmt.Errorf("attributes row %d: expected 13 fields", line)
	}
	required, err := strconv.ParseBool(strings.TrimSpace(record[7]))
	if err != nil {
		return attributeRow{}, fmt.Errorf("attributes row %d: invalid required", line)
	}
	sku, err := strconv.ParseBool(strings.TrimSpace(record[8]))
	if err != nil {
		return attributeRow{}, fmt.Errorf("attributes row %d: invalid sku_variant", line)
	}
	km, pending := stripReview(record[4])
	a := attributeRow{CategoryID: strings.TrimSpace(record[0]), Key: strings.TrimSpace(record[1]), Name: localized{strings.TrimSpace(record[2]), strings.TrimSpace(record[3]), km}, Type: strings.TrimSpace(record[5]), Unit: strings.TrimSpace(record[6]), Required: required, SKU: sku, KMPendingReview: pending}
	if a.CategoryID == "" || a.Key == "" || a.Name.ZH == "" || a.Name.EN == "" || a.Name.KM == "" {
		return a, fmt.Errorf("attributes row %d: category_id, key and trilingual name required", line)
	}
	if a.Type != "enum" && a.Type != "text" && a.Type != "number" {
		return a, fmt.Errorf("attributes row %d: unsupported type %q", line, a.Type)
	}
	if a.SKU && a.Type != "enum" {
		return a, fmt.Errorf("attributes row %d: sku_variant requires type enum", line)
	}
	if a.Type == "enum" {
		zh, en, kmValues := splitValues(record[9]), splitValues(record[10]), splitValues(record[11])
		if len(zh) == 0 || len(zh) != len(en) || len(zh) != len(kmValues) {
			return a, fmt.Errorf("attributes row %d: enum values must align across zh/en/km", line)
		}
		for i := range zh {
			cleanKM, review := stripReview(kmValues[i])
			a.KMPendingReview = a.KMPendingReview || review
			a.Values = append(a.Values, enumValue{fmt.Sprintf("value_%d", i+1), zh[i], en[i], cleanKM})
		}
	} else if len(splitValues(record[9])) != 0 || len(splitValues(record[10])) != 0 || len(splitValues(record[11])) != 0 {
		return a, fmt.Errorf("attributes row %d: values only allowed for enum", line)
	}
	return a, nil
}

func readCSV(path string, header []string) ([][]string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	got, err := reader.Read()
	if err != nil {
		return nil, err
	}
	if strings.Join(got, ",") != strings.Join(header, ",") {
		return nil, fmt.Errorf("%s: unexpected header", path)
	}
	var records [][]string
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return records, nil
		}
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
}
func stripReview(value string) (string, bool) {
	value = strings.TrimSpace(value)
	pending := strings.HasSuffix(value, reviewSuffix)
	return strings.TrimSpace(strings.TrimSuffix(value, reviewSuffix)), pending
}
func splitValues(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	raw := strings.Split(value, "|")
	out := make([]string, len(raw))
	for i := range raw {
		out[i] = strings.TrimSpace(raw[i])
	}
	return out
}
func categoryCode(sourceID string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(sourceID), "-", "_"))
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "catalog import error:", err); os.Exit(1) }
