package gateway

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

type BodyPolicy struct {
	DefaultBytes int64
	Overrides    map[string]int64
	ExemptPaths  []string
}

func LimitRequestBody(policy BodyPolicy) func(http.Handler) http.Handler {
	if policy.DefaultBytes <= 0 {
		policy.DefaultBytes = 64 << 10
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !writeMethod(r.Method) || matchesPrefix(policy.ExemptPaths, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnsupportedMedia))
				return
			}
			limit := policy.DefaultBytes
			if override := policy.Overrides[r.URL.Path]; override > 0 {
				limit = override
			}
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
			if err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeRequestTooLarge))
					return
				}
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			next.ServeHTTP(w, r)
		})
	}
}

func matchesPrefix(prefixes []string, path string) bool {
	for _, prefix := range prefixes {
		if path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/") {
			return true
		}
	}
	return false
}
