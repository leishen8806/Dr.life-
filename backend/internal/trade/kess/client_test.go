package kess

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPClientRejectsMissingConfigAndNonSandboxEndpointWithoutLeakingSecrets(t *testing.T) {
	secret := "secret-must-not-appear"
	for _, config := range []Config{
		{CreateService: CreateServiceLower},
		{BaseURL: "https://payments.example.test", ClientID: "id", ClientSecret: secret, Username: "user", Password: secret, SigningKey: secret, SellerCode: "seller", CreateService: CreateServiceLower},
		{ClientID: "id", ClientSecret: secret, Username: "user", Password: secret, SigningKey: secret, SellerCode: "seller", CreateService: "createOrder"},
	} {
		_, err := NewHTTPClient(config, nil)
		if !errors.Is(err, ErrConfig) {
			t.Fatalf("NewHTTPClient error = %v, want ErrConfig", err)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("configuration error leaked secret: %v", err)
		}
	}
}

func TestHTTPClientUsesOAuthBearerAndConfiguredCreateService(t *testing.T) {
	transport := roundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != sandboxHost {
			t.Fatalf("host = %q, want %q", request.URL.Host, sandboxHost)
		}
		switch request.URL.Path {
		case "/oauth/token":
			return jsonResponse(`{"access_token":"token","token_type":"Bearer","expires_in":60}`), nil
		case "/api/mch/v2/gateway":
			if request.Header.Get("Authorization") != "Bearer token" {
				t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
			}
			body, _ := io.ReadAll(request.Body)
			if !strings.Contains(string(body), `"service":"webpay.acquire.createOrder"`) || !strings.Contains(string(body), `"sign"`) {
				t.Fatalf("gateway body missing configured service/sign: %s", body)
			}
			return jsonResponse(gatewayResponse(t, `{"out_trade_no":"DRL-01","seller":{"code":"seller"},"total_amount":"12.34","currency":"USD","status":"WAITING","payment_link":"https://devwebpayment.kesspay.io/pay/a"}`)), nil
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
			return nil, nil
		}
	})
	client, err := NewHTTPClient(validConfig(CreateServiceCamel), &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	order, err := client.Create(context.Background(), Intent{TradeNo: "DRL-01", Amount: 1234, Currency: "USD", ExpectedSellerCode: "seller"})
	if err != nil || order.Amount != 1234 || order.Status != "WAITING" {
		t.Fatalf("Create = %+v, %v", order, err)
	}
}

func TestHTTPClientRejectsTamperedNestedDataWithValidTopLevelSignature(t *testing.T) {
	transport := roundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/oauth/token":
			return jsonResponse(`{"access_token":"token","token_type":"Bearer","expires_in":60}`), nil
		case "/api/mch/v2/gateway":
			// The envelope still has a valid signature because data is intentionally
			// excluded from the official scalar signing set.
			return jsonResponse(gatewayResponse(t, `{"out_trade_no":"DRL-01","seller":{"code":"seller"},"total_amount":"99.99","currency":"USD","status":"SUCCESS","transaction_id":"forged"}`)), nil
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
			return nil, nil
		}
	})
	client, err := NewHTTPClient(validConfig(CreateServiceLower), &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Create(context.Background(), Intent{TradeNo: "DRL-01", Amount: 1234, Currency: "USD", ExpectedSellerCode: "seller"})
	if !errors.Is(err, ErrInvalidChannel) {
		t.Fatalf("Create error = %v, want ErrInvalidChannel", err)
	}
}

func TestHTTPClientRejectsMissingOrInvalidGatewaySignature(t *testing.T) {
	for _, body := range []string{
		`{"success":true,"data":{"out_trade_no":"DRL-01","seller":{"code":"seller"},"total_amount":"12.34","currency":"USD","status":"WAITING","payment_link":"https://devwebpayment.kesspay.io/pay/a"}}`,
		`{"success":true,"sign":"invalid","data":{"out_trade_no":"DRL-01","seller":{"code":"seller"},"total_amount":"12.34","currency":"USD","status":"WAITING","payment_link":"https://devwebpayment.kesspay.io/pay/a"}}`,
	} {
		t.Run("reject", func(t *testing.T) {
			transport := roundTripper(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == "/oauth/token" {
					return jsonResponse(`{"access_token":"token","token_type":"Bearer","expires_in":60}`), nil
				}
				return jsonResponse(body), nil
			})
			client, err := NewHTTPClient(validConfig(CreateServiceLower), &http.Client{Transport: transport})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Create(context.Background(), Intent{TradeNo: "DRL-01", Amount: 1234, Currency: "USD", ExpectedSellerCode: "seller"})
			if !errors.Is(err, ErrInvalidChannel) {
				t.Fatalf("Create error = %v, want ErrInvalidChannel", err)
			}
		})
	}
}

func TestSignScalarsExcludesEmptyAndNestedData(t *testing.T) {
	fields := map[string]string{"b": "2", "a": "1", "sign": "old", "empty": ""}
	got := SignScalars(fields, "key")
	if got != "38c84c553479d2d3b591ed2feba712a2e26ba00baf02402a637bb59af8a377bd" {
		t.Fatalf("signature = %s", got)
	}
}

func validConfig(service string) Config {
	return Config{ClientID: "id", ClientSecret: "client-secret", Username: "user", Password: "password", SigningKey: "signing-key", SellerCode: "seller", CreateService: service}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (fn roundTripper) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func jsonResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func gatewayResponse(t *testing.T, data string) string {
	t.Helper()
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		t.Fatal(err)
	}
	response := map[string]any{"success": true, "sign": SignScalars(map[string]string{"success": "true"}, "signing-key"), "data": raw}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
