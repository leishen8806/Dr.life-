package logx

import (
	"context"
	"io"
	"log/slog"
	"strings"
)

const redacted = "[REDACTED]"

var sensitiveNames = []string{"token", "secret", "password", "key", "authorization", "phone", "initdata"}

type redactingHandler struct {
	inner slog.Handler
}

func NewRedactingHandler(inner slog.Handler) slog.Handler {
	return &redactingHandler{inner: inner}
}

func NewLogger(level slog.Level, output io.Writer) *slog.Logger {
	base := slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level})
	return slog.New(NewRedactingHandler(base))
}

func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	redactedRecord := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		redactedRecord.AddAttrs(Redact(attr))
		return true
	})
	return h.inner.Handle(ctx, redactedRecord)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &redactingHandler{inner: h.inner.WithAttrs(redactAttrs(attrs))}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{inner: h.inner.WithGroup(name)}
}

func Redact(attr slog.Attr) slog.Attr {
	if isSensitive(attr.Key) {
		return slog.String(attr.Key, redacted)
	}
	value := attr.Value.Resolve()
	if value.Kind() == slog.KindGroup {
		attr.Value = slog.GroupValue(redactAttrs(value.Group())...)
	} else if value.Kind() == slog.KindAny {
		attr.Value = slog.AnyValue(redactNested(value.Any()))
	} else {
		attr.Value = value
	}
	return attr
}

func redactAttrs(attrs []slog.Attr) []slog.Attr {
	redactedAttrs := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		redactedAttrs[i] = Redact(attr)
	}
	return redactedAttrs
}

func isSensitive(name string) bool {
	name = strings.ToLower(name)
	for _, marker := range sensitiveNames {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

func redactNested(value any) any {
	switch nested := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(nested))
		for key, item := range nested {
			if isSensitive(key) {
				result[key] = redacted
			} else {
				result[key] = redactNested(item)
			}
		}
		return result
	case map[string]string:
		result := make(map[string]string, len(nested))
		for key, item := range nested {
			if isSensitive(key) {
				result[key] = redacted
			} else {
				result[key] = item
			}
		}
		return result
	case []any:
		result := make([]any, len(nested))
		for i, item := range nested {
			result[i] = redactNested(item)
		}
		return result
	default:
		return value
	}
}
