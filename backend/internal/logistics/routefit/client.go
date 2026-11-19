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
	FulfillmentID   string    `json:"fulfillmentId"`
	Version         int64     `json:"version"`
	ExternalOrderID string    `json:"externalOrderId"`
	State           string    `json:"state"`
	Duplicate       bool      `json:"duplicate"`
	QuoteID         string    `json:"quoteId"`
	Total           Money     `json:"total"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

type QuoteCmd struct {
	OwnerRef     string          `json:"ownerRef"`
	WarehouseID  string          `json:"warehouseId"`
	Lines        []Line          `json:"lines"`
	Delivery     json.RawMessage `json:"delivery"`
	ServiceLevel string          `json:"serviceLevel"`
}

type Quote struct {
	QuoteID   string    `json:"quoteId"`
	Version   int64     `json:"version"`
	Total     Money     `json:"total"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type CancelCmd struct {
	FulfillmentID string
	ReasonCode    string `json:"reasonCode"`
	Note          string `json:"note,omitempty"`
	Version       int64
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

// Quote asks RouteFit for a read-only delivery price. Callers may surface an
// error as "shipping pending" without blocking product browsing or checkout.
func (c *Client) Quote(ctx context.Context, cmd QuoteCmd) (Quote, error) {
	if err := validateQuote(cmd); err != nil {
		return Quote{}, err
	}
	body, err := json.Marshal(cmd)
	if err != nil {
		return Quote{}, fmt.Errorf("encode RouteFit quote command: %w", err)
	}
	response, err := c.request(ctx, http.MethodPost, "/api/platform/v1/quotes", body, "")
	if err != nil {
		return Quote{}, err
	}
	return Quote{QuoteID: response.QuoteID, Version: response.Version, Total: response.Total, ExpiresAt: response.ExpiresAt}, nil
}

// Cancel requests cancellation and relies on RouteFit's idempotency contract.
// A 409 INVALID_TRANSITION is returned to the caller for domain handling.
func (c *Client) Cancel(ctx context.Context, cmd CancelCmd) (Result, error) {
	if strings.TrimSpace(cmd.FulfillmentID) == "" || strings.TrimSpace(cmd.ReasonCode) == "" || cmd.Version <= 0 {
		return Result{}, errors.New("invalid RouteFit cancellation command")
	}
	body, err := json.Marshal(struct {
		ReasonCode string `json:"reasonCode"`
		Note       string `json:"note,omitempty"`
	}{cmd.ReasonCode, cmd.Note})
	if err != nil {
		return Result{}, fmt.Errorf("encode RouteFit cancellation command: %w", err)
	}
	path := "/api/platform/v1/fulfillments/" + url.PathEscape(cmd.FulfillmentID) + "/actions/cancel"
	return c.requestWithHeaders(ctx, http.MethodPost, path, body, idempotencyKey("routefit.cancel:v1:"+cmd.FulfillmentID+":"+cmd.ReasonCode), cmd.Version)
}

func (c *Client) create(ctx context.Context, cmd CreateShipmentCmd, key string) (Result, bool, error) {
	// The fulfillment command is persisted in the outbox as jsonb. PostgreSQL
	// may reorder object keys while round-tripping json.RawMessage values, so
	// canonicalize nested JSON before encoding the request. This keeps the
	// request body stable for RouteFit's idempotency contract across retries.
	cmd.Delivery = canonicalJSON(cmd.Delivery)
	cmd.RequestedDeliveryWindow = canonicalJSON(cmd.RequestedDeliveryWindow)
	body, err := json.Marshal(struct {
		CreateShipmentCmd
		ExternalRevision int64  `json:"externalRevision"`
		PaymentTerms     string `json:"paymentTerms"`
		SourcePlatform   string `json:"sourcePlatform"`
	}{cmd, 1, "PLATFORM_PREPAID", "drlife"})
	if err != nil {
		return Result{}, false, fmt.Errorf("encode RouteFit shipment command: %w", err)
	}
	response, err := c.request(ctx, http.MethodPost, "/api/platform/v1/fulfillments", body, key)
	if err != nil {
		return Result{}, errors.Is(err, context.DeadlineExceeded), err
	}
	return response, false, nil
}

func canonicalJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return canonical
}

func (c *Client) lookup(ctx context.Context, externalOrderID string) (Result, error) {
	path := "/api/platform/v1/fulfillments?externalOrderId=" + url.QueryEscape(externalOrderID)
	return c.request(ctx, http.MethodGet, path, nil, "")
}

func (c *Client) request(parent context.Context, method, path string, body []byte, key string) (Result, error) {
	return c.requestWithHeaders(parent, method, path, body, key, 0)
}

func (c *Client) requestWithHeaders(parent context.Context, method, path string, body []byte, key string, version int64) (Result, error) {
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
	if version > 0 {
		request.Header.Set("If-Match", fmt.Sprintf("\"%d\"", version))
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
	if cmd.ExternalOrderID == "" || cmd.OwnerRef == "" || cmd.WarehouseID == "" || len(cmd.Lines) == 0 || len(cmd.Delivery) == 0 || !json.Valid(cmd.Delivery) || (len(cmd.RequestedDeliveryWindow) != 0 && !json.Valid(cmd.RequestedDeliveryWindow)) || cmd.ServiceLevel == "" || cmd.DeclaredValue.Currency == "" {
		return errors.New("invalid RouteFit shipment command")
	}
	for _, line := range cmd.Lines {
		if line.Ref == "" || line.SKU == "" || line.Quantity <= 0 {
			return errors.New("invalid RouteFit shipment line")
		}
	}
	return nil
}

func validateQuote(cmd QuoteCmd) error {
	if cmd.OwnerRef == "" || cmd.WarehouseID == "" || len(cmd.Lines) == 0 || len(cmd.Delivery) == 0 || !json.Valid(cmd.Delivery) || cmd.ServiceLevel == "" {
		return errors.New("invalid RouteFit quote command")
	}
	for _, line := range cmd.Lines {
		if line.Ref == "" || line.SKU == "" || line.Quantity <= 0 {
			return errors.New("invalid RouteFit quote line")
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
