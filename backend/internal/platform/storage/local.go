package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type LocalConfig struct {
	BaseURL string
	Secret  []byte
	Now     func() time.Time
}

type localStorage struct {
	baseURL string
	secret  []byte
	now     func() time.Time
	policy  Policy
}

func NewLocal(config LocalConfig, policy Policy) Storage {
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &localStorage{baseURL: strings.TrimRight(config.BaseURL, "/"), secret: config.Secret, now: now, policy: policy}
}

func (s *localStorage) PresignPut(_ context.Context, in PutRequest) (SignedURL, error) {
	if err := s.policy.validate(in); err != nil {
		return SignedURL{}, err
	}
	return s.signed("PUT", randomKey(), in.ContentType, in.ContentLength, putTTL), nil
}

func (s *localStorage) PresignGet(_ context.Context, key string) (SignedURL, error) {
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
