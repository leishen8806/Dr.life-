package logx

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactsSensitiveFieldsIncludingNestedValues(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &output)
	logger.Info("request",
		slog.String("access_TOKEN", "top-secret"),
		slog.Group("credentials",
			slog.String("refresh_token", "refresh-secret"),
			slog.Group("profile", slog.String("PhoneNumber", "0123456789")),
		),
		slog.Any("payload", map[string]any{
			"safe":   "visible",
			"nested": map[string]any{"initData": "telegram-secret"},
			"list":   []any{map[string]any{"authorization": "bearer-secret"}},
		}),
		slog.String("safe", "visible"),
	)

	line := output.String()
	for _, secret := range []string{"top-secret", "refresh-secret", "0123456789", "telegram-secret", "bearer-secret"} {
		if strings.Contains(line, secret) {
			t.Errorf("log leaked %q: %s", secret, line)
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["access_TOKEN"] != redacted || decoded["safe"] != "visible" {
		t.Fatalf("unexpected top-level fields: %#v", decoded)
	}
	credentials := decoded["credentials"].(map[string]any)
	if credentials["refresh_token"] != redacted {
		t.Fatalf("nested group not redacted: %#v", credentials)
	}
	payload := decoded["payload"].(map[string]any)
	if payload["nested"].(map[string]any)["initData"] != redacted || payload["list"].([]any)[0].(map[string]any)["authorization"] != redacted {
		t.Fatalf("nested map not redacted: %#v", payload)
	}
}

func TestRedactsBoundAttributes(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &output).With("api_key", "stored-secret")
	logger.Info("bound")
	if strings.Contains(output.String(), "stored-secret") || !strings.Contains(output.String(), redacted) {
		t.Fatalf("bound attribute was not redacted: %s", output.String())
	}
}
