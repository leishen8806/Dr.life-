package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

func TestPublicCacheAndETagOnDeclaredRoute(t *testing.T) {
	handler := httpx.ETag(gateway.GroupedCORS(gateway.OriginGroups{})(httpx.RequestID(gateway.SecurityHeaders("test")(httpx.CacheControl(true, time.Minute, 5*time.Minute, "Accept-Language")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteOK(w, r, map[string]bool{"pong": true})
	}))))))

	first := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodGet, "/v1/public/products", nil)
	firstRequest.Header.Set("X-Request-ID", "first-request")
	handler.ServeHTTP(first, firstRequest)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	if !strings.Contains(first.Body.String(), `"request_id":"first-request"`) {
		t.Fatalf("first body is missing the response envelope: %s", first.Body.String())
	}
	if got := first.Header().Get("Cache-Control"); got != "public, max-age=60, stale-while-revalidate=300" {
		t.Fatalf("cache-control = %q", got)
	}
	if !hasHeaderValue(first.Header().Values("Vary"), "Origin") || !hasHeaderValue(first.Header().Values("Vary"), "Accept-Language") {
		t.Fatalf("Vary = %q, want Origin and Accept-Language", first.Header().Values("Vary"))
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}

	second := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/public/products", nil)
	request.Header.Set("X-Request-ID", "second-request")
	request.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(second, request)
	if second.Code != http.StatusNotModified {
		t.Fatalf("second status = %d, want 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Fatalf("304 body = %q", second.Body.String())
	}
	if second.Header().Get("ETag") != etag {
		t.Fatalf("304 etag = %q, want %q", second.Header().Get("ETag"), etag)
	}
	if got := second.Header().Get("Cache-Control"); got != "public, max-age=60, stale-while-revalidate=300" {
		t.Fatalf("304 cache-control = %q", got)
	}
}

func TestPublicCacheETagVariesByAcceptLanguage(t *testing.T) {
	handler := httpx.ETag(httpx.CacheControl(true, time.Minute, time.Minute, "Accept-Language")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Header.Get("Accept-Language")))
	})))

	english := httptest.NewRecorder()
	englishRequest := httptest.NewRequest(http.MethodGet, "/v1/public/products", nil)
	englishRequest.Header.Set("Accept-Language", "en")
	handler.ServeHTTP(english, englishRequest)

	chinese := httptest.NewRecorder()
	chineseRequest := httptest.NewRequest(http.MethodGet, "/v1/public/products", nil)
	chineseRequest.Header.Set("Accept-Language", "zh")
	chineseRequest.Header.Set("If-None-Match", english.Header().Get("ETag"))
	handler.ServeHTTP(chinese, chineseRequest)

	if english.Header().Get("ETag") == chinese.Header().Get("ETag") {
		t.Fatalf("ETag = %q for distinct language responses", chinese.Header().Get("ETag"))
	}
	if chinese.Code != http.StatusOK {
		t.Fatalf("language variant status = %d, want 200", chinese.Code)
	}
}

func TestUndeclaredPublicRouteKeepsNoStore(t *testing.T) {
	handler := httpx.ETag(httpx.RequestID(gateway.SecurityHeaders("test")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteOK(w, r, map[string]bool{"pong": true})
	}))))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"pong":true`) {
		t.Fatalf("undeclared public route status = %d body = %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("undeclared route cache-control = %q", got)
	}
	if got := response.Header().Get("ETag"); got != "" {
		t.Fatalf("undeclared route etag = %q", got)
	}
	if got := response.Header().Values("Vary"); len(got) != 0 {
		t.Fatalf("undeclared route Vary = %q", got)
	}
}

func hasHeaderValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestETagIgnoresNonGET(t *testing.T) {
	handler := httpx.ETag(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("created"))
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/public/products", nil)
	request.Header.Set("If-None-Match", `"anything"`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "created" {
		t.Fatalf("non-GET status = %d body = %q", response.Code, response.Body.String())
	}
}

func TestPrivateCacheControlIsNoStore(t *testing.T) {
	handler := httpx.CacheControl(false, time.Minute, time.Minute)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/public/secret", nil))
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("private cache-control = %q", got)
	}
}
