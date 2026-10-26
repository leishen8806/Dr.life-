package telegram

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

func TestValidatePoCVectorAndTampering(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	v := NewValidator(24 * time.Hour)
	v.Now = func() time.Time { return now }
	fields := url.Values{
		"auth_date": {strconv.FormatInt(now.Unix(), 10)}, "query_id": {"query-1"},
		"signature": {"kept-in-data-check-string"}, "start_param": {"campaign_1"},
		"user": {`{"id":123456,"first_name":"Ada","username":"ada","language_code":"en","allows_write_to_pm":true}`},
	}
	raw := sign(fields, testToken)
	got, err := v.Validate(raw, []byte(testToken))
	if err != nil || got.User.ID != 123456 || got.StartParam != "campaign_1" || !got.User.AllowsWriteToPM {
		t.Fatalf("Validate() = %+v, %v", got, err)
	}
	for _, field := range []string{"user", "auth_date", "query_id", "start_param", "signature"} {
		t.Run("tampered_"+field, func(t *testing.T) {
			values, _ := url.ParseQuery(raw)
			values.Set(field, values.Get(field)+"x")
			if _, err := v.Validate(values.Encode(), []byte(testToken)); err == nil {
				t.Fatal("tampered field accepted")
			}
		})
	}
}

func TestValidateBoundaries(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	v := NewValidator(time.Hour)
	v.Now = func() time.Time { return now }
	tests := []string{
		signedAt(now.Add(-time.Hour-time.Second), testToken),
		signedAt(now.Add(time.Minute+time.Second), testToken),
		strings.Repeat("x", MaxInitDataBytes+1), "%zz",
		signedAt(now, testToken) + "&user=%7B%22id%22%3A1%7D",
		"auth_date=1&user=%7B%22id%22%3A1%7D&hash=00", "",
	}
	for i, raw := range tests {
		if _, err := v.Validate(raw, []byte(testToken)); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func FuzzValidateDoesNotPanic(f *testing.F) {
	f.Add("")
	f.Add("auth_date=broken&user=%7B&hash=xyz")
	f.Add(strings.Repeat("a", MaxInitDataBytes+1))
	v := NewValidator(time.Hour)
	f.Fuzz(func(t *testing.T, raw string) { _, _ = v.Validate(raw, []byte(testToken)) })
}

func signedAt(at time.Time, token string) string {
	return sign(url.Values{"auth_date": {strconv.FormatInt(at.Unix(), 10)}, "query_id": {"query-1"}, "user": {`{"id":123456}`}}, token)
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
	copyValues := url.Values{}
	for key, values := range fields {
		copyValues[key] = append([]string(nil), values...)
	}
	copyValues.Set("hash", hex.EncodeToString(digest.Sum(nil)))
	return copyValues.Encode()
}
