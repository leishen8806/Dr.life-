package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const MaxDetailBytes = 16 << 10

var sensitiveKeyParts = [...]string{"password", "token", "secret", "key", "phone", "authorization", "init_data", "initdata"}

func sanitizeDetail(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || len(raw) > MaxDetailBytes {
		return nil, fmt.Errorf("audit detail must be 1-%d bytes", MaxDetailBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode audit detail: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, err
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, errors.New("audit detail must be a JSON object")
	}
	redact(value)
	var out bytes.Buffer
	if err := writeCanonical(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func ensureEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("audit detail must contain one JSON value")
	}
	return nil
}

func redact(value any) {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			if sensitiveKey(key) {
				current[key] = "[REDACTED]"
				continue
			}
			redact(child)
		}
	case []any:
		for _, child := range current {
			redact(child)
		}
	}
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, part := range sensitiveKeyParts {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

func writeCanonical(out *bytes.Buffer, value any) error {
	switch current := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(current))
	case string:
		encoded, _ := json.Marshal(current)
		out.Write(encoded)
	case json.Number:
		number, err := normalizeNumber(current.String())
		if err != nil {
			return err
		}
		out.WriteString(number)
	case []any:
		out.WriteByte('[')
		for i, child := range current {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeCanonical(out, child); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(current))
		for key := range current {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			encoded, _ := json.Marshal(key)
			out.Write(encoded)
			out.WriteByte(':')
			if err := writeCanonical(out, current[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("unsupported JSON type %T", value)
	}
	return nil
}

func normalizeNumber(input string) (string, error) {
	sign := ""
	if strings.HasPrefix(input, "-") {
		sign, input = "-", input[1:]
	}
	parts := strings.FieldsFunc(input, func(r rune) bool { return r == 'e' || r == 'E' })
	if len(parts) > 2 || len(parts) == 0 {
		return "", fmt.Errorf("invalid JSON number")
	}
	exponent := 0
	var err error
	if len(parts) == 2 {
		exponent, err = strconv.Atoi(parts[1])
		if err != nil || exponent > 10000 || exponent < -10000 {
			return "", fmt.Errorf("invalid JSON number")
		}
	}
	mantissa := strings.Split(parts[0], ".")
	if len(mantissa) > 2 || mantissa[0] == "" {
		return "", fmt.Errorf("invalid JSON number")
	}
	fraction := ""
	if len(mantissa) == 2 {
		fraction = mantissa[1]
	}
	digits := mantissa[0] + fraction
	if strings.Trim(digits, "0123456789") != "" || digits == "" {
		return "", fmt.Errorf("invalid JSON number")
	}
	decimal := len(mantissa[0]) + exponent
	leading := len(digits) - len(strings.TrimLeft(digits, "0"))
	digits = strings.TrimLeft(digits, "0")
	decimal -= leading
	if digits == "" {
		return "0", nil
	}
	var normalized string
	switch {
	case decimal <= 0:
		normalized = "0." + strings.Repeat("0", -decimal) + digits
	case decimal >= len(digits):
		normalized = digits + strings.Repeat("0", decimal-len(digits))
	default:
		normalized = digits[:decimal] + "." + digits[decimal:]
	}
	if strings.Contains(normalized, ".") {
		normalized = strings.TrimRight(normalized, "0")
		normalized = strings.TrimSuffix(normalized, ".")
	}
	return sign + normalized, nil
}
