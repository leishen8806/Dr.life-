package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

func TestHealthAndReadiness(t *testing.T) {
	for _, test := range []struct {
		name    string
		handler http.Handler
		path    string
		status  int
		body    string
	}{
		{"health", NewHandler(nil), "/healthz", http.StatusOK, `{"ok":true}`},
		{"ready without checks", NewHandler(nil), "/readyz", http.StatusOK, ""},
		{"not ready", NewHandler(nil, func(context.Context) error { return context.DeadlineExceeded }), "/readyz", http.StatusServiceUnavailable, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, test.path, nil)
			w := httptest.NewRecorder()
			test.handler.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("status = %d, want %d", w.Code, test.status)
			}
			if test.body != "" && strings.TrimSpace(w.Body.String()) != test.body {
				t.Fatalf("body = %q, want %q", w.Body.String(), test.body)
			}
			if test.path == "/readyz" && !strings.Contains(w.Body.String(), `"request_id"`) {
				t.Fatalf("ready response missing request_id: %s", w.Body.String())
			}
			if test.name == "not ready" {
				if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
					t.Fatalf("Content-Type = %q", got)
				}
				var response struct {
					Error struct {
						Code    int               `json:"code"`
						Message map[string]string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Error.Code != int(httpx.CodeServiceUnavailable) {
					t.Fatalf("error code = %d", response.Error.Code)
				}
				for _, language := range []string{"zh", "en", "km"} {
					if response.Error.Message[language] == "" {
						t.Errorf("missing %s message: %#v", language, response.Error.Message)
					}
				}
			}
		})
	}
}

func TestMetricsAreNotExposedOnPublicHandler(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	NewHandler(nil).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", response.Code, response.Body.String())
	}
}

func TestHTTPServerSecurityLimits(t *testing.T) {
	srv := NewHTTPServer(":0", NewHandler(nil), time.Second, 2*time.Second, 3*time.Second)
	if srv.MaxHeaderBytes != 16<<10 || srv.ReadHeaderTimeout != time.Second || srv.ReadTimeout != 2*time.Second || srv.IdleTimeout != 3*time.Second || srv.WriteTimeout != 0 {
		t.Fatalf("unexpected server limits: %+v", srv)
	}
}

func TestSecurityHeadersCoverRouterErrors(t *testing.T) {
	handler := NewHandler(nil, func(context.Context) error { return context.DeadlineExceeded })
	for _, test := range []struct {
		method, path string
	}{
		{http.MethodGet, "/missing"},
		{http.MethodPost, "/v1/public/ping"},
		{http.MethodGet, "/readyz"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		for _, name := range []string{"X-Content-Type-Options", "Referrer-Policy", "Content-Security-Policy", "Cross-Origin-Resource-Policy", "Cache-Control"} {
			if response.Header().Get(name) == "" {
				t.Errorf("%s %s missing %s", test.method, test.path, name)
			}
		}
	}
}

func TestServeWaitsForInflightRequestDuringShutdown(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- Serve(ctx, srv, listener, time.Second) }()
	clientDone := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			defer response.Body.Close()
			_, err = io.Copy(io.Discard, response.Body)
		}
		clientDone <- err
	}()
	<-started
	cancel()
	select {
	case err := <-serveDone:
		t.Fatalf("Serve returned before in-flight request completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}
