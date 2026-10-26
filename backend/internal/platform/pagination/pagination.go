// Package pagination validates shared list-query parameters.
package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	DefaultPageNumber = 1
	DefaultPageSize   = 20
	MaxPageSize       = 100
)

// Page is a validated offset page request.
type Page struct {
	Number int
	Size   int
}

// ParsePage parses page and page_size, applying defaults and rejecting
// out-of-range or non-numeric values.
func ParsePage(values url.Values) (Page, error) {
	page := Page{Number: DefaultPageNumber, Size: DefaultPageSize}
	if raw, present, err := singleValue(values, "page"); err != nil {
		return Page{}, err
	} else if present {
		page.Number, err = strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || page.Number < 1 {
			return Page{}, fmt.Errorf("page must be an integer greater than or equal to 1")
		}
	}
	if raw, present, err := singleValue(values, "page_size"); err != nil {
		return Page{}, err
	} else if present {
		page.Size, err = strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || page.Size < 1 || page.Size > MaxPageSize {
			return Page{}, fmt.Errorf("page_size must be between 1 and %d", MaxPageSize)
		}
	}
	return page, nil
}

// Sort is one validated sort term.
type Sort struct {
	Field string
	Desc  bool
}

// ParseSort parses a comma-separated list such as "name,-created_at" and
// rejects empty, duplicate, or non-whitelisted fields.
func ParseSort(raw string, allowed ...string) ([]Sort, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	permitted := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		permitted[field] = struct{}{}
	}
	seen := make(map[string]struct{}, len(allowed))
	result := make([]Sort, 0, strings.Count(raw, ",")+1)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return nil, fmt.Errorf("sort contains an empty field")
		}
		desc := strings.HasPrefix(entry, "-")
		field := strings.TrimSpace(strings.TrimPrefix(entry, "-"))
		if field == "" {
			return nil, fmt.Errorf("sort contains an empty field")
		}
		if _, ok := permitted[field]; !ok {
			return nil, fmt.Errorf("unsupported sort field %q", field)
		}
		if _, ok := seen[field]; ok {
			return nil, fmt.Errorf("duplicate sort field %q", field)
		}
		seen[field] = struct{}{}
		result = append(result, Sort{Field: field, Desc: desc})
	}
	return result, nil
}

// ParseFilter parses filter[name]=value entries and rejects unknown or
// malformed filter names. Non-filter query parameters are ignored.
func ParseFilter(values url.Values, allowed ...string) (map[string]string, error) {
	permitted := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		permitted[field] = struct{}{}
	}
	result := make(map[string]string)
	for key, entries := range values {
		if !strings.HasPrefix(key, "filter[") {
			continue
		}
		if !strings.HasSuffix(key, "]") {
			return nil, fmt.Errorf("malformed filter name")
		}
		field := strings.TrimSuffix(strings.TrimPrefix(key, "filter["), "]")
		if field == "" || strings.ContainsAny(field, "[]") {
			return nil, fmt.Errorf("malformed filter name")
		}
		if _, ok := permitted[field]; !ok {
			return nil, fmt.Errorf("unsupported filter field %q", field)
		}
		if len(entries) != 1 {
			return nil, fmt.Errorf("filter %q must have exactly one value", field)
		}
		if _, exists := result[field]; exists {
			return nil, fmt.Errorf("duplicate filter field %q", field)
		}
		result[field] = entries[0]
	}
	return result, nil
}

// EncodeCursor encodes a keyset cursor. The value is URL-safe and opaque to
// clients, but it is not secret and must not contain sensitive data.
func EncodeCursor(values ...string) string {
	if len(values) == 0 {
		return ""
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

// DecodeCursor decodes a cursor produced by EncodeCursor and rejects empty or
// malformed values.
func DecodeCursor(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("cursor is empty")
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("cursor is not valid base64")
	}
	var values []string
	if err := json.Unmarshal(payload, &values); err != nil {
		return nil, errors.New("cursor is not a valid keyset")
	}
	if len(values) == 0 {
		return nil, errors.New("cursor is empty")
	}
	return values, nil
}

func singleValue(values url.Values, name string) (string, bool, error) {
	entries, present := values[name]
	if !present {
		return "", false, nil
	}
	if len(entries) != 1 {
		return "", false, fmt.Errorf("%s must be specified exactly once", name)
	}
	return entries[0], true, nil
}
