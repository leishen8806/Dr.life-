package gateway

import (
	"context"
	"net/http"
)

type Role string

const (
	RoleBuyer    Role = "buyer"
	RoleMerchant Role = "merchant"
	RoleDriver   Role = "driver"
	RoleWMS      Role = "wms"
	RoleAdmin    Role = "admin"
)

type Identity struct {
	AccountID   string
	Role        Role
	SessionID   string
	Permissions map[string]string
}

func (i Identity) HasPermission(module, level string) bool {
	value := i.Permissions[module]
	return value == level || value == "edit"
}

type Authenticator interface {
	Authenticate(*http.Request) (Identity, bool, error)
}

type identityKey struct{}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityKey{}).(Identity)
	return identity, ok
}

func withIdentity(r *http.Request, identity Identity) {
	*r = *r.WithContext(context.WithValue(r.Context(), identityKey{}, identity))
}

func RoleFromContext(ctx context.Context) string {
	identity, _ := IdentityFromContext(ctx)
	return string(identity.Role)
}
