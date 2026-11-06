package kess

import (
	"crypto/hmac"
	"crypto/md5" // #nosec G501 -- KESS WebPay v1 currently documents MD5 signing.
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

const (
	SignMD5        = "MD5"
	SignHMACSHA256 = "HMAC-SHA256"
)

// SignScalars implements KESS's documented HMAC input: scalar values sorted by
// key, excluding sign and empty values, then the signing key is appended.
func SignScalars(fields map[string]string, key string) string {
	return signScalars(fields, key, SignHMACSHA256)
}

func signScalars(fields map[string]string, key, signType string) string {
	keys := make([]string, 0, len(fields))
	for name, value := range fields {
		if name != "sign" && value != "" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+1)
	for _, name := range keys {
		parts = append(parts, name+"="+fields[name])
	}
	parts = append(parts, "key="+key)
	input := []byte(strings.Join(parts, "&"))
	if signType == SignMD5 {
		sum := md5.Sum(input) // #nosec G401 -- required by the current KESS contract.
		return hex.EncodeToString(sum[:])
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(input)
	return hex.EncodeToString(mac.Sum(nil))
}

func VerifyScalars(fields map[string]string, key string) bool {
	return verifyScalars(fields, key, SignHMACSHA256)
}

func verifyScalars(fields map[string]string, key, signType string) bool {
	provided := fields["sign"]
	if provided == "" {
		return false
	}
	want := signScalars(fields, key, signType)
	return hmac.Equal([]byte(strings.ToLower(provided)), []byte(want))
}
