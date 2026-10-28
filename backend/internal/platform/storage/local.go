package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type LocalConfig struct {
	BaseURL string
	Secret  []byte
	Now     func() time.Time
	RootDir string
}

type localStorage struct {
	baseURL string
	secret  []byte
	now     func() time.Time
	policy  Policy
	rootDir string
	puts    map[string]PutRequest
}

func NewLocal(config LocalConfig, policy Policy) (Storage, error) {
	if len(config.Secret) == 0 {
		return nil, ErrSecretRequired
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &localStorage{baseURL: strings.TrimRight(config.BaseURL, "/"), secret: config.Secret, now: now, policy: policy, rootDir: config.RootDir, puts: make(map[string]PutRequest)}, nil
}

func (s *localStorage) PresignPut(_ context.Context, in PutRequest) (SignedURL, error) {
	if err := s.policy.validate(in); err != nil {
		return SignedURL{}, err
	}
	key := randomKey()
	s.puts[key] = in
	signed := s.signed("PUT", key, in.ContentType, in.ContentLength, putTTL)
	signed.Headers = map[string]string{"Content-Type": in.ContentType, "Content-Length": strconv.FormatInt(in.ContentLength, 10), "X-Amz-Checksum-Sha256": in.ChecksumSHA256}
	return signed, nil
}

func (s *localStorage) Stat(_ context.Context, key string) (ObjectStat, error) {
	if !validRandomKey(key) {
		return ObjectStat{}, ErrInvalidObjectKey
	}
	in, ok := s.puts[key]
	if !ok || s.rootDir == "" {
		return ObjectStat{}, os.ErrNotExist
	}
	path := filepath.Join(s.rootDir, filepath.FromSlash(key))
	contents, err := os.ReadFile(path)
	if err != nil {
		return ObjectStat{}, err
	}
	sum := sha256.Sum256(contents)
	return ObjectStat{Size: int64(len(contents)), ContentType: in.ContentType, ChecksumSHA256: base64.StdEncoding.EncodeToString(sum[:])}, nil
}

func (s *localStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	if !validRandomKey(key) || s.rootDir == "" {
		return nil, os.ErrNotExist
	}
	return os.Open(filepath.Join(s.rootDir, filepath.FromSlash(key)))
}

func (s *localStorage) Delete(_ context.Context, key string) error {
	if !validRandomKey(key) || s.rootDir == "" {
		return os.ErrNotExist
	}
	return os.Remove(filepath.Join(s.rootDir, filepath.FromSlash(key)))
}

func (s *localStorage) PresignGet(_ context.Context, key string) (SignedURL, error) {
	if !validRandomKey(key) {
		return SignedURL{}, ErrInvalidObjectKey
	}
	return s.signed("GET", key, "", 0, getTTL), nil
}

func (s *localStorage) signed(method, key, contentType string, contentLength int64, ttl time.Duration) SignedURL {
	expiresAt := s.now().Add(ttl)
	values := url.Values{"expires": {strconv.FormatInt(expiresAt.Unix(), 10)}, "method": {method}}
	if contentType != "" {
		values.Set("content-type", contentType)
	}
	if contentLength > 0 {
		values.Set("content-length", strconv.FormatInt(contentLength, 10))
	}
	values.Set("signature", signature(s.secret, method, key, values))
	return SignedURL{Key: key, URL: s.baseURL + "/" + key + "?" + values.Encode(), ExpiresAt: expiresAt}
}

func VerifyLocalURL(rawURL string, secret []byte, now time.Time) error {
	if len(secret) == 0 {
		return ErrSecretRequired
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse signed URL: %w", err)
	}
	values := u.Query()
	expires, err := strconv.ParseInt(values.Get("expires"), 10, 64)
	if err != nil || now.After(time.Unix(expires, 0)) {
		return ErrURLExpired
	}
	signatureValue := values.Get("signature")
	values.Del("signature")
	key := strings.TrimPrefix(u.EscapedPath(), "/")
	if !validRandomKey(key) {
		return ErrInvalidObjectKey
	}
	if !hmac.Equal([]byte(signatureValue), []byte(signature(secret, values.Get("method"), key, values))) {
		return ErrInvalidSignature
	}
	return nil
}

func signature(secret []byte, method, key string, values url.Values) string {
	values = values.Clone()
	values.Del("signature")
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(method + "\n" + key + "\n" + values.Encode()))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
