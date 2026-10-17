package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

func TestPublicCacheAndETagOnDeclaredRoute(t *testing.T) {
	handler := gateway.SecurityHeaders("test")(httpx.CacheControl(true, time.Minute, 5*time.Minute)(httpx.ETag(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/v1/public/products", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	if got := first.Header().Get("Cache-Control"); got != "public, max-age=60, stale-while-revalidate=300" {
		t.Fatalf("cache-control = %q", got)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}

	second := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/public/products", nil)
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

func TestUndeclaredRouteKeepsNoStore(t *testing.T) {
	handler := gateway.SecurityHeaders("test")(httpx.ETag(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("private"))
	})))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/buyer/orders", nil))
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("undeclared route cache-control = %q", got)
	}
	if got := response.Header().Get("ETag"); got != "" {
		t.Fatalf("undeclared route etag = %q", got)
	}
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
