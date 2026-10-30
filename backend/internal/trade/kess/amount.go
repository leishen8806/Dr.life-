package kess

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var ErrInvalidAmount = errors.New("invalid KESS amount or currency")

// FormatAmount converts an integer minor amount to KESS's textual amount. It
// never exchanges currencies: USD is cents and KHR is already an integer.
func FormatAmount(amount int64, currency string) (string, error) {
	if amount <= 0 {
		return "", ErrInvalidAmount
	}
	switch currency {
	case "USD":
		return fmt.Sprintf("%d.%02d", amount/100, amount%100), nil
	case "KHR":
		return strconv.FormatInt(amount, 10), nil
	default:
		return "", ErrInvalidAmount
	}
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
		if err != nil || whole > (1<<63-1)/100 {
			return 0, ErrInvalidAmount
		}
		fraction, _ := strconv.ParseInt(parts[1], 10, 64)
		amount := whole*100 + fraction
		if amount <= 0 {
			return 0, ErrInvalidAmount
		}
		return amount, nil
	case "KHR":
		if !digits(raw) {
			return 0, ErrInvalidAmount
		}
		amount, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || amount <= 0 {
			return 0, ErrInvalidAmount
		}
		return amount, nil
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
