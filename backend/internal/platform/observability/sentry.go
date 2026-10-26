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
	bearerTokenPattern   = regexp.MustCompile(`(?i)\bbearer[ \t]+[^\s,;]+`)
	colonTokenPattern    = regexp.MustCompile(`\b[0-9]+:[[:alnum:]_-]+\b`)
	phonePattern         = regexp.MustCompile(`\+?[0-9](?:[ ()-]*[0-9]){7,14}`)
	authorizationPattern = regexp.MustCompile(`(?i)\bauthorization["']?[ \t]*[:=][ \t]*(?:basic|bearer)?[ \t]*[^\s,;]+`)
	namedTokenPattern    = regexp.MustCompile(`(?i)\b(?:init_?data|token)["']?[ \t]*[:=][ \t]*[^\s,;]+`)
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
	event.User = sentry.User{ID: event.User.ID}
	scrubStrings(&event.Message, &event.Dist, &event.Environment, &event.Platform, &event.Release, &event.ServerName, &event.Transaction, &event.Logger, &event.Type)
	for i := range event.Fingerprint {
		event.Fingerprint[i] = scrubText(event.Fingerprint[i])
	}
	scrubStringMap(event.Tags)
	scrubStringMap(event.Modules)
	for _, context := range event.Contexts {
		scrubMap(context)
	}
	for _, breadcrumb := range event.Breadcrumbs {
		if breadcrumb == nil {
			continue
		}
		scrubStrings(&breadcrumb.Type, &breadcrumb.Category, &breadcrumb.Message)
		scrubMap(breadcrumb.Data)
	}
	for i := range event.Exception {
		event.Exception[i].Type = scrubText(event.Exception[i].Type)
		event.Exception[i].Value = scrubText(event.Exception[i].Value)
		event.Exception[i].Module = scrubText(event.Exception[i].Module)
		scrubStacktrace(event.Exception[i].Stacktrace)
		if mechanism := event.Exception[i].Mechanism; mechanism != nil {
			scrubStrings(&mechanism.Type, &mechanism.Description, &mechanism.HelpLink, &mechanism.Source)
			scrubMap(mechanism.Data)
		}
	}
	for i := range event.Threads {
		event.Threads[i].Name = scrubText(event.Threads[i].Name)
		scrubStacktrace(event.Threads[i].Stacktrace)
	}
	for _, span := range event.Spans {
		if span == nil {
			continue
		}
		scrubStrings(&span.Name, &span.Op, &span.Description)
		scrubStringMap(span.Tags)
		scrubMap(span.Data)
	}
	if event.Request != nil {
		event.Request.Data = ""
		event.Request.Cookies = ""
		for key, value := range event.Request.Headers {
			if sensitiveName(key) {
				delete(event.Request.Headers, key)
				continue
			}
			event.Request.Headers[key] = scrubText(value)
		}
		scrubStringMap(event.Request.Env)
		event.Request.QueryString = scrubQuery(event.Request.QueryString)
		if parsed, err := url.Parse(event.Request.URL); err == nil {
			parsed.RawQuery = scrubQuery(parsed.RawQuery)
			parsed.Fragment = ""
			parsed.Path = scrubText(parsed.Path)
			parsed.RawPath = ""
			parsed.User = nil
			event.Request.URL = parsed.String()
		} else {
			event.Request.URL = ""
		}
	}
	return event
}

func scrubStrings(values ...*string) {
	for _, value := range values {
		*value = scrubText(*value)
	}
}

func scrubStringMap(values map[string]string) {
	for key, value := range values {
		if sensitiveName(key) {
			values[key] = "[REDACTED]"
			continue
		}
		values[key] = scrubText(value)
	}
}

func scrubMap(values map[string]any) {
	for key, value := range values {
		if sensitiveName(key) {
			values[key] = "[REDACTED]"
			continue
		}
		values[key] = scrubValue(value)
	}
}

func scrubValue(value any) any {
	switch value := value.(type) {
	case string:
		return scrubText(value)
	case map[string]any:
		scrubMap(value)
		return value
	case map[string]string:
		scrubStringMap(value)
		return value
	case []any:
		for i := range value {
			value[i] = scrubValue(value[i])
		}
		return value
	case []string:
		for i := range value {
			value[i] = scrubText(value[i])
		}
		return value
	default:
		return value
	}
}

func scrubStacktrace(stacktrace *sentry.Stacktrace) {
	if stacktrace == nil {
		return
	}
	for i := range stacktrace.Frames {
		frame := &stacktrace.Frames[i]
		scrubStrings(&frame.Function, &frame.Symbol, &frame.Module, &frame.Filename, &frame.AbsPath, &frame.ContextLine, &frame.Package, &frame.InstructionAddr, &frame.AddrMode, &frame.SymbolAddr, &frame.ImageAddr, &frame.Platform)
		for j := range frame.PreContext {
			frame.PreContext[j] = scrubText(frame.PreContext[j])
		}
		for j := range frame.PostContext {
			frame.PostContext[j] = scrubText(frame.PostContext[j])
		}
		scrubMap(frame.Vars)
	}
}

func scrubText(value string) string {
	value = authorizationPattern.ReplaceAllString(value, "[REDACTED]")
	value = namedTokenPattern.ReplaceAllString(value, "[REDACTED]")
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
			continue
		}
		for i, value := range values[key] {
			values[key][i] = scrubText(value)
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
