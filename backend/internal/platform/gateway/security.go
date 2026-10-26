package gateway

import (
	"net/http"
	"strings"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

const hstsValue = "max-age=31536000; includeSubDomains"

func SecurityHeaders(appEnv string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			h.Set("Cross-Origin-Resource-Policy", "same-site")
			h.Set("Cache-Control", "no-store")
			h.Del("Server")
			h.Del("X-Powered-By")
			if appEnv != "dev" {
				h.Set("Strict-Transport-Security", hstsValue)
			}
			next.ServeHTTP(fingerprintWriter{ResponseWriter: w}, r)
		})
	}
}

type fingerprintWriter struct{ http.ResponseWriter }

func (w fingerprintWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w fingerprintWriter) Flush() {
	w.Header().Del("Server")
	w.Header().Del("X-Powered-By")
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w fingerprintWriter) WriteHeader(status int) {
	w.Header().Del("Server")
	w.Header().Del("X-Powered-By")
	w.ResponseWriter.WriteHeader(status)
}

func (w fingerprintWriter) Write(body []byte) (int, error) {
	w.Header().Del("Server")
	w.Header().Del("X-Powered-By")
	return w.ResponseWriter.Write(body)
}

type OriginGroups struct {
	Buyer, Merchant, Driver, WMS, Admin, Public []string
	CredentialPaths                             []string
}

func DefaultOriginGroups(appEnv string) OriginGroups {
	groups := OriginGroups{
		Buyer:    []string{"https://miniapp.settlecore.org"},
		Merchant: []string{"https://shop.settlecore.org"},
		Driver:   []string{"https://driver.settlecore.org"},
		WMS:      []string{"https://wms.settlecore.org"},
		Admin:    []string{"https://admin.settlecore.org"},
	}
	groups.Public = append(groups.Public, groups.Buyer...)
	groups.Public = append(groups.Public, groups.Merchant...)
	groups.Public = append(groups.Public, groups.Driver...)
	groups.Public = append(groups.Public, groups.WMS...)
	groups.Public = append(groups.Public, groups.Admin...)
	groups.Public = append(groups.Public, "https://settlecore.org")
	if appEnv == "dev" {
		for _, origins := range []*[]string{&groups.Buyer, &groups.Merchant, &groups.Driver, &groups.WMS, &groups.Admin, &groups.Public} {
			*origins = append(*origins, "http://localhost:5173")
		}
	}
	return groups
}

func GroupedCORS(groups OriginGroups) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Origin")
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !containsOrigin(originsForPath(groups, r.URL.Path), origin) {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodePermissionDenied))
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, Idempotent-Replayed")
			if matchesPath(groups.CredentialPaths, r.URL.Path) {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Set("Access-Control-Allow-Methods", allowedMethods)
				w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
				w.Header().Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func originsForPath(groups OriginGroups, path string) []string {
	switch {
	case path == "/v1/buyer" || strings.HasPrefix(path, "/v1/buyer/"):
		return groups.Buyer
	case path == "/v1/merchant" || strings.HasPrefix(path, "/v1/merchant/"):
		return groups.Merchant
	case path == "/v1/driver" || strings.HasPrefix(path, "/v1/driver/"):
		return groups.Driver
	case path == "/v1/wms" || strings.HasPrefix(path, "/v1/wms/"):
		return groups.WMS
	case path == "/v1/admin" || strings.HasPrefix(path, "/v1/admin/"):
		return groups.Admin
	case path == "/v1/public" || strings.HasPrefix(path, "/v1/public/"):
		return groups.Public
	default:
		return nil
	}
}

func containsOrigin(origins []string, origin string) bool {
	for _, allowed := range origins {
		if allowed == origin {
			return true
		}
	}
	return false
}

func matchesPath(paths []string, path string) bool {
	for _, candidate := range paths {
		if path == candidate {
			return true
		}
	}
	return false
}
