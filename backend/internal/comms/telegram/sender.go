// Package telegram contains the outbound Telegram Bot API adapter.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type ResultKind uint8

const (
	ResultSuccess ResultKind = iota
	ResultRateLimited
	ResultUserBlocked
	ResultChatNotFound
	ResultTemporaryError
	ResultPermanentError
)

type Result struct {
	Kind       ResultKind
	RetryAfter time.Duration
	Err        error
}

type SendOptions struct{}

type Sender interface {
	SendMessage(context.Context, string, int64, string, SendOptions) Result
}

type TokenProvider interface {
	Token(context.Context, string) (string, error)
}

type StaticTokenProvider map[string]string

func (p StaticTokenProvider) Token(_ context.Context, botID string) (string, error) {
	token := p[botID]
	if token == "" {
		return "", errors.New("telegram bot token unavailable")
	}
	return token, nil
}

type SenderOptions struct {
	Timeout time.Duration
}

type HTTPSender struct {
	baseURL string
	tokens  TokenProvider
	client  *http.Client
	mu      sync.Mutex
}

func NewHTTPSender(baseURL string, tokens TokenProvider, options SenderOptions) *HTTPSender {
	if options.Timeout <= 0 {
		options.Timeout = 10 * time.Second
	}
	return &HTTPSender{baseURL: strings.TrimRight(baseURL, "/"), tokens: tokens, client: &http.Client{Timeout: options.Timeout}}
}

func (s *HTTPSender) SendMessage(ctx context.Context, botID string, chatID int64, text string, _ SendOptions) Result {
	token, err := s.tokens.Token(ctx, botID)
	if err != nil {
		return Result{Kind: ResultPermanentError, Err: err}
	}
	body, err := json.Marshal(struct {
		ChatID int64  `json:"chat_id"`
		Text   string `json:"text"`
	}{ChatID: chatID, Text: text})
	if err != nil {
		return Result{Kind: ResultPermanentError, Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/bot"+token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return Result{Kind: ResultPermanentError, Err: scrubURLError(err)}
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return Result{Kind: ResultTemporaryError, Err: scrubURLError(err)}
	}
	defer response.Body.Close()
	var payload struct {
		Description string `json:"description"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	_ = json.NewDecoder(response.Body).Decode(&payload)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return Result{Kind: ResultSuccess}
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return Result{Kind: ResultRateLimited, RetryAfter: time.Duration(payload.Parameters.RetryAfter) * time.Second}
	}
	description := strings.ToLower(payload.Description)
	if response.StatusCode == http.StatusForbidden && strings.Contains(description, "blocked") {
		return Result{Kind: ResultUserBlocked}
	}
	if strings.Contains(description, "chat not found") {
		return Result{Kind: ResultChatNotFound}
	}
	if response.StatusCode >= 500 {
		return Result{Kind: ResultTemporaryError}
	}
	return Result{Kind: ResultPermanentError}
}

// scrubURLError drops the request URL from transport and request-building errors: the URL embeds the
// bot token, so a *url.Error must never reach logs or callers.
func scrubURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("telegram request failed: %s: %w", urlErr.Op, urlErr.Err)
	}
	return errors.New("telegram request failed")
}
