package iam

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
)

func TestTokenIssueParseRotationAndAuthenticator(t *testing.T) {
	oldKey, newKey := make([]byte, 32), make([]byte, 32)
	_, _ = rand.Read(oldKey)
	_, _ = rand.Read(newKey)
	oldRing, _ := secrets.NewKeyring(map[string][]byte{"old": oldKey, "new": newKey}, "old")
	newRing, _ := secrets.NewKeyring(map[string][]byte{"old": oldKey, "new": newKey}, "new")
	now := time.Unix(1_800_000_000, 0)
	oldManager := TokenManager{Keys: oldRing, AccessTTL: 15 * time.Minute, Now: func() time.Time { return now }}
	rawOld, err := oldManager.IssueAccess(context.Background(), "acct", "buyer", "session")
	if err != nil {
		t.Fatal(err)
	}
	manager := TokenManager{Keys: newRing, AccessTTL: 15 * time.Minute, Now: func() time.Time { return now }}
	identity, sid, err := manager.ParseAccess(context.Background(), rawOld)
	if err != nil || identity.Role != gateway.RoleBuyer || sid != "session" {
		t.Fatalf("parse = %+v %q %v", identity, sid, err)
	}
	rawNew, _ := manager.IssueAccess(context.Background(), "acct", "merchant", "session2")
	if _, _, err := oldManager.ParseAccess(context.Background(), rawNew); err != nil {
		t.Fatalf("new key not accepted: %v", err)
	}
	req := httptest.NewRequest("GET", "/v1/buyer/orders", nil)
	req.Header.Set("Authorization", "Bearer "+rawOld)
	got, ok, err := (Authenticator{Tokens: manager}).Authenticate(req)
	if err != nil || !ok || got.AccountID != "acct" {
		t.Fatalf("auth = %+v %v %v", got, ok, err)
	}
}

func TestTokenRejectsExpiryKidAndAlgorithmConfusion(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	ring, _ := secrets.NewKeyring(map[string][]byte{"active": key}, "active")
	now := time.Unix(1_800_000_000, 0)
	m := TokenManager{Keys: ring, AccessTTL: time.Second, Now: func() time.Time { return now }}
	raw, _ := m.IssueAccess(context.Background(), "acct", "buyer", "sid")
	if _, _, err := m.ParseAccess(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatal("bad fixture")
	}
	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, AccessClaims{Role: "buyer", SID: "sid", RegisteredClaims: jwt.RegisteredClaims{Subject: "acct", ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)), ID: "jti"}})
	unsigned.Header["kid"] = "active"
	none, _ := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, _, err := m.ParseAccess(context.Background(), none); err == nil {
		t.Fatal("alg none accepted")
	}
	hs384 := jwt.NewWithClaims(jwt.SigningMethodHS384, AccessClaims{Role: "buyer", SID: "sid", RegisteredClaims: jwt.RegisteredClaims{Issuer: "drlife", Audience: jwt.ClaimStrings{"drlife:buyer"}, Subject: "acct", ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)), ID: "jti"}})
	hs384.Header["kid"], hs384.Header["typ"] = "active", "at+jwt"
	hs384Raw, _ := hs384.SignedString(key)
	if _, _, err := m.ParseAccess(context.Background(), hs384Raw); err == nil {
		t.Fatal("HS384 accepted")
	}
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	rs256 := jwt.NewWithClaims(jwt.SigningMethodRS256, AccessClaims{Role: "buyer", SID: "sid", RegisteredClaims: jwt.RegisteredClaims{Issuer: "drlife", Audience: jwt.ClaimStrings{"drlife:buyer"}, Subject: "acct", ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)), ID: "jti"}})
	rs256.Header["kid"], rs256.Header["typ"] = "active", "at+jwt"
	rs256Raw, _ := rs256.SignedString(rsaKey)
	if _, _, err := m.ParseAccess(context.Background(), rs256Raw); err == nil {
		t.Fatal("RS256 accepted")
	}
	badKid := jwt.NewWithClaims(jwt.SigningMethodHS256, AccessClaims{Role: "buyer", SID: "sid", RegisteredClaims: jwt.RegisteredClaims{Subject: "acct", ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)), ID: "jti"}})
	badKid.Header["kid"] = "missing"
	bad, _ := badKid.SignedString(key)
	if _, _, err := m.ParseAccess(context.Background(), bad); err == nil {
		t.Fatal("bad kid accepted")
	}
	expired := TokenManager{Keys: ring, AccessTTL: time.Second, Now: func() time.Time { return now.Add(-time.Hour) }}
	expiredRaw, _ := expired.IssueAccess(context.Background(), "acct", "buyer", "sid")
	if _, _, err := m.ParseAccess(context.Background(), expiredRaw); err == nil {
		t.Fatal("expired accepted")
	}
}

func TestAccessTokenTypeAudienceIssuerAndLegacyWindow(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	ring, _ := secrets.NewKeyring(map[string][]byte{"active": key}, "active")
	now := time.Unix(1_800_000_000, 0)
	manager := TokenManager{Keys: ring, AccessTTL: 15 * time.Minute, Issuer: "drlife", Now: func() time.Time { return now }}
	raw, err := manager.IssueAccess(context.Background(), "acct", "buyer", "sid")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, _ := jwt.NewParser().ParseUnverified(raw, new(AccessClaims))
	claims := parsed.Claims.(*AccessClaims)
	if parsed.Header["typ"] != "at+jwt" || claims.Issuer != "drlife" || len(claims.Audience) != 1 || claims.Audience[0] != "drlife:buyer" {
		t.Fatalf("unexpected access contract: header=%v claims=%+v", parsed.Header, claims)
	}

	sign := func(role, issuer string, audience jwt.ClaimStrings, typ string) string {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, AccessClaims{Role: role, SID: "sid", RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, Audience: audience, Subject: "acct", IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)), ID: "jti"}})
		token.Header["kid"] = "active"
		if typ == "" {
			delete(token.Header, "typ")
		} else {
			token.Header["typ"] = typ
		}
		value, _ := token.SignedString(key)
		return value
	}
	for _, test := range []struct {
		name, token string
	}{
		{"missing typ", sign("buyer", "drlife", jwt.ClaimStrings{"drlife:buyer"}, "")},
		{"wrong typ", sign("buyer", "drlife", jwt.ClaimStrings{"drlife:buyer"}, "JWT")},
		{"missing audience", sign("buyer", "drlife", nil, "at+jwt")},
		{"wrong audience", sign("buyer", "drlife", jwt.ClaimStrings{"drlife:merchant"}, "at+jwt")},
		{"role audience mismatch", sign("merchant", "drlife", jwt.ClaimStrings{"drlife:buyer"}, "at+jwt")},
		{"wrong issuer", sign("buyer", "other", jwt.ClaimStrings{"drlife:buyer"}, "at+jwt")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := manager.ParseAccess(context.Background(), test.token); err == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}

	legacy := sign("buyer", "", nil, "")
	if _, _, err := manager.ParseAccess(context.Background(), legacy); err == nil {
		t.Fatal("legacy token accepted while transition is closed")
	}
	manager.LegacyAcceptUntil = now.Add(15 * time.Minute)
	if _, _, err := manager.ParseAccess(context.Background(), legacy); err != nil {
		t.Fatalf("legacy token rejected during transition: %v", err)
	}
	manager.Now = func() time.Time { return now.Add(15 * time.Minute) }
	if _, _, err := manager.ParseAccess(context.Background(), legacy); err == nil {
		t.Fatal("legacy token accepted at transition expiry")
	}
}

func TestWrongAudienceIsUnauthenticatedBeforeRoleAuthorization(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	ring, _ := secrets.NewKeyring(map[string][]byte{"active": key}, "active")
	manager := TokenManager{Keys: ring, AccessTTL: 15 * time.Minute}
	buyer, _ := manager.IssueAccess(context.Background(), "acct", "buyer", "sid")
	for _, path := range []string{"/v1/merchant/orders", "/v1/driver/tasks"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+buyer)
		if _, ok, err := (Authenticator{Tokens: manager}).Authenticate(req); err != nil || ok {
			t.Fatalf("wrong audience authenticated for %s: ok=%v err=%v", path, ok, err)
		}
	}
}

func TestRefreshTokenOnlyHashMatches(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	ring, _ := secrets.NewKeyring(map[string][]byte{"active": key}, "active")
	plain, hash, kid, err := (TokenManager{Keys: ring}).IssueRefresh(context.Background(), "session", 0)
	if err != nil || kid != "active" || len(plain) < 43 || len(hash) != 32 || strings.Contains(string(hash), plain) {
		t.Fatalf("refresh = %d/%d %v", len(plain), len(hash), err)
	}
}

func TestLoadJWTKeyringFromEnvironment(t *testing.T) {
	oldKey, newKey := make([]byte, 32), make([]byte, 32)
	_, _ = rand.Read(oldKey)
	_, _ = rand.Read(newKey)
	t.Setenv("JWT_SIGNING_KEYS", "old:"+base64.StdEncoding.EncodeToString(oldKey)+",current:"+base64.StdEncoding.EncodeToString(newKey))
	t.Setenv("JWT_ACTIVE_KEY_ID", "current")
	ring, err := LoadJWTKeyringFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	id, key, err := ring.ActiveKey(context.Background())
	if err != nil || id != "current" || string(key) != string(newKey) {
		t.Fatalf("keyring id=%q err=%v", id, err)
	}
	wipe(key)
}

func TestJWTAuthenticatorThroughGateway(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	ring, _ := secrets.NewKeyring(map[string][]byte{"active": key}, "active")
	now := time.Unix(1_800_000_000, 0)
	manager := TokenManager{Keys: ring, AccessTTL: 15 * time.Minute, Now: func() time.Time { return now }}
	buyer, _ := manager.IssueAccess(context.Background(), "buyer-account", "buyer", "sid")
	handler := gateway.Authenticate(Authenticator{Tokens: manager}, "")(gateway.Authorize("")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })))
	for _, tc := range []struct {
		name, path, token string
		want              int
	}{{"wrong role", "/v1/merchant/orders", buyer, http.StatusUnauthorized}, {"admin", "/v1/admin/users", buyer, http.StatusUnauthorized}, {"missing", "/v1/buyer/orders", "", http.StatusUnauthorized}} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s status=%d body=%s", tc.name, w.Code, w.Body.String())
		}
	}
	expiredManager := TokenManager{Keys: ring, AccessTTL: time.Second, Now: func() time.Time { return now.Add(-time.Hour) }}
	expired, _ := expiredManager.IssueAccess(context.Background(), "buyer-account", "buyer", "sid")
	req := httptest.NewRequest(http.MethodGet, "/v1/buyer/orders", nil)
	req.Header.Set("Authorization", "Bearer "+expired)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired status=%d", w.Code)
	}
}
