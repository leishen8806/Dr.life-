package iam

import (
	"context"
	"crypto/rand"
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

func TestRefreshTokenOnlyHashMatches(t *testing.T) {
	plain, hash, err := NewRefreshToken()
	if err != nil || len(plain) < 43 || len(hash) != 32 || strings.Contains(string(hash), plain) {
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
	}{{"wrong role", "/v1/merchant/orders", buyer, http.StatusForbidden}, {"admin", "/v1/admin/users", buyer, http.StatusForbidden}, {"missing", "/v1/buyer/orders", "", http.StatusUnauthorized}} {
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
