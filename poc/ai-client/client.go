// Package aiclient is a local PoC for OpenAI-compatible chat-completion fallback.
package aiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTimeout    = 2 * time.Second
	defaultRetryDelay = 50 * time.Millisecond
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Model      string    `json:"model,omitempty"`
	Messages   []Message `json:"messages"`
	Idempotent bool      `json:"-"`
}

type Endpoint struct {
	Name    string
	BaseURL string
	Model   string
}

type Result struct {
	Content  string
	Endpoint string
	Fallback bool
}

type Config struct {
	Endpoints  []Endpoint
	Timeout    time.Duration
	MaxRetries int
	RetryDelay time.Duration
	Fallback   func(Request) Result
}

type Client struct {
	config Config
	http   *http.Client
}

func New(config Config) *Client {
	if config.Timeout <= 0 {
		config.Timeout = defaultTimeout
	}
	if config.MaxRetries < 0 {
		config.MaxRetries = 0
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = defaultRetryDelay
	}
	return &Client{config: config, http: &http.Client{}}
}

func (c *Client) Complete(ctx context.Context, request Request) (Result, error) {
	if len(c.config.Endpoints) == 0 {
		return c.local(request, fmt.Errorf("no endpoints configured"))
	}

	var lastErr error
	for _, endpoint := range c.config.Endpoints {
		attempts := 1
		if request.Idempotent {
			attempts += c.config.MaxRetries
		}
		for attempt := 0; attempt < attempts; attempt++ {
			result, err := c.completeEndpoint(ctx, endpoint, request)
			if err == nil {
				return result, nil
			}
			lastErr = err
			if attempt+1 < attempts {
				if err := wait(ctx, c.config.RetryDelay); err != nil {
					lastErr = err
					break
				}
			}
		}
		if !request.Idempotent {
			break
		}
	}
	return c.local(request, lastErr)
}

func (c *Client) local(request Request, cause error) (Result, error) {
	if c.config.Fallback == nil {
		return Result{}, cause
	}
	result := c.config.Fallback(request)
	result.Fallback = true
	if result.Endpoint == "" {
		result.Endpoint = "local-rule"
	}
	return result, nil
}

func (c *Client) completeEndpoint(parent context.Context, endpoint Endpoint, request Request) (Result, error) {
	body, err := json.Marshal(struct {
		Model    string    `json:"model,omitempty"`
		Messages []Message `json:"messages"`
	}{Model: firstNonEmpty(request.Model, endpoint.Model), Messages: request.Messages})
	if err != nil {
		return Result{}, fmt.Errorf("encode request: %w", err)
	}
	ctx, cancel := context.WithTimeout(parent, c.config.Timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint.BaseURL, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("create request for %s: %w", endpoint.Name, err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(httpRequest)
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", endpoint.Name, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Result{}, fmt.Errorf("call %s: unexpected HTTP status %d", endpoint.Name, response.StatusCode)
	}
	var payload struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return Result{}, fmt.Errorf("decode %s response: %w", endpoint.Name, err)
	}
	if len(payload.Choices) == 0 || payload.Choices[0].Message.Content == "" {
		return Result{}, fmt.Errorf("decode %s response: missing message content", endpoint.Name)
	}
	return Result{Content: payload.Choices[0].Message.Content, Endpoint: endpoint.Name}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
