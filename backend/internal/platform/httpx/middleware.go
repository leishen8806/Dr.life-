package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
)

func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					if logger != nil {
						logger.ErrorContext(r.Context(), "panic recovered",
							"request_id", RequestIDFromContext(r.Context()),
							"panic_type", fmt.Sprintf("%T", recovered),
							"stack", string(debug.Stack()),
						)
					}
					if state, ok := w.(interface{ Written() bool }); ok && state.Written() {
						return
					}
					WriteError(w, r, NewAppError(CodeInternalError))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func RequestLogger(logger *slog.Logger, roleFromContext ...func(context.Context) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			response := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				if logger == nil {
					return
				}
				path := "[unmatched]"
				if route := chi.RouteContext(r.Context()).RoutePattern(); route != "" {
					path = route
				}
				role := ""
				if len(roleFromContext) == 1 {
					role = roleFromContext[0](r.Context())
				}
				logger.InfoContext(r.Context(), "http request",
					"request_id", RequestIDFromContext(r.Context()),
					"method", r.Method,
					"path", path,
					"status", response.status,
					"duration_ms", time.Since(started).Milliseconds(),
					"role", role,
				)
			}()
			next.ServeHTTP(response, r)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *statusWriter) Written() bool {
	return w.wroteHeader
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *statusWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
