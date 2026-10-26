package gateway

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

type AdminAccess struct {
	AppEnv   string
	Resolver ClientIPResolver
	Allowed  []*net.IPNet
}

func NewAdminAccess(appEnv string, resolver ClientIPResolver, cidrs []string) (AdminAccess, error) {
	access := AdminAccess{AppEnv: appEnv, Resolver: resolver}
	for _, value := range cidrs {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return AdminAccess{}, errors.New("invalid admin CIDR")
		}
		access.Allowed = append(access.Allowed, network)
	}
	return access, nil
}

func (a AdminAccess) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !adminPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if len(a.Allowed) == 0 && a.AppEnv == "dev" {
			next.ServeHTTP(w, r)
			return
		}
		ip := resolveClientIP(a.Resolver, r)
		for _, network := range a.Allowed {
			if network.Contains(ip) {
				next.ServeHTTP(w, r)
				return
			}
		}
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodePermissionDenied))
	})
}

func adminPath(path string) bool {
	return path == "/v1/admin" || strings.HasPrefix(path, "/v1/admin/") || path == "/v1/wms" || strings.HasPrefix(path, "/v1/wms/")
}
