package gateway

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

type WebhookVerifier interface {
	Verify(*http.Request, []byte) error
}

func VerifyWebhooks(prefix string, maxBody int64, bindings map[string]WebhookVerifier) func(http.Handler) http.Handler {
	if maxBody <= 0 {
		maxBody = 64 << 10
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			verifier := bindings[r.URL.Path]
			underPrefix := prefix != "" && (r.URL.Path == prefix || strings.HasPrefix(r.URL.Path, strings.TrimSuffix(prefix, "/")+"/"))
			if !underPrefix && verifier == nil {
				next.ServeHTTP(w, r)
				return
			}
			if verifier == nil {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeResourceNotFound))
				return
			}
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
			if err != nil {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeRequestTooLarge))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			if err := verifier.Verify(r, body); err != nil {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeWebhookInvalid))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type HeaderSecretVerifier struct {
	Header string
	Secret string
}

func (v HeaderSecretVerifier) Verify(r *http.Request, _ []byte) error {
	provided := r.Header.Get(v.Header)
	if v.Header == "" || v.Secret == "" || len(provided) != len(v.Secret) || subtle.ConstantTimeCompare([]byte(provided), []byte(v.Secret)) != 1 {
		return errors.New("webhook verification failed")
	}
	return nil
}

type HMACVerifier struct {
	Secret    []byte
	Tolerance time.Duration
	Nonces    JTIStore
	Now       func() time.Time
}

func (v HMACVerifier) Verify(r *http.Request, body []byte) error {
	if v.Tolerance <= 0 {
		v.Tolerance = 5 * time.Minute
	}
	if v.Now == nil {
		v.Now = time.Now
	}
	rawTime := r.Header.Get("X-Webhook-Timestamp")
	unix, err := strconv.ParseInt(rawTime, 10, 64)
	if err != nil || absDuration(v.Now().Sub(time.Unix(unix, 0))) > v.Tolerance {
		return errors.New("webhook verification failed")
	}
	nonce := r.Header.Get("X-Webhook-Nonce")
	provided, err := hex.DecodeString(r.Header.Get("X-Webhook-Signature"))
	if err != nil || nonce == "" || len(v.Secret) == 0 || v.Nonces == nil {
		return errors.New("webhook verification failed")
	}
	mac := hmac.New(sha256.New, v.Secret)
	_, _ = mac.Write(body)
	_, _ = mac.Write([]byte(rawTime))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return errors.New("webhook verification failed")
	}
	// Key replay protection to the authenticated signature, so changing an unsigned nonce cannot bypass it.
	fresh, err := v.Nonces.Consume(r.Context(), "webhook:"+hex.EncodeToString(provided), 10*time.Minute)
	if err != nil || !fresh {
		return errors.New("webhook verification failed")
	}
	return nil
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

type CIDRVerifier struct {
	Resolver ClientIPResolver
	Allowed  []*net.IPNet
}

func NewCIDRVerifier(resolver ClientIPResolver, cidrs []string) (CIDRVerifier, error) {
	verifier := CIDRVerifier{Resolver: resolver}
	for _, value := range cidrs {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return CIDRVerifier{}, errors.New("invalid webhook CIDR")
		}
		verifier.Allowed = append(verifier.Allowed, network)
	}
	return verifier, nil
}

func (v CIDRVerifier) Verify(r *http.Request, _ []byte) error {
	ip := resolveClientIP(v.Resolver, r)
	for _, network := range v.Allowed {
		if network.Contains(ip) {
			return nil
		}
	}
	return errors.New("webhook verification failed")
}

func resolveClientIP(resolver ClientIPResolver, request *http.Request) net.IP {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	direct := net.ParseIP(host)
	if resolver.Header != "" && direct != nil && resolver.isTrusted(direct) {
		if forwarded := net.ParseIP(strings.TrimSpace(request.Header.Get(resolver.Header))); forwarded != nil {
			return forwarded
		}
	}
	return direct
}
