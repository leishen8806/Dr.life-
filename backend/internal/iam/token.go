package iam

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
)

var (
	ErrInvalidToken            = errors.New("iam: invalid token")
	ErrSessionStoreUnavailable = errors.New("iam: session store unavailable")
)

type SessionChecker interface {
	Active(context.Context, string, string) (bool, error)
}

type PassSessionChecker struct{}

// Active is only for isolated token tests; production injects DBSessionChecker.
func (PassSessionChecker) Active(context.Context, string, string) (bool, error) { return true, nil }

type AccessClaims struct {
	Role string `json:"role"`
	SID  string `json:"sid"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	Keys              secrets.KeyProvider
	AccessTTL         time.Duration
	Issuer            string
	LegacyAcceptUntil time.Time
	Now               func() time.Time
	Checker           SessionChecker
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
	return m.signAccess(accountPublicID, role, sessionPublicID, kid, key, now, jti)
}

func (m TokenManager) IssueRotatedAccess(ctx context.Context, accountPublicID, role, sessionPublicID, kid string, rotation int64, issuedAt time.Time) (string, error) {
	if m.Keys == nil || m.AccessTTL <= 0 || accountPublicID == "" || sessionPublicID == "" || kid == "" || !validRole(role) {
		return "", ErrInvalidToken
	}
	key, err := m.Keys.KeyByID(ctx, kid)
	if err != nil {
		return "", ErrInvalidToken
	}
	defer wipe(key)
	jti := derivedToken(key, "access", sessionPublicID, rotation)
	return m.signAccess(accountPublicID, role, sessionPublicID, kid, key, issuedAt, jti)
}

func (m TokenManager) signAccess(accountPublicID, role, sessionPublicID, kid string, key []byte, issuedAt time.Time, jti string) (string, error) {
	issuer := m.Issuer
	if issuer == "" {
		issuer = "drlife"
	}
	claims := AccessClaims{Role: role, SID: sessionPublicID, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: issuer, Subject: accountPublicID, Audience: jwt.ClaimStrings{audienceForRole(role)},
		IssuedAt: jwt.NewNumericDate(issuedAt), ExpiresAt: jwt.NewNumericDate(issuedAt.Add(m.AccessTTL)), ID: jti,
	}}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = kid
	token.Header["typ"] = "at+jwt"
	value, err := token.SignedString(key)
	if err != nil {
		return "", ErrInvalidToken
	}
	return value, nil
}

func (m TokenManager) ParseAccess(ctx context.Context, raw string) (gateway.Identity, string, error) {
	return m.parseAccess(ctx, raw, "", false)
}

func (m TokenManager) parseAccess(ctx context.Context, raw, expectedAudience string, allowUnavailable bool) (gateway.Identity, string, error) {
	if m.Keys == nil || raw == "" {
		return gateway.Identity{}, "", ErrInvalidToken
	}
	claims := new(AccessClaims)
	now := time.Now
	if m.Now != nil {
		now = m.Now
	}
	var tokenType string
	parsed, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, ErrInvalidToken
		}
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, ErrInvalidToken
		}
		tokenType, _ = token.Header["typ"].(string)
		key, keyErr := m.Keys.KeyByID(ctx, kid)
		if keyErr != nil {
			return nil, ErrInvalidToken
		}
		return key, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired(), jwt.WithTimeFunc(now))
	if err != nil || !parsed.Valid || claims.Subject == "" || claims.SID == "" || claims.ID == "" || !validRole(claims.Role) {
		return gateway.Identity{}, "", ErrInvalidToken
	}
	nowValue := now()
	legacy := (tokenType == "" || tokenType == "JWT") && claims.Issuer == "" && len(claims.Audience) == 0
	if legacy {
		if m.LegacyAcceptUntil.IsZero() || !nowValue.Before(m.LegacyAcceptUntil) {
			return gateway.Identity{}, "", ErrInvalidToken
		}
	} else {
		issuer := m.Issuer
		if issuer == "" {
			issuer = "drlife"
		}
		wantAudience := audienceForRole(claims.Role)
		if tokenType != "at+jwt" || claims.Issuer != issuer || len(claims.Audience) != 1 || claims.Audience[0] != wantAudience || expectedAudience != "" && expectedAudience != wantAudience {
			return gateway.Identity{}, "", ErrInvalidToken
		}
	}
	checker := m.Checker
	if checker == nil {
		checker = PassSessionChecker{}
	}
	active, err := checker.Active(ctx, claims.Subject, claims.SID)
	if err != nil && !(allowUnavailable && errors.Is(err, ErrSessionStoreUnavailable)) || err == nil && !active {
		return gateway.Identity{}, "", ErrInvalidToken
	}
	return gateway.Identity{AccountID: claims.Subject, Role: gateway.Role(claims.Role), SessionID: claims.SID}, claims.SID, nil
}

type Authenticator struct{ Tokens TokenManager }

func (a Authenticator) Authenticate(r *http.Request) (gateway.Identity, bool, error) {
	header := r.Header.Get("Authorization")
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return gateway.Identity{}, false, nil
	}
	identity, _, err := a.Tokens.parseAccess(r.Context(), strings.TrimSpace(parts[1]), audienceForPath(r.URL.Path), ordinaryRead(r))
	if err != nil {
		return gateway.Identity{}, false, nil
	}
	return identity, true, nil
}

func (m TokenManager) IssueRefresh(ctx context.Context, sessionPublicID string, rotation int64) (string, []byte, string, error) {
	if m.Keys == nil || sessionPublicID == "" || rotation < 0 {
		return "", nil, "", ErrInvalidToken
	}
	kid, key, err := m.Keys.ActiveKey(ctx)
	if err != nil {
		return "", nil, "", ErrInvalidToken
	}
	defer wipe(key)
	plain := derivedToken(key, "refresh", sessionPublicID, rotation)
	digest := sha256.Sum256([]byte(plain))
	return plain, digest[:], kid, nil
}

func (m TokenManager) ReissueRefresh(ctx context.Context, sessionPublicID, kid string, rotation int64) (string, []byte, error) {
	key, err := m.Keys.KeyByID(ctx, kid)
	if err != nil {
		return "", nil, ErrInvalidToken
	}
	defer wipe(key)
	plain := derivedToken(key, "refresh", sessionPublicID, rotation)
	digest := sha256.Sum256([]byte(plain))
	return plain, digest[:], nil
}

func randomToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("iam: random source failed")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func derivedToken(key []byte, purpose, sessionID string, rotation int64) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(purpose + ":" + sessionID + ":" + fmt.Sprint(rotation)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func audienceForRole(role string) string {
	switch role {
	case "buyer":
		return "drlife:buyer"
	case "merchant":
		return "drlife:merchant"
	case "driver":
		return "drlife:driver"
	case "admin":
		return "drlife:admin"
	case "wms":
		return "drlife:wms"
	default:
		return ""
	}
}

func audienceForPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "v1" {
		return audienceForRole(parts[1])
	}
	return ""
}

func ordinaryRead(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	path := r.URL.Path
	if strings.HasPrefix(path, "/v1/admin/") || strings.HasPrefix(path, "/v1/wms/") || strings.HasPrefix(path, "/v1/ledger/") {
		return false
	}
	return !strings.Contains(path, "/withdraw") && !strings.Contains(path, "/finance/")
}

func validRole(role string) bool {
	return role == string(gateway.RoleBuyer) || role == string(gateway.RoleMerchant) || role == string(gateway.RoleDriver)
}

func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
