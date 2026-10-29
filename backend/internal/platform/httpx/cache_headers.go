package httpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CacheControl sets the response cache policy. Public responses are the only
// ones allowed to be cached; everything else keeps the gateway's no-store
// default. Install it inside gateway.SecurityHeaders and httpx.ETag so this
// value wins and the validator sees the declaration.
func CacheControl(public bool, maxAge, staleWhileRevalidate time.Duration, vary ...string) func(http.Handler) http.Handler {
	publicValue := ""
	if public {
		publicValue = "public, max-age=" + seconds(maxAge) + ", stale-while-revalidate=" + seconds(staleWhileRevalidate)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			value := "no-store"
			if publicValue != "" && r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/public/") {
				value = publicValue
				for _, header := range vary {
					if header = strings.TrimSpace(header); header != "" {
						w.Header().Add("Vary", header)
					}
				}
			}
			w.Header().Set("Cache-Control", value)
			next.ServeHTTP(w, r)
		})
	}
}

func seconds(duration time.Duration) string {
	value := int64(duration / time.Second)
	if value < 0 {
		value = 0
	}
	return strconv.FormatInt(value, 10)
}

// ETag adds a content-derived validator to explicitly declared public GET
// routes and answers a matching If-None-Match with 304. A route is declared by
// setting a public Cache-Control value (normally via CacheControl); public
// paths without that declaration keep the gateway's no-store policy and never
// receive an ETag. Install ETag outside the router so inner middleware and
// handlers can still set their headers before the body is buffered.
func ETag(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/v1/public/") {
			next.ServeHTTP(w, r)
			return
		}

		buffered := &bufferedWriter{target: w, header: w.Header(), status: http.StatusOK}
		next.ServeHTTP(buffered, r)

		status := buffered.status
		if !buffered.wrote {
			status = http.StatusOK
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write(buffered.body.Bytes())
			return
		}

		if !isPublic(buffered.header.Get("Cache-Control")) {
			w.Header().Set("Content-Length", strconv.Itoa(buffered.body.Len()))
			w.WriteHeader(status)
			_, _ = w.Write(buffered.body.Bytes())
			return
		}
		etag := contentETag(stableBody(buffered.body.Bytes()))
		w.Header().Set("ETag", etag)
		if etagMatches(r.Header.Get("If-None-Match"), etag) {
			w.Header().Del("Content-Length")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(buffered.body.Len()))
		w.WriteHeader(status)
		_, _ = w.Write(buffered.body.Bytes())
	})
}

// stableBody removes the per-request envelope ID so identical payloads hash
// identically. Non-JSON bodies are hashed as-is.
func stableBody(body []byte) []byte {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	delete(payload, "request_id")
	stable, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return stable
}

func isPublic(cacheControl string) bool {
	return strings.HasPrefix(strings.TrimSpace(cacheControl), "public")
}

func contentETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func etagMatches(header, current string) bool {
	if strings.TrimSpace(header) == "" {
		return false
	}
	current = strings.TrimPrefix(strings.TrimSpace(current), "W/")
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		if strings.TrimPrefix(candidate, "W/") == current {
			return true
		}
	}
	return false
}

// bufferedWriter captures status and body so the ETag can be computed before
// anything is sent. Headers are written straight to the real ResponseWriter so
// outer middleware values such as Cache-Control are preserved.
type bufferedWriter struct {
	target http.ResponseWriter
	header http.Header
	status int
	wrote  bool
	body   bytes.Buffer
}

func (w *bufferedWriter) Header() http.Header { return w.header }

func (w *bufferedWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status, w.wrote = status, true
}

func (w *bufferedWriter) Write(body []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(body)
}
