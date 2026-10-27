package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPSenderClassifiesTelegramResponses(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantKind  ResultKind
		wantRetry time.Duration
	}{
		{"success", http.StatusOK, `{"ok":true}`, ResultSuccess, 0},
		{"rate limited", http.StatusTooManyRequests, `{"ok":false,"parameters":{"retry_after":3}}`, ResultRateLimited, 3 * time.Second},
		{"blocked", http.StatusForbidden, `{"ok":false,"description":"Forbidden: bot was blocked by the user"}`, ResultUserBlocked, 0},
		{"chat missing", http.StatusBadRequest, `{"ok":false,"description":"Bad Request: chat not found"}`, ResultChatNotFound, 0},
		{"temporary", http.StatusBadGateway, `{"ok":false}`, ResultTemporaryError, 0},
		{"permanent", http.StatusBadRequest, `{"ok":false}`, ResultPermanentError, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			sender := NewHTTPSender(server.URL, StaticTokenProvider{"primary": "test-token"}, SenderOptions{Timeout: time.Second})
			result := sender.SendMessage(context.Background(), "primary", 123, "private message", SendOptions{})
			if result.Kind != tc.wantKind || result.RetryAfter != tc.wantRetry {
				t.Fatalf("result = %#v, want kind %v retry %v", result, tc.wantKind, tc.wantRetry)
			}
		})
	}
}

func TestHTTPSenderErrorsNeverContainTheBotToken(t *testing.T) {
	const token = "123456:SECRET-TOKEN"
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close() // connection refused: the transport error would normally embed the full request URL

	sender := NewHTTPSender(url, StaticTokenProvider{"primary": token}, SenderOptions{Timeout: time.Second})
	result := sender.SendMessage(context.Background(), "primary", 123, "hello", SendOptions{})
	if result.Kind != ResultTemporaryError || result.Err == nil {
		t.Fatalf("result = %#v, want temporary error", result)
	}
	if strings.Contains(result.Err.Error(), token) || strings.Contains(result.Err.Error(), "SECRET") {
		t.Fatalf("error leaks the bot token: %v", result.Err)
	}

	bad := NewHTTPSender("http://bad host", StaticTokenProvider{"primary": token}, SenderOptions{Timeout: time.Second})
	result = bad.SendMessage(context.Background(), "primary", 123, "hello", SendOptions{})
	if result.Err == nil || strings.Contains(result.Err.Error(), token) {
		t.Fatalf("request-building error leaks the bot token: %v", result.Err)
	}
}
