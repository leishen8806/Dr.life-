"""Validate the checked-in catalog taxonomy and SKU attribute templates."""
import csv
import sys
from collections import Counter
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
CATEGORIES = ROOT / "data" / "catalog" / "categories.csv"
ATTRIBUTES = ROOT / "data" / "catalog" / "sku_attributes.csv"


def read_csv(path, required):
    with path.open(encoding="utf-8-sig", newline="") as file:
        rows = list(csv.DictReader(file))
        missing = set(required) - set(rows[0] if rows else [])
    if missing:
        raise ValueError(f"{path}: missing columns {sorted(missing)}")
    return rows


def main():
    errors = []
    category_columns = ["id", "parent_id", "level", "zh", "en", "km", "sort", "source"]
    attribute_columns = ["category_id", "key", "zh", "en", "km", "type", "unit", "required", "sku_variant", "values_zh", "values_en", "values_km", "source"]
    try:
        categories = read_csv(CATEGORIES, category_columns)
        attributes = read_csv(ATTRIBUTES, attribute_columns)
    except (OSError, ValueError) as error:
        print(f"FAIL: {error}")
        return 1

    ids = [row["id"] for row in categories]
    duplicates = [value for value, count in Counter(ids).items() if count > 1]
    if duplicates:
        errors.append(f"duplicate category ids: {duplicates}")
    by_id = {row["id"]: row for row in categories}
    for row in categories:
        if not all(row[field].strip() for field in ("id", "zh", "en", "km", "source")):
            errors.append(f"category {row['id']}: missing required text")
        try:
            level = int(row["level"])
            int(row["sort"])
        except ValueError:
            errors.append(f"category {row['id']}: level/sort must be integers")
            continue
        parent = row["parent_id"].strip()
        if parent:
            if parent not in by_id:
                errors.append(f"category {row['id']}: unknown parent {parent}")
            elif level != int(by_id[parent]["level"]) + 1:
                errors.append(f"category {row['id']}: level does not follow parent")
        elif level != 1:
            errors.append(f"category {row['id']}: root must be level 1")

    parents = {row["parent_id"] for row in categories if row["parent_id"]}
    leaves = set(by_id) - parents
    count_by_category = Counter()
    attribute_ids = Counter()
    for row in attributes:
        category_id = row["category_id"]
        count_by_category[category_id] += 1
        attribute_ids[(category_id, row["key"])] += 1
        if category_id not in leaves:
            errors.append(f"attribute {row['key']}: category must be a leaf")
        if not all(row[field].strip() for field in ("category_id", "key", "zh", "en", "km", "type", "source")):
            errors.append(f"attribute {category_id}/{row['key']}: missing required text")
        if row["type"] not in {"enum", "text", "number"}:
            errors.append(f"attribute {category_id}/{row['key']}: invalid type")
        if row["required"] not in {"true", "false"} or row["sku_variant"] not in {"true", "false"}:
            errors.append(f"attribute {category_id}/{row['key']}: flags must be true/false")
        if row["type"] == "enum" and not all(row[field].strip() for field in ("values_zh", "values_en", "values_km")):
            errors.append(f"attribute {category_id}/{row['key']}: enum values required in all languages")
        if row["type"] == "enum":
            lengths = {len(row[field].split("|")) for field in ("values_zh", "values_en", "values_km")}
            if len(lengths) != 1:
                errors.append(f"attribute {category_id}/{row['key']}: enum value counts differ by language")
    duplicates = [f"{category_id}/{key}" for (category_id, key), count in attribute_ids.items() if count > 1]
    if duplicates:
        errors.append(f"duplicate attribute keys: {duplicates}")
    for leaf in sorted(leaves):
        if count_by_category[leaf] < 2:
            errors.append(f"leaf {leaf}: needs at least 2 attributes")

    if errors:
        print("FAIL")
        print("\n".join(f"- {error}" for error in errors))
        return 1
    print(f"PASS: {len(categories)} categories; {len(leaves)} leaves; {len(attributes)} attribute templates")
    return 0


if __name__ == "__main__":
    sys.exit(main())
