package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

var (
	bearerTokenPattern = regexp.MustCompile(`(?i)\bbearer[ \t]+[^\s,;]+`)
	colonTokenPattern  = regexp.MustCompile(`\b[0-9]+:[[:alnum:]_-]+\b`)
	phonePattern       = regexp.MustCompile(`\+?[0-9](?:[ ()-]*[0-9]){7,14}`)
)

type ErrorReporter struct {
	hub *sentry.Hub
}

func NewErrorReporter(dsn, environment, release string, transport sentry.Transport) (*ErrorReporter, error) {
	if strings.TrimSpace(dsn) == "" {
		return &ErrorReporter{}, nil
	}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn: dsn, Environment: environment, Release: release, Transport: transport,
		BeforeSend: scrubEvent,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Sentry: %w", err)
	}
	return &ErrorReporter{hub: sentry.NewHub(client, sentry.NewScope())}, nil
}

func (r *ErrorReporter) Enabled() bool { return r != nil && r.hub != nil }

func (r *ErrorReporter) Flush(timeout time.Duration) bool {
	return !r.Enabled() || r.hub.Flush(timeout)
}

func (r *ErrorReporter) Capture(ctx context.Context, err error) {
	if !r.Enabled() {
		return
	}
	hub := r.hub.Clone()
	hub.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("request_id", httpx.RequestIDFromContext(ctx))
		if identity, ok := gateway.IdentityFromContext(ctx); ok {
			scope.SetTag("role", string(identity.Role))
		}
	})
	hub.CaptureException(err)
}

func Recover(logger *slog.Logger, reporter *ErrorReporter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					requestID := httpx.RequestIDFromContext(r.Context())
					if logger != nil {
						logger.ErrorContext(r.Context(), "panic recovered", "request_id", requestID, "panic_type", fmt.Sprintf("%T", recovered), "stack", string(debug.Stack()))
					}
					reporter.Capture(r.Context(), fmt.Errorf("panic recovered: %T", recovered))
					if state, ok := w.(interface{ Written() bool }); ok && state.Written() {
						return
					}
					httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInternalError))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func scrubEvent(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event == nil {
		return nil
	}
	event.Extra = nil
	event.User = sentry.User{ID: event.User.ID}
	event.Message = scrubText(event.Message)
	for i := range event.Exception {
		event.Exception[i].Type = scrubText(event.Exception[i].Type)
		event.Exception[i].Value = scrubText(event.Exception[i].Value)
		event.Exception[i].Module = scrubText(event.Exception[i].Module)
	}
	if event.Request != nil {
		event.Request.Data = ""
		event.Request.Cookies = ""
		for key := range event.Request.Headers {
			if sensitiveName(key) {
				delete(event.Request.Headers, key)
			}
		}
		event.Request.QueryString = scrubQuery(event.Request.QueryString)
		if parsed, err := url.Parse(event.Request.URL); err == nil {
			parsed.RawQuery = scrubQuery(parsed.RawQuery)
			parsed.Fragment = ""
			event.Request.URL = parsed.String()
		}
	}
	return event
}

func scrubText(value string) string {
	value = bearerTokenPattern.ReplaceAllString(value, "[REDACTED]")
	value = colonTokenPattern.ReplaceAllString(value, "[REDACTED]")
	return phonePattern.ReplaceAllString(value, "[REDACTED]")
}

func scrubQuery(raw string) string {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return ""
	}
	for key := range values {
		if sensitiveName(key) {
			values.Del(key)
		}
	}
	return values.Encode()
}

func sensitiveName(name string) bool {
	name = strings.ToLower(strings.ReplaceAll(name, "-", "_"))
	for _, marker := range []string{"authorization", "cookie", "idempotency_key", "init_data", "initdata", "token", "secret", "password", "phone", "key"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}
