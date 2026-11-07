package kess

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var ErrInvalidAmount = errors.New("invalid KESS amount or currency")

// FormatAmount converts USD e4 to KESS's two-decimal amount without rounding.
func FormatAmount(amountE4 int64, currency string) (string, error) {
	if amountE4 <= 0 || amountE4%100 != 0 || currency != "USD" {
		return "", ErrInvalidAmount
	}
	cents := amountE4 / 100
	return fmt.Sprintf("%d.%02d", cents/100, cents%100), nil
}

func ParseAmount(raw, currency string) (int64, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.HasPrefix(raw, "+") || strings.HasPrefix(raw, "-") {
		return 0, ErrInvalidAmount
	}
	switch currency {
	case "USD":
		parts := strings.Split(raw, ".")
		if len(parts) != 2 || len(parts[1]) != 2 || parts[0] == "" || !digits(parts[0]) || !digits(parts[1]) {
			return 0, ErrInvalidAmount
		}
		whole, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, ErrInvalidAmount
		}
		fraction, _ := strconv.ParseInt(parts[1], 10, 64)
		if whole > (math.MaxInt64-fraction*100)/10000 {
			return 0, ErrInvalidAmount
		}
		amountE4 := whole*10000 + fraction*100
		if amountE4 <= 0 {
			return 0, ErrInvalidAmount
		}
		return amountE4, nil
	default:
		return 0, ErrInvalidAmount
	}
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
