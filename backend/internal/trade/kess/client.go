package kess

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var ErrConfig = errors.New("KESS configuration is incomplete or unsafe")

const (
	CreateServiceLower = "webpay.acquire.createorder"
	CreateServiceCamel = "webpay.acquire.createOrder"
	QueryService       = "webpay.acquire.queryOrder"
)

// Config must be assembled from protected server environment variables. The
// create-service spelling stays explicit until KESS confirms the sandbox form.
type Config struct {
	BaseURL       string
	ClientID      string
	ClientSecret  string
	Username      string
	Password      string
	SigningKey    string
	SellerCode    string
	CreateService string
}

type HTTPClient struct {
	config Config
	http   *http.Client
	mu     sync.Mutex
	token  string
	expiry time.Time
}

func NewHTTPClient(config Config, client *http.Client) (*HTTPClient, error) {
	if config.BaseURL == "" {
		config.BaseURL = "https://" + sandboxHost
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
	}
	return &HTTPClient{config: config, http: client}, nil
}

func (c *HTTPClient) Create(ctx context.Context, intent Intent) (ChannelOrder, error) {
	amount, err := FormatAmount(intent.Amount, intent.Currency)
	if err != nil {
		return ChannelOrder{}, err
	}
	data, err := c.gateway(ctx, map[string]string{
		"service": c.config.CreateService, "sign_type": "HMAC-SHA256", "seller_code": c.config.SellerCode,
		"out_trade_no": intent.TradeNo, "body": "Dr.life+ order", "total_amount": amount, "currency": intent.Currency, "login_type": "ANONYMOUS",
	})
	if err != nil {
		return ChannelOrder{}, err
	}
	return decodeOrder(data)
}

func (c *HTTPClient) Query(ctx context.Context, intent Intent) (ChannelOrder, error) {
	data, err := c.gateway(ctx, map[string]string{"service": QueryService, "sign_type": "HMAC-SHA256", "out_trade_no": intent.TradeNo})
	if err != nil {
		return ChannelOrder{}, err
	}
	return decodeOrder(data)
}

func (c *HTTPClient) gateway(ctx context.Context, fields map[string]string) (json.RawMessage, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	fields["sign"] = SignScalars(fields, c.config.SigningKey)
	var response struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := c.post(ctx, "/api/mch/v2/gateway", fields, token, &response); err != nil {
		return nil, err
	}
	if !response.Success || len(response.Data) == 0 || string(response.Data) == "null" {
		return nil, ErrInvalidChannel
	}
	return response.Data, nil
}

func (c *HTTPClient) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
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
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("KESS request failed: HTTP %d", resp.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(output); err != nil {
		return ErrInvalidChannel
	}
	return nil
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
	return ChannelOrder{TradeNo: response.TradeNo, SellerCode: response.Seller.Code, Amount: amount, Currency: response.Currency, Status: response.Status, TransactionID: response.TransactionID, PaymentURL: response.PaymentLink}, nil
}
