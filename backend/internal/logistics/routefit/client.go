// Package routefit delivers a paid order command to RouteFit once.
package routefit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	BaseURL           string
	ServiceToken      string
	Timeout           time.Duration
	RetryAttempts     int
	RetryInitialDelay time.Duration
}

type Money struct {
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
}

type Line struct {
	Ref      string `json:"lineRef"`
	SKU      string `json:"skuId"`
	Quantity int64  `json:"quantity"`
}

// CreateShipmentCmd is the seam consumed by the future logistics outbox.
type CreateShipmentCmd struct {
	ExternalOrderID         string          `json:"externalOrderId"`
	OwnerRef                string          `json:"ownerRef"`
	WarehouseID             string          `json:"warehouseId"`
	Lines                   []Line          `json:"lines"`
	Delivery                json.RawMessage `json:"delivery"`
	RequestedDeliveryWindow json.RawMessage `json:"requestedDeliveryWindow"`
	ServiceLevel            string          `json:"serviceLevel"`
	DeclaredValue           Money           `json:"declaredValue"`
}

type Result struct {
	FulfillmentID   string `json:"fulfillmentId"`
	Version         int64  `json:"version"`
	ExternalOrderID string `json:"externalOrderId"`
	State           string `json:"state"`
	Duplicate       bool   `json:"duplicate"`
}

type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string {
	return fmt.Sprintf("routefit request failed: status=%d code=%s", e.Status, e.Code)
}

type Client struct {
	baseURL url.URL
	token   string
	http    *http.Client
	timeout time.Duration
	attempt int
	delay   time.Duration
}

func New(config Config) (*Client, error) {
	baseURL, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("ROUTEFIT_BASE_URL must be an absolute URL")
	}
	if baseURL.Scheme == "http" && !loopbackHost(baseURL.Hostname()) {
		return nil, errors.New("ROUTEFIT_BASE_URL must use https outside loopback")
	}
	if strings.TrimSpace(config.ServiceToken) == "" {
		return nil, errors.New("ROUTEFIT_SERVICE_TOKEN must be configured")
	}
	if config.Timeout <= 0 {
		return nil, errors.New("ROUTEFIT_TIMEOUT must be positive")
	}
	if config.RetryAttempts <= 0 {
		config.RetryAttempts = 3
	}
	if config.RetryInitialDelay <= 0 {
		config.RetryInitialDelay = 100 * time.Millisecond
	}
	return &Client{baseURL: *baseURL, token: config.ServiceToken, http: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, timeout: config.Timeout, attempt: config.RetryAttempts, delay: config.RetryInitialDelay}, nil
}

// Deliver makes one idempotent delivery action suitable for a caller-owned outbox.
func (c *Client) Deliver(ctx context.Context, cmd CreateShipmentCmd) (Result, error) {
	if err := validate(cmd); err != nil {
		return Result{}, err
	}
	key := idempotencyKey(cmd.ExternalOrderID)
	for attempt := 0; attempt < c.attempt; attempt++ {
		result, timedOut, err := c.create(ctx, cmd, key)
		if err == nil {
			return result, nil
		}
		if timedOut {
			found, lookupErr := c.lookup(ctx, cmd.ExternalOrderID)
			if lookupErr == nil && found.FulfillmentID != "" {
				return found, nil
			}
			if lookupErr != nil && !retryable(lookupErr) {
				return Result{}, lookupErr
			}
		}
		if !retryable(err) || attempt+1 == c.attempt {
			return Result{}, err
		}
		if err := wait(ctx, c.delay<<attempt); err != nil {
			return Result{}, err
		}
	}
	return Result{}, errors.New("routefit retry loop exhausted")
}

func (c *Client) create(ctx context.Context, cmd CreateShipmentCmd, key string) (Result, bool, error) {
	body, _ := json.Marshal(struct {
		CreateShipmentCmd
		ExternalRevision int64  `json:"externalRevision"`
		PaymentTerms     string `json:"paymentTerms"`
		SourcePlatform   string `json:"sourcePlatform"`
	}{cmd, 1, "PLATFORM_PREPAID", "drlife"})
	response, err := c.request(ctx, http.MethodPost, "/api/platform/v1/fulfillments", body, key)
	if err != nil {
		return Result{}, errors.Is(err, context.DeadlineExceeded), err
	}
	return response, false, nil
}

func (c *Client) lookup(ctx context.Context, externalOrderID string) (Result, error) {
	path := "/api/platform/v1/fulfillments?externalOrderId=" + url.QueryEscape(externalOrderID)
	return c.request(ctx, http.MethodGet, path, nil, "")
}

func (c *Client) request(parent context.Context, method, path string, body []byte, key string) (Result, error) {
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()
	endpoint := c.baseURL
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + strings.Split(path, "?")[0]
	if _, query, ok := strings.Cut(path, "?"); ok {
		endpoint.RawQuery = query
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return Result{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound && method == http.MethodGet {
		return Result{}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Result{}, decodeError(response)
	}
	var result Result
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return Result{}, fmt.Errorf("decode RouteFit response: %w", err)
	}
	return result, nil
}

func decodeError(response *http.Response) error {
	var payload struct {
		Error struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload)
	return &Error{Status: response.StatusCode, Code: payload.Error.Code}
}

func validate(cmd CreateShipmentCmd) error {
	if cmd.ExternalOrderID == "" || cmd.OwnerRef == "" || cmd.WarehouseID == "" || len(cmd.Lines) == 0 || len(cmd.Delivery) == 0 || cmd.ServiceLevel == "" || cmd.DeclaredValue.Currency == "" {
		return errors.New("invalid RouteFit shipment command")
	}
	for _, line := range cmd.Lines {
		if line.Ref == "" || line.SKU == "" || line.Quantity <= 0 {
			return errors.New("invalid RouteFit shipment line")
		}
	}
	return nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func idempotencyKey(externalOrderID string) string {
	sum := sha256.Sum256([]byte("routefit.fulfillment:v1:" + externalOrderID))
	return hex.EncodeToString(sum[:])
}

func retryable(err error) bool {
	var routefitErr *Error
	if errors.As(err, &routefitErr) {
		return routefitErr.Status == http.StatusTooManyRequests || routefitErr.Status >= 500
	}
	return errors.Is(err, context.DeadlineExceeded)
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
