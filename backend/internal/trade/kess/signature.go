package kess

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// SignScalars implements KESS's documented HMAC input: scalar values sorted by
// key, excluding sign and empty values, then the signing key is appended.
func SignScalars(fields map[string]string, key string) string {
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
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(strings.Join(parts, "&")))
	return hex.EncodeToString(mac.Sum(nil))
}

func VerifyScalars(fields map[string]string, key string) bool {
	provided := fields["sign"]
	if provided == "" {
		return false
	}
	want := SignScalars(fields, key)
	return hmac.Equal([]byte(strings.ToLower(provided)), []byte(want))
}
