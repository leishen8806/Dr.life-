package kess

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Host != sandboxHost {
			t.Fatalf("host = %q, want %q", request.Host, sandboxHost)
		}
		switch request.URL.Path {
		case "/oauth/token":
			_, _ = io.WriteString(writer, `{"access_token":"token","token_type":"Bearer","expires_in":60}`)
		case "/api/mch/v2/gateway":
			if request.Header.Get("Authorization") != "Bearer token" {
				t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
			}
			body, _ := io.ReadAll(request.Body)
			if !strings.Contains(string(body), `"service":"webpay.acquire.createOrder"`) || !strings.Contains(string(body), `"sign"`) {
				t.Fatalf("gateway body missing configured service/sign: %s", body)
			}
			_, _ = io.WriteString(writer, gatewayResponse(t, `{"out_trade_no":"DRL-01","seller":{"code":"seller"},"total_amount":"12.34","currency":"USD","status":"WAITING","payment_link":"https://devwebpayment.kesspay.io/pay/a"}`))
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewHTTPClient(validConfig(CreateServiceCamel), sandboxHTTPClient(t, server))
	if err != nil {
		t.Fatal(err)
	}
	order, err := client.Create(context.Background(), Intent{TradeNo: "DRL-01", Amount: 1234, Currency: "USD", ExpectedSellerCode: "seller"})
	if err != nil || order.Amount != 1234 || order.Status != "WAITING" {
		t.Fatalf("Create = %+v, %v", order, err)
	}
}

func TestHTTPClientRejectsTamperedNestedDataWithValidTopLevelSignature(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/token":
			_, _ = io.WriteString(writer, `{"access_token":"token","token_type":"Bearer","expires_in":60}`)
		case "/api/mch/v2/gateway":
			// The envelope still has a valid signature because data is intentionally
			// excluded from the official scalar signing set.
			_, _ = io.WriteString(writer, gatewayResponse(t, `{"out_trade_no":"DRL-01","seller":{"code":"seller"},"total_amount":"99.99","currency":"USD","status":"SUCCESS","transaction_id":"forged"}`))
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewHTTPClient(validConfig(CreateServiceLower), sandboxHTTPClient(t, server))
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
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/oauth/token" {
					_, _ = io.WriteString(writer, `{"access_token":"token","token_type":"Bearer","expires_in":60}`)
					return
				}
				_, _ = io.WriteString(writer, body)
			}))
			defer server.Close()
			client, err := NewHTTPClient(validConfig(CreateServiceLower), sandboxHTTPClient(t, server))
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

// sandboxHTTPClient routes the fixed sandbox host to a local TLS server. The
// client therefore exercises its real request/response decoding path without
// any network access to KESS.
func sandboxHTTPClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig.InsecureSkipVerify = true // #nosec G402 -- test-only local TLS server
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	return &http.Client{Transport: transport}
}
