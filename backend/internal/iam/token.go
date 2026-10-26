package iam

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
)

var ErrInvalidToken = errors.New("iam: invalid token")

type SessionChecker interface {
	Active(context.Context, string) (bool, error)
}

type PassSessionChecker struct{}

// Active deliberately accepts every signed session until A-03 adds refresh
// rotation and revocation checks.
func (PassSessionChecker) Active(context.Context, string) (bool, error) { return true, nil }

type AccessClaims struct {
	Role string `json:"role"`
	SID  string `json:"sid"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	Keys      secrets.KeyProvider
	AccessTTL time.Duration
	Now       func() time.Time
	Checker   SessionChecker
}

func (m TokenManager) IssueAccess(ctx context.Context, accountPublicID, role, sessionPublicID string) (string, error) {
	if m.Keys == nil || m.AccessTTL <= 0 || accountPublicID == "" || sessionPublicID == "" || !validRole(role) {
		return "", ErrInvalidToken
	}
	now := time.Now()
	if m.Now != nil {
		now = m.Now()
	}
	kid, key, err := m.Keys.ActiveKey(ctx)
	if err != nil {
		return "", ErrInvalidToken
	}
	defer wipe(key)
	jti, err := randomToken(16)
	if err != nil {
		return "", err
	}
	claims := AccessClaims{Role: role, SID: sessionPublicID, RegisteredClaims: jwt.RegisteredClaims{
		Subject: accountPublicID, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(m.AccessTTL)), ID: jti,
	}}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = kid
	value, err := token.SignedString(key)
	if err != nil {
		return "", ErrInvalidToken
	}
	return value, nil
}

func (m TokenManager) ParseAccess(ctx context.Context, raw string) (gateway.Identity, string, error) {
	if m.Keys == nil || raw == "" {
		return gateway.Identity{}, "", ErrInvalidToken
	}
	claims := new(AccessClaims)
	now := time.Now
	if m.Now != nil {
		now = m.Now
	}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, ErrInvalidToken
		}
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, ErrInvalidToken
		}
		key, keyErr := m.Keys.KeyByID(ctx, kid)
		if keyErr != nil {
			return nil, ErrInvalidToken
		}
		return key, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired(), jwt.WithTimeFunc(now))
	if err != nil || !parsed.Valid || claims.Subject == "" || claims.SID == "" || claims.ID == "" || !validRole(claims.Role) {
		return gateway.Identity{}, "", ErrInvalidToken
	}
	checker := m.Checker
	if checker == nil {
		checker = PassSessionChecker{}
	}
	active, err := checker.Active(ctx, claims.SID)
	if err != nil || !active {
		return gateway.Identity{}, "", ErrInvalidToken
	}
	return gateway.Identity{AccountID: claims.Subject, Role: gateway.Role(claims.Role)}, claims.SID, nil
}

type Authenticator struct{ Tokens TokenManager }

func (a Authenticator) Authenticate(r *http.Request) (gateway.Identity, bool, error) {
	header := r.Header.Get("Authorization")
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return gateway.Identity{}, false, nil
	}
	identity, _, err := a.Tokens.ParseAccess(r.Context(), strings.TrimSpace(parts[1]))
	if err != nil {
		return gateway.Identity{}, false, nil
	}
	return identity, true, nil
}

func NewRefreshToken() (plaintext string, hash []byte, err error) {
	plaintext, err = randomToken(32)
	if err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256([]byte(plaintext))
	return plaintext, digest[:], nil
}

func randomToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("iam: random source failed")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func validRole(role string) bool {
	return role == string(gateway.RoleBuyer) || role == string(gateway.RoleMerchant) || role == string(gateway.RoleDriver)
}

func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
