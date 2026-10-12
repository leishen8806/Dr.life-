package telegraminitdata

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testToken = "synthetic-test-token"

func TestValidate(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	validator := NewValidator()
	validator.Now = func() time.Time { return now }
	fields := url.Values{
		"auth_date": {strconv.FormatInt(now.Unix(), 10)},
		"query_id":  {"query-1"},
		"signature": {"third-party-signature-is-part-of-token-check"},
		"user":      {`{"id":123456,"first_name":"not logged"}`},
	}
	raw := sign(fields, testToken)

	got, err := validator.Validate(raw, testToken)
	if err != nil || got.User.ID != 123456 || !got.AuthDate.Equal(now) {
		t.Fatalf("Validate() = %+v, %v", got, err)
	}

	for _, field := range []string{"user", "auth_date", "query_id"} {
		t.Run("tampered_"+field, func(t *testing.T) {
			values, _ := url.ParseQuery(raw)
			values.Set(field, values.Get(field)+"x")
			if _, err := validator.Validate(values.Encode(), testToken); err == nil {
				t.Fatal("tampered field was accepted")
			}
		})
	}
}

func TestValidateTimeAndBoundaries(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	validator := NewValidator()
	validator.Now = func() time.Time { return now }
	tests := []struct {
		name string
		raw  string
	}{
		{"expired", signedAt(now.Add(-time.Hour-time.Second), testToken)},
		{"future", signedAt(now.Add(time.Minute+time.Second), testToken)},
		{"too long", strings.Repeat("x", MaxInitDataBytes+1)},
		{"malformed", "%zz"},
		{"duplicate", signedAt(now, testToken) + "&user=%7B%22id%22%3A1%7D"},
		{"bad hash", "auth_date=1&user=%7B%22id%22%3A1%7D&hash=00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := validator.Validate(tt.raw, testToken); err == nil {
				t.Fatal("invalid data was accepted")
			}
		})
	}
}

func FuzzValidateDoesNotPanic(f *testing.F) {
	f.Add("")
	f.Add("auth_date=broken&user=%7B&hash=xyz")
	f.Add(strings.Repeat("a", MaxInitDataBytes+1))
	validator := NewValidator()
	f.Fuzz(func(t *testing.T, raw string) {
		_, _ = validator.Validate(raw, testToken)
	})
}

func signedAt(at time.Time, token string) string {
	return sign(url.Values{
		"auth_date": {strconv.FormatInt(at.Unix(), 10)},
		"query_id":  {"query-1"},
		"user":      {`{"id":123456}`},
	}, token)
}

func sign(fields url.Values, token string) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+fields.Get(key))
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secret.Write([]byte(token))
	digest := hmac.New(sha256.New, secret.Sum(nil))
	_, _ = digest.Write([]byte(strings.Join(lines, "\n")))
	copy := url.Values{}
	for key, values := range fields {
		copy[key] = append([]string(nil), values...)
	}
	copy.Set("hash", hex.EncodeToString(digest.Sum(nil)))
	return copy.Encode()
}
