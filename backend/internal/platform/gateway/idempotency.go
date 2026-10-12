package gateway

import (
	"context"
	"net/http"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

type idempotencyKey struct{}

func IdempotencyKeyFromContext(ctx context.Context) string {
	value, _ := ctx.Value(idempotencyKey{}).(string)
	return value
}

func RequireIdempotency(exempt func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !writeMethod(r.Method) || exempt != nil && exempt(r) {
				next.ServeHTTP(w, r)
				return
			}
			key := r.Header.Get("Idempotency-Key")
			if !validOpaqueID(key) {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
				return
			}
			*r = *r.WithContext(context.WithValue(r.Context(), idempotencyKey{}, key))
			next.ServeHTTP(w, r)
		})
	}
}

func writeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func validOpaqueID(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}
