package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newTestLocal(t *testing.T, policy Policy) Storage {
	t.Helper()
	store, err := NewLocal(LocalConfig{BaseURL: "https://files.example.test", Secret: []byte("test-secret")}, policy)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func validChecksum(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func TestPresignPutRejectsInvalidRequests(t *testing.T) {
	store := newTestLocal(t, Policy{
		AllowedContentTypes: []string{"image/jpeg"},
		MaxContentLength:    1024,
	})

	for _, tc := range []struct {
		name string
		in   PutRequest
		want error
	}{
		{"content type", PutRequest{ContentType: "application/pdf", ContentLength: 1}, ErrContentTypeNotAllowed},
		{"content length", PutRequest{ContentType: "image/jpeg", ContentLength: 1025}, ErrContentLengthTooLarge},
		{"non-positive length", PutRequest{ContentType: "image/jpeg", ContentLength: 0}, ErrContentLengthInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.PresignPut(context.Background(), tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("PresignPut() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPresignPutRequiresAValidSHA256Checksum(t *testing.T) {
	store := newTestLocal(t, Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 1024})
	checksum := base64.StdEncoding.EncodeToString(sha256.New().Sum(nil)[:31])
	if _, err := store.PresignPut(context.Background(), PutRequest{ContentType: "image/jpeg", ContentLength: 1, ChecksumSHA256: checksum}); !errors.Is(err, ErrChecksumInvalid) {
		t.Fatalf("PresignPut() error = %v, want %v", err, ErrChecksumInvalid)
	}
}

func TestPresignedURLsHaveRequiredTTLAndNoPII(t *testing.T) {
	store := newTestLocal(t, Policy{
		AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 1024,
	})

	put, err := store.PresignPut(context.Background(), PutRequest{ContentType: "image/jpeg", ContentLength: 42, ChecksumSHA256: validChecksum("put")})
	if err != nil {
		t.Fatal(err)
	}
	if got := put.ExpiresAt.Sub(time.Now()); got < 9*time.Minute || got > 10*time.Minute+time.Second {
		t.Fatalf("PUT TTL = %s", got)
	}
	if strings.Contains(put.Key, "account-123") || strings.Contains(put.Key, "13800138000") || strings.Contains(put.Key, "passport.jpg") {
		t.Fatalf("key contains PII: %q", put.Key)
	}
	if !strings.HasPrefix(put.Key, "objects/") {
		t.Fatalf("key = %q", put.Key)
	}
	t.Logf("randomized object key sample: %s", put.Key)

	get, err := store.PresignGet(context.Background(), put.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got := get.ExpiresAt.Sub(time.Now()); got < 4*time.Minute || got > 5*time.Minute+time.Second {
		t.Fatalf("GET TTL = %s", got)
	}
	if err := VerifyLocalURL(get.URL, []byte("test-secret"), time.Now()); err != nil {
		t.Fatalf("VerifyLocalURL() = %v", err)
	}

	u, err := url.Parse(put.URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("content-type") != "image/jpeg" || u.Query().Get("content-length") != "42" {
		t.Fatalf("PUT constraints absent from %q", put.URL)
	}
}

func TestPresigningDoesNotLogSignature(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	store := newTestLocal(t, Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 1})
	signed, err := store.PresignPut(context.Background(), PutRequest{ContentType: "image/jpeg", ContentLength: 1, ChecksumSHA256: validChecksum("put")})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "signature=") || strings.Contains(logs.String(), signed.URL) {
		t.Fatalf("logs contain signed URL: %q", logs.String())
	}
}

func TestVerifyLocalURLRejectsExpiredAndModifiedURLs(t *testing.T) {
	store, err := NewLocal(LocalConfig{BaseURL: "https://files.example.test", Secret: []byte("test-secret"), Now: func() time.Time { return time.Unix(1000, 0) }}, Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 1})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := store.PresignGet(context.Background(), "objects/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(VerifyLocalURL(signed.URL, []byte("test-secret"), time.Unix(1301, 0)), ErrURLExpired) {
		t.Fatal("expired URL accepted")
	}
	if !errors.Is(VerifyLocalURL(strings.Replace(signed.URL, "objects/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "objects/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 1), []byte("test-secret"), time.Unix(1001, 0)), ErrInvalidSignature) {
		t.Fatal("modified URL accepted")
	}
}

func TestVerifyLocalURLRejectsEmptySecretAndInvalidObjectKey(t *testing.T) {
	const key = "objects/../../etc/passwd"
	values := url.Values{"expires": {"2000"}, "method": {"GET"}}
	values.Set("signature", signature([]byte("test-secret"), "GET", key, values))
	rawURL := "https://files.example.test/" + key + "?" + values.Encode()

	if !errors.Is(VerifyLocalURL(rawURL, nil, time.Unix(1000, 0)), ErrSecretRequired) {
		t.Fatal("empty secret was accepted")
	}
	if !errors.Is(VerifyLocalURL(rawURL, []byte("test-secret"), time.Unix(1000, 0)), ErrInvalidObjectKey) {
		t.Fatal("invalid object key was accepted")
	}
}

func TestPolicyFailsClosedWithoutExplicitLimits(t *testing.T) {
	for _, policy := range []Policy{
		{},
		{AllowedContentTypes: []string{"image/jpeg"}},
		{MaxContentLength: 1},
	} {
		_, err := newTestLocal(t, policy).PresignPut(context.Background(), PutRequest{ContentType: "image/jpeg", ContentLength: 1, ChecksumSHA256: validChecksum("put")})
		if !errors.Is(err, ErrPolicyNotConfigured) {
			t.Fatalf("PresignPut() error = %v, want %v", err, ErrPolicyNotConfigured)
		}
	}
}

func TestNewLocalRejectsEmptySecret(t *testing.T) {
	_, err := NewLocal(LocalConfig{BaseURL: "https://files.example.test"}, Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 1})
	if !errors.Is(err, ErrSecretRequired) {
		t.Fatalf("NewLocal() error = %v, want %v", err, ErrSecretRequired)
	}
}

func TestPresignGetRejectsInvalidObjectKeys(t *testing.T) {
	store := newTestLocal(t, Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 1})
	for _, key := range []string{"", "objects/..", "objects/a?b", "objects/a#b", "objects/1234", "objects/" + strings.Repeat("a", 33)} {
		if _, err := store.PresignGet(context.Background(), key); !errors.Is(err, ErrInvalidObjectKey) {
			t.Fatalf("PresignGet(%q) error = %v, want %v", key, err, ErrInvalidObjectKey)
		}
	}
}
