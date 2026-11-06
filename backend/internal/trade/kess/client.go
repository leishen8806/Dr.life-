package kess

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var ErrConfig = errors.New("KESS configuration is incomplete or unsafe")

const (
	CreateServiceLower = "webpay.acquire.createorder"
	CreateServiceCamel = "webpay.acquire.createOrder"
	QueryService       = "webpay.acquire.queryorder"
)

// Config must be assembled from protected server environment variables. The
// create-service spelling stays explicit until KESS confirms the sandbox form.
type Config struct {
	BaseURL         string
	ClientID        string
	ClientSecret    string
	Username        string
	Password        string
	SigningKey      string
	SellerCode      string
	CreateService   string
	SignType        string
	Logger          *slog.Logger
	MaxRetries      *int
	CircuitFailures int
	CircuitOpenFor  time.Duration
	PaymentURLHost  string
}

type HTTPClient struct {
	config    Config
	http      *http.Client
	tokenMu   sync.Mutex
	stateMu   sync.Mutex
	token     string
	expiry    time.Time
	failures  int
	openUntil time.Time
	halfOpen  bool
}

func NewHTTPClient(config Config, client *http.Client) (*HTTPClient, error) {
	if config.BaseURL == "" {
		config.BaseURL = "https://" + sandboxHost
	}
	if config.SignType == "" {
		config.SignType = SignMD5 // Pending confirmation against the KESS sandbox account.
	}
	if config.MaxRetries == nil {
		defaultRetries := 1
		config.MaxRetries = &defaultRetries
	}
	if config.PaymentURLHost == "" {
		config.PaymentURLHost = sandboxHost
	}
	if config.SignType != SignMD5 && config.SignType != SignHMACSHA256 || *config.MaxRetries < 0 || !validPaymentHost(config.PaymentURLHost) {
		return nil, ErrConfig
	}
	if config.CircuitFailures == 0 {
		config.CircuitFailures = 3
	}
	if config.CircuitOpenFor == 0 {
		config.CircuitOpenFor = 30 * time.Second
	}
	if config.CircuitFailures < 1 || config.CircuitOpenFor < 0 {
		return nil, ErrConfig
	}
	if config.Logger == nil {
		config.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if config.BaseURL != "https://"+sandboxHost || config.CreateService != CreateServiceLower && config.CreateService != CreateServiceCamel {
		return nil, ErrConfig
	}
	for _, value := range []string{config.ClientID, config.ClientSecret, config.Username, config.Password, config.SigningKey, config.SellerCode} {
		if strings.TrimSpace(value) == "" {
			return nil, ErrConfig
		}
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	} else {
		if transport, ok := client.Transport.(*http.Transport); ok && transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify {
			return nil, ErrConfig
		}
		copy := *client
		copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &copy
	}
	return &HTTPClient{config: config, http: client}, nil
}

func (c *HTTPClient) Create(ctx context.Context, intent Intent) (ChannelOrder, error) {
	amount, err := FormatAmount(intent.AmountE4, intent.Currency)
	if err != nil {
		return ChannelOrder{}, errors.Join(ErrRequestNotSent, err)
	}
	data, err := c.gateway(ctx, map[string]string{
		"service": c.config.CreateService, "sign_type": c.config.SignType, "seller_code": c.config.SellerCode,
		"out_trade_no": intent.TradeNo, "body": "Dr.life+ order", "total_amount": amount, "currency": intent.Currency, "login_type": "ANONYMOUS",
	})
	if err != nil {
		return ChannelOrder{}, err
	}
	order, err := decodeAndValidateOrder(data, intent, true, c.config.PaymentURLHost)
	if err != nil {
		c.config.Logger.ErrorContext(ctx, "KESS response order rejected", "operation", "createorder")
	}
	return order, err
}

func (c *HTTPClient) Query(ctx context.Context, intent Intent) (ChannelOrder, error) {
	data, err := c.gateway(ctx, map[string]string{"service": QueryService, "sign_type": c.config.SignType, "out_trade_no": intent.TradeNo})
	if err != nil {
		return ChannelOrder{}, err
	}
	if queryOrderMissing(data) {
		return ChannelOrder{}, ErrChannelMissing
	}
	order, err := decodeAndValidateOrder(data, intent, false, c.config.PaymentURLHost)
	if err != nil {
		c.config.Logger.ErrorContext(ctx, "KESS response order rejected", "operation", "queryorder")
	}
	return order, err
}

func (c *HTTPClient) gateway(ctx context.Context, fields map[string]string) (json.RawMessage, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, errors.Join(ErrRequestNotSent, err)
	}
	fields["sign"] = signScalars(fields, c.config.SigningKey, c.config.SignType)
	var response json.RawMessage
	if err := c.post(ctx, "/api/mch/v2/gateway", fields, token, &response); err != nil {
		return nil, err
	}
	return verifyGatewayResponse(response, c.config.SigningKey, c.config.SignType, c.config.Logger)
}

func (c *HTTPClient) accessToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.token != "" && time.Now().Add(30*time.Second).Before(c.expiry) {
		return c.token, nil
	}
	var response struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := c.post(ctx, "/oauth/token", map[string]string{"grant_type": "password", "client_id": c.config.ClientID, "client_secret": c.config.ClientSecret, "username": c.config.Username, "password": c.config.Password}, "", &response); err != nil {
		return "", err
	}
	if response.AccessToken == "" || response.TokenType != "Bearer" || response.ExpiresIn <= 30 {
		return "", ErrInvalidChannel
	}
	c.token, c.expiry = response.AccessToken, time.Now().Add(time.Duration(response.ExpiresIn)*time.Second)
	return c.token, nil
}

func (c *HTTPClient) post(ctx context.Context, path string, body any, bearer string, output any) error {
	if err := c.circuitReady(); err != nil {
		return errors.Join(ErrRequestNotSent, err)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return errors.Join(ErrRequestNotSent, err)
	}
	var resp *http.Response
	for attempt := 0; ; attempt++ {
		req, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+path, bytes.NewReader(payload))
		if requestErr != nil {
			return errors.Join(ErrRequestNotSent, requestErr)
		}
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err = c.http.Do(req)
		if err == nil || !definitelyNotSent(err) || attempt >= *c.config.MaxRetries {
			break
		}
		c.config.Logger.WarnContext(ctx, "KESS request retry", "operation", path, "attempt", attempt+1)
	}
	if err != nil {
		c.recordFailure()
		c.config.Logger.WarnContext(ctx, "KESS request failed", "operation", path, "definitely_not_sent", definitelyNotSent(err))
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.recordFailure()
		c.config.Logger.WarnContext(ctx, "KESS response failed", "operation", path, "status_code", resp.StatusCode)
		if resp.StatusCode == http.StatusNotFound {
			return ErrChannelMissing
		}
		return fmt.Errorf("KESS request failed: HTTP %d", resp.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(output); err != nil {
		c.recordFailure()
		c.config.Logger.WarnContext(ctx, "KESS response decode failed", "operation", path)
		return ErrInvalidChannel
	}
	c.recordSuccess()
	c.config.Logger.DebugContext(ctx, "KESS request completed", "operation", path, "status_code", resp.StatusCode)
	return nil
}

func (c *HTTPClient) circuitReady() error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if time.Now().Before(c.openUntil) {
		return fmt.Errorf("KESS circuit open: %w", ErrInvalidChannel)
	}
	if !c.openUntil.IsZero() {
		if c.halfOpen {
			return fmt.Errorf("KESS circuit half-open probe already active: %w", ErrInvalidChannel)
		}
		c.halfOpen = true
	}
	return nil
}

func (c *HTTPClient) recordFailure() {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.failures++
	if c.halfOpen || c.failures >= c.config.CircuitFailures {
		c.openUntil = time.Now().Add(c.config.CircuitOpenFor)
		c.halfOpen = false
	}
}

func (c *HTTPClient) recordSuccess() {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.failures = 0
	c.openUntil = time.Time{}
	c.halfOpen = false
}

func definitelyNotSent(err error) bool {
	if errors.Is(err, ErrRequestNotSent) {
		return true
	}
	var urlErr *url.Error
	var opErr *net.OpError
	return errors.As(err, &urlErr) && errors.As(urlErr.Err, &opErr) && opErr.Op == "dial"
}

func decodeOrder(raw json.RawMessage) (ChannelOrder, error) {
	var response struct {
		TradeNo string `json:"out_trade_no"`
		Seller  struct {
			Code string `json:"code"`
		} `json:"seller"`
		TotalAmount   string `json:"total_amount"`
		Currency      string `json:"currency"`
		Status        string `json:"status"`
		TransactionID string `json:"transaction_id"`
		PaymentLink   string `json:"payment_link"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return ChannelOrder{}, ErrInvalidChannel
	}
	amount, err := ParseAmount(response.TotalAmount, response.Currency)
	if err != nil {
		return ChannelOrder{}, err
	}
	return ChannelOrder{TradeNo: response.TradeNo, SellerCode: response.Seller.Code, AmountE4: amount, Currency: response.Currency, Status: response.Status, TransactionID: response.TransactionID, PaymentURL: response.PaymentLink}, nil
}

// verifyGatewayResponse authenticates the signed scalar response envelope.
// Its nested data remains separately validated against the persisted intent.
func verifyGatewayResponse(raw json.RawMessage, signingKey, configuredSignType string, logger *slog.Logger) (json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, ErrInvalidChannel
	}
	if signRaw, ok := envelope["sign"]; ok {
		var sign string
		if err := json.Unmarshal(signRaw, &sign); err != nil || sign == "" {
			return nil, ErrInvalidChannel
		}
		fields := map[string]string{"sign": sign}
		for name, value := range envelope {
			if name == "sign" || name == "data" {
				continue
			}
			if scalar, scalarOK := gatewayScalar(value); scalarOK {
				fields[name] = scalar
			}
		}
		signType := configuredSignType
		if value, exists := fields["sign_type"]; exists && value != "" {
			signType = value
		}
		if signType != SignMD5 && signType != SignHMACSHA256 || !verifyScalars(fields, signingKey, signType) {
			return nil, ErrInvalidChannel
		}
	} else {
		logger.Warn("KESS response omitted signature")
	}
	if !gatewaySuccess(envelope["success"]) {
		return nil, ErrInvalidChannel
	}
	data, ok := envelope["data"]
	if !ok || len(data) == 0 || string(data) == "null" {
		return nil, ErrInvalidChannel
	}
	return data, nil
}

func gatewaySuccess(raw json.RawMessage) bool {
	var boolean bool
	if json.Unmarshal(raw, &boolean) == nil {
		return boolean
	}
	var text string
	return json.Unmarshal(raw, &text) == nil && strings.EqualFold(text, "true")
}

func gatewayScalar(raw json.RawMessage) (string, bool) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", false
	}
	switch scalar := value.(type) {
	case string:
		return scalar, true
	case bool:
		if scalar {
			return "true", true
		}
		return "false", true
	case json.Number:
		return scalar.String(), true
	default:
		return "", false
	}
}

func decodeAndValidateOrder(raw json.RawMessage, intent Intent, requireURL bool, paymentURLHost string) (ChannelOrder, error) {
	order, err := decodeOrder(raw)
	if err != nil {
		return ChannelOrder{}, err
	}
	if _, err := validateOrder(intent, order, requireURL, paymentURLHost); err != nil {
		return ChannelOrder{}, err
	}
	return order, nil
}

func queryOrderMissing(raw json.RawMessage) bool {
	var response struct {
		Status string `json:"status"`
	}
	return json.Unmarshal(raw, &response) == nil && response.Status == "NOT_FOUND"
}
