package catalog

import (
	"fmt"
	"strings"
)

// languages 是上架所需的三语顺序；报错按此顺序定位第一个缺失的语言。
var languages = []string{LangZH, LangEN, LangKM}

func invalid(field, reason string) error {
	return &ValidationError{Field: field, Reason: reason}
}

// ValidateLocalized 要求三语齐全且非空白；缺任意一语即失败。
func ValidateLocalized(field string, text LocalizedText) error {
	for _, lang := range languages {
		if strings.TrimSpace(text[lang]) == "" {
			return invalid(field+"."+lang, "text is required in all three languages")
		}
	}
	return nil
}

// ValidateTiers 实现验收标准②：至少 2 档、起订量唯一、起订量为正、单价非负。
func ValidateTiers(tiers []Tier) error {
	if len(tiers) < 2 {
		return invalid("tiers", "at least two price tiers are required")
	}
	seen := make(map[string]struct{}, len(tiers))
	for i, tier := range tiers {
		quantity, err := ParseDecimal(tier.MinQty, 3)
		if err != nil || quantity == "0" {
			return invalid(fmt.Sprintf("tiers[%d].min_qty", i), "minimum quantity must be a positive decimal with at most three decimals")
		}
		if _, duplicate := seen[quantity]; duplicate {
			return invalid(fmt.Sprintf("tiers[%d].min_qty", i), "minimum quantity must be unique per product")
		}
		seen[quantity] = struct{}{}
		if tier.UnitPriceE4 < 0 {
			return invalid(fmt.Sprintf("tiers[%d].unit_price_e4", i), "unit price must not be negative")
		}
	}
	return nil
}

// ValidateProductForPublish 是上架闸门（验收标准①②）：三语齐全、阶梯价合规、且尚未上架。
func ValidateProductForPublish(product Product, tiers []Tier) error {
	if product.Status == StatusOn {
		return invalid("status", "product is already on sale")
	}
	if err := ValidateLocalized("name", product.Name); err != nil {
		return err
	}
	return ValidateTiers(tiers)
}

// ParseDecimal 把十进制字符串规范化（去掉多余前后零），最多保留 scale 位小数。
// 拒绝负号与浮点/科学计数法写法，确保金额与数量永不经由 float64。
func ParseDecimal(raw string, scale int) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", invalid("decimal", "value is required")
	}
	integerPart, fractionPart, hasFraction := strings.Cut(value, ".")
	if !digits(integerPart) || hasFraction && (!digits(fractionPart) || len(fractionPart) > scale) {
		return "", invalid("decimal", "value must be a plain non-negative decimal within the allowed scale")
	}
	normalisedInteger := strings.TrimLeft(integerPart, "0")
	if normalisedInteger == "" {
		normalisedInteger = "0"
	}
	normalisedFraction := strings.TrimRight(fractionPart, "0")
	if normalisedFraction == "" {
		return normalisedInteger, nil
	}
	return normalisedInteger + "." + normalisedFraction, nil
}

func digits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// validateLeadDays 对应 catalog.product 的 CHECK (lead_days IS NULL OR lead_days >= 0)。
// 在服务端拦下来，非法取值才会映射成 400，而不是数据库 check_violation 的 500。
func validateLeadDays(days *int) error {
	if days != nil && *days < 0 {
		return invalid("lead_days", "lead days must not be negative")
	}
	return nil
}

// validateVolume 对应 numeric(12,4) CHECK (vol_m3 IS NULL OR vol_m3 >= 0)：
// 空值合法（列可空），非空必须是 4 位以内小数的非负十进制，整数位不超过 8 位。
func validateVolume(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	normalised, err := ParseDecimal(raw, 4)
	if err != nil {
		return invalid("vol_m3", "volume must be a plain non-negative decimal with at most four decimals")
	}
	if integerPart, _, _ := strings.Cut(normalised, "."); len(integerPart) > 8 {
		return invalid("vol_m3", "volume is out of range")
	}
	return nil
}
