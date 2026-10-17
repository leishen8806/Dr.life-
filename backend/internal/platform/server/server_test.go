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
	"github.com/leishen8806/Dr.life-/backend/internal/platform/overload"
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

func TestServeDrainsBeforeShutdown(t *testing.T) {
	bulkhead := overload.New(overload.Config{Total: 2, Wait: time.Millisecond, Limits: overload.Limits{Critical: 2, Auth: 2, Write: 2, Read: 2, Heavy: 2}, Timeouts: overload.DefaultConfig().Timeouts})
	started, release := make(chan struct{}), make(chan struct{})
	router := http.NewServeMux()
	router.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if bulkhead.Draining() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	router.HandleFunc("/v1/public/ping", func(w http.ResponseWriter, _ *http.Request) { close(started); <-release; w.WriteHeader(http.StatusOK) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, &http.Server{Handler: bulkhead.Middleware(router)}, listener, time.Second, struct {
			Drainer
			Delay time.Duration
		}{Drainer: bulkhead, Delay: 50 * time.Millisecond})
	}()
	clientDone := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String() + "/v1/public/ping")
		if err == nil {
			response.Body.Close()
		}
		clientDone <- err
	}()
	<-started
	cancel()
	ready := eventually(t, func() int {
		response, err := http.Get("http://" + listener.Addr().String() + "/readyz")
		if err != nil {
			return 0
		}
		defer response.Body.Close()
		return response.StatusCode
	})
	if ready != http.StatusServiceUnavailable {
		t.Fatalf("ready status=%d", ready)
	}
	response, err := http.Get("http://" + listener.Addr().String() + "/v1/public/ping")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("new request status=%d", response.StatusCode)
	}
	select {
	case err := <-done:
		t.Fatalf("shutdown completed before inflight request: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func eventually(t *testing.T, read func() int) int {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if value := read(); value != 0 {
			return value
		}
		select {
		case <-deadline.C:
			t.Fatal("condition did not become true")
		case <-tick.C:
		}
	}
}
