package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
		})
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
