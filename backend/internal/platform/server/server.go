package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

type ReadinessCheck func(context.Context) error

func NewHandler(logger *slog.Logger, checks ...ReadinessCheck) http.Handler {
	router := chi.NewRouter()
	router.Use(httpx.RequestID, httpx.RequestLogger(logger), httpx.Recover(logger))
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		for _, check := range checks {
			if err := check(r.Context()); err != nil {
				if logger != nil {
					logger.ErrorContext(r.Context(), "readiness check failed", "request_id", httpx.RequestIDFromContext(r.Context()))
				}
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
				return
			}
		}
		httpx.WriteOK(w, r, map[string]bool{"ready": true})
	})
	return router
}

func Serve(ctx context.Context, srv *http.Server, listener net.Listener, shutdownTimeout time.Duration) error {
	serveResult := make(chan error, 1)
	go func() { serveResult <- srv.Serve(listener) }()
	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		return err
	}
	err := <-serveResult
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
