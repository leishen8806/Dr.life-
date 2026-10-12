package gateway

import (
	"net/http"
	"strings"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

type audience struct {
	public bool
	role   Role
	known  bool
}

var protectedAudiences = []struct {
	prefix string
	role   Role
}{
	{"/v1/buyer", RoleBuyer}, {"/v1/merchant", RoleMerchant}, {"/v1/driver", RoleDriver},
	{"/v1/wms", RoleWMS}, {"/v1/admin", RoleAdmin},
}

func Authenticate(auth Authenticator, publicWebhookPrefix string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := classifyPath(r.URL.Path, publicWebhookPrefix)
			if target.public || !target.known {
				next.ServeHTTP(w, r)
				return
			}
			if auth == nil {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
				return
			}
			identity, ok, err := auth.Authenticate(r)
			if err != nil || !ok || identity.AccountID == "" || !validRole(identity.Role) {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
				return
			}
			withIdentity(r, identity)
			next.ServeHTTP(w, r)
		})
	}
}

func Authorize(publicWebhookPrefix string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := classifyPath(r.URL.Path, publicWebhookPrefix)
			if !target.known || unsafePath(r) {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodePermissionDenied))
				return
			}
			if target.public {
				next.ServeHTTP(w, r)
				return
			}
			identity, ok := IdentityFromContext(r.Context())
			if !ok {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
				return
			}
			if identity.Role != target.role {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodePermissionDenied))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func classifyPath(path, publicWebhookPrefix string) audience {
	if path == "/healthz" || path == "/readyz" || path == "/v1/public" || strings.HasPrefix(path, "/v1/public/") {
		return audience{public: true, known: true}
	}
	if publicWebhookPrefix != "" && (path == publicWebhookPrefix || strings.HasPrefix(path, publicWebhookPrefix+"/")) {
		return audience{public: true, known: true}
	}
	for _, item := range protectedAudiences {
		if path == item.prefix || strings.HasPrefix(path, item.prefix+"/") {
			return audience{role: item.role, known: true}
		}
	}
	if strings.HasPrefix(path, "/v1/") || path == "/v1" {
		return audience{}
	}
	return audience{}
}

func unsafePath(r *http.Request) bool {
	path := r.URL.Path
	if strings.ToLower(path) != path || strings.Contains(path, "//") || strings.Contains(path, "\\") || r.URL.RawPath != "" {
		return true
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	rawPath := strings.SplitN(r.RequestURI, "?", 2)[0]
	return strings.Contains(rawPath, "%")
}

func validRole(role Role) bool {
	switch role {
	case RoleBuyer, RoleMerchant, RoleDriver, RoleWMS, RoleAdmin:
		return true
	default:
		return false
	}
}
