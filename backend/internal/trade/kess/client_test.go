package kess

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestHTTPClientRejectsDisabledCertificateVerification(t *testing.T) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 -- verifies the client rejects this configuration.
	_, err := NewHTTPClient(validConfig(CreateServiceLower), &http.Client{Transport: transport})
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("NewHTTPClient error = %v, want ErrConfig", err)
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
			_, _ = io.WriteString(writer, `{"success":"true","data":{"out_trade_no":"DRL-01","seller":{"code":"seller"},"total_amount":"12.34","currency":"USD","status":"WAITING","payment_link":"https://devwebpayment.kesspay.io/pay/a"}}`)
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewHTTPClient(validConfig(CreateServiceCamel), sandboxHTTPClient(t, server))
	if err != nil {
		t.Fatal(err)
	}
	order, err := client.Create(context.Background(), Intent{TradeNo: "DRL-01", AmountE4: 123400, Currency: "USD", ExpectedSellerCode: "seller"})
	if err != nil || order.AmountE4 != 123400 || order.Status != "WAITING" {
		t.Fatalf("Create = %+v, %v", order, err)
	}
}

func TestHTTPClientRejectsTamperedNestedDataWithValidTopLevelSignature(t *testing.T) {
	var logs bytes.Buffer
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/token":
			_, _ = io.WriteString(writer, `{"access_token":"token","token_type":"Bearer","expires_in":60}`)
		case "/api/mch/v2/gateway":
			_, _ = io.WriteString(writer, `{"success":true,"data":{"out_trade_no":"DRL-01","seller":{"code":"seller"},"total_amount":"99.99","currency":"USD","status":"SUCCESS","transaction_id":"forged"}}`)
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	config := validConfig(CreateServiceLower)
	config.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	client, err := NewHTTPClient(config, sandboxHTTPClient(t, server))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Create(context.Background(), Intent{TradeNo: "DRL-01", AmountE4: 123400, Currency: "USD", ExpectedSellerCode: "seller"})
	if !errors.Is(err, ErrInvalidChannel) {
		t.Fatalf("Create error = %v, want ErrInvalidChannel", err)
	}
	if !strings.Contains(logs.String(), "KESS response order rejected") {
		t.Fatalf("missing sanitized mismatch alert log: %s", logs.String())
	}
}

func TestHTTPClientAllowsUnsignedResponseButRejectsInvalidProvidedSignature(t *testing.T) {
	validSign := signScalars(map[string]string{"success": "true"}, "signing-key", SignMD5)
	for _, test := range []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "unsigned string success", body: `{"success":"true","data":{"out_trade_no":"DRL-01","seller":{"code":"seller"},"total_amount":"12.34","currency":"USD","status":"WAITING","payment_link":"https://devwebpayment.kesspay.io/pay/a"}}`},
		{name: "valid provided signature", body: fmt.Sprintf(`{"success":true,"sign":"%s","data":{"out_trade_no":"DRL-01","seller":{"code":"seller"},"total_amount":"12.34","currency":"USD","status":"WAITING","payment_link":"https://devwebpayment.kesspay.io/pay/a"}}`, validSign)},
		{name: "invalid provided signature", body: `{"success":true,"sign":"invalid","data":{"out_trade_no":"DRL-01","seller":{"code":"seller"},"total_amount":"12.34","currency":"USD","status":"WAITING","payment_link":"https://devwebpayment.kesspay.io/pay/a"}}`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/oauth/token" {
					_, _ = io.WriteString(writer, `{"access_token":"token","token_type":"Bearer","expires_in":60}`)
					return
				}
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			client, err := NewHTTPClient(validConfig(CreateServiceLower), sandboxHTTPClient(t, server))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Create(context.Background(), Intent{TradeNo: "DRL-01", AmountE4: 123400, Currency: "USD", ExpectedSellerCode: "seller"})
			if test.wantErr && !errors.Is(err, ErrInvalidChannel) {
				t.Fatalf("Create error = %v, want ErrInvalidChannel", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("Create error = %v, want nil", err)
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

func TestKESSMD5SignatureUsesSortedRequestFieldsAndAppendedKey(t *testing.T) {
	if got := signScalars(map[string]string{"b": "2", "a": "1"}, "key", SignMD5); got != "735a0bafc42420a9b223ce31415e043a" {
		t.Fatalf("MD5 signature = %s", got)
	}
}

func TestHTTPClientRetriesOnlyDialFailure(t *testing.T) {
	var calls atomic.Int32
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		call := calls.Add(1)
		if call == 1 {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("test dial failure")}
		}
		body := `{"access_token":"token","token_type":"Bearer","expires_in":60}`
		if request.URL.Path != "/oauth/token" {
			body = `{"success":true,"data":{"out_trade_no":"DRL-01","seller":{"code":"seller"},"total_amount":"1.00","currency":"USD","status":"WAITING","payment_link":"https://devwebpayment.kesspay.io/pay/a"}}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	config := validConfig(CreateServiceLower)
	config.MaxRetries = intPtr(1)
	client, err := NewHTTPClient(config, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Create(context.Background(), Intent{TradeNo: "DRL-01", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("HTTP calls = %d, want 3 (dial failure + token retry + gateway)", calls.Load())
	}
}

func TestHTTPClientAllowsZeroRetries(t *testing.T) {
	var calls atomic.Int32
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("test dial failure")}
	})
	config := validConfig(CreateServiceLower)
	config.MaxRetries = intPtr(0)
	client, err := NewHTTPClient(config, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.Create(context.Background(), Intent{TradeNo: "DRL-01", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller"})
	if calls.Load() != 1 {
		t.Fatalf("HTTP calls = %d, want 1 with zero retries", calls.Load())
	}
}

func TestHTTPClientClassifiesPreGatewayFailuresAsNotSent(t *testing.T) {
	client, err := NewHTTPClient(validConfig(CreateServiceLower), &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unauthorized")), Request: request}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Create(context.Background(), Intent{AmountE4: 1, Currency: "USD"}); !errors.Is(err, ErrRequestNotSent) {
		t.Fatalf("format error=%v, want ErrRequestNotSent", err)
	}
	if _, err := client.Create(context.Background(), Intent{TradeNo: "DRL-01", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller"}); !errors.Is(err, ErrRequestNotSent) {
		t.Fatalf("token error=%v, want ErrRequestNotSent", err)
	}
	client.openUntil = time.Now().Add(time.Minute)
	if _, err := client.Create(context.Background(), Intent{TradeNo: "DRL-01", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller"}); !errors.Is(err, ErrRequestNotSent) {
		t.Fatalf("circuit error=%v, want ErrRequestNotSent", err)
	}
}

func TestHTTPNotFoundIsNeverAQueryOrderBusinessResult(t *testing.T) {
	for _, test := range []struct {
		name        string
		cachedToken bool
	}{
		{name: "token endpoint"},
		{name: "gateway endpoint", cachedToken: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewHTTPClient(validConfig(CreateServiceLower), &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not found")), Request: request}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			if test.cachedToken {
				client.token, client.expiry = "token", time.Now().Add(time.Minute)
			}
			_, err = client.Query(context.Background(), Intent{TradeNo: "DRL-01", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller"})
			if err == nil || errors.Is(err, ErrChannelMissing) || !strings.Contains(err.Error(), "HTTP 404") {
				t.Fatalf("Query error = %v, want retryable HTTP 404 and not ErrChannelMissing", err)
			}
		})
	}
}

func TestHTTPClientCircuitOpensAfterRepeatedSentFailures(t *testing.T) {
	var calls atomic.Int32
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("bad gateway")), Request: request}, nil
	})
	config := validConfig(CreateServiceLower)
	config.CircuitFailures = 2
	config.CircuitOpenFor = time.Minute
	config.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := NewHTTPClient(config, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	intent := Intent{TradeNo: "DRL-01", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller"}
	for range 3 {
		_, _ = client.Create(context.Background(), intent)
	}
	if calls.Load() != 2 {
		t.Fatalf("HTTP calls = %d, want 2 before circuit opens", calls.Load())
	}
}

func TestHTTPClientCircuitAllowsOneHalfOpenProbe(t *testing.T) {
	var calls atomic.Int32
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		call := calls.Add(1)
		if call == 1 {
			return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("bad gateway")), Request: request}, nil
		}
		body := `{"access_token":"token","token_type":"Bearer","expires_in":60}`
		if request.URL.Path != "/oauth/token" {
			body = `{"success":true,"data":{"out_trade_no":"DRL-01","seller":{"code":"seller"},"total_amount":"1.00","currency":"USD","status":"WAITING","payment_link":"https://devwebpayment.kesspay.io/pay/a"}}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	config := validConfig(CreateServiceLower)
	config.CircuitFailures = 1
	config.CircuitOpenFor = time.Millisecond
	client, err := NewHTTPClient(config, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	intent := Intent{TradeNo: "DRL-01", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller"}
	_, _ = client.Create(context.Background(), intent)
	_, _ = client.Create(context.Background(), intent)
	time.Sleep(5 * time.Millisecond)
	if _, err := client.Create(context.Background(), intent); err != nil {
		t.Fatalf("half-open probe error = %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("HTTP calls = %d, want failed token plus successful token and gateway", calls.Load())
	}
}

func TestHTTPClientCircuitAllowsOnlyOneConcurrentHalfOpenProbe(t *testing.T) {
	client, err := NewHTTPClient(validConfig(CreateServiceLower), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.openUntil = time.Now().Add(-time.Second)
	if err := client.circuitReady(); err != nil {
		t.Fatalf("first half-open probe error=%v", err)
	}
	if err := client.circuitReady(); err == nil {
		t.Fatal("second concurrent half-open probe was allowed")
	}
	client.recordSuccess()
	if err := client.circuitReady(); err != nil {
		t.Fatalf("closed circuit error=%v", err)
	}
}

func TestHTTPClientRequestConstructionFailureClosesHalfOpenProbe(t *testing.T) {
	client, err := NewHTTPClient(validConfig(CreateServiceLower), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.openUntil = time.Now().Add(-time.Second)
	if err := client.post(context.Background(), "\n", map[string]string{"a": "b"}, "", &struct{}{}); !errors.Is(err, ErrRequestNotSent) {
		t.Fatalf("post error = %v, want ErrRequestNotSent", err)
	}
	if client.halfOpen || !client.openUntil.After(time.Now()) {
		t.Fatalf("half-open state stuck: halfOpen=%v openUntil=%v", client.halfOpen, client.openUntil)
	}
}

func TestHTTPClientAcceptsConfiguredPaymentURLHost(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/oauth/token" {
			_, _ = io.WriteString(writer, `{"access_token":"token","token_type":"Bearer","expires_in":60}`)
			return
		}
		_, _ = io.WriteString(writer, `{"success":true,"data":{"out_trade_no":"DRL-01","seller":{"code":"seller"},"total_amount":"1.00","currency":"USD","status":"WAITING","payment_link":"https://pay.example.test/pay/a"}}`)
	}))
	defer server.Close()
	config := validConfig(CreateServiceLower)
	config.PaymentURLHost = "pay.example.test"
	client, err := NewHTTPClient(config, sandboxHTTPClient(t, server))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Create(context.Background(), Intent{TradeNo: "DRL-01", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller"}); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPClientRecognizesQueryNotFoundBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/oauth/token" {
			_, _ = io.WriteString(writer, `{"access_token":"token","token_type":"Bearer","expires_in":60}`)
			return
		}
		_, _ = io.WriteString(writer, `{"success":true,"data":{"status":"NOT_FOUND"}}`)
	}))
	defer server.Close()
	client, err := NewHTTPClient(validConfig(CreateServiceLower), sandboxHTTPClient(t, server))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Query(context.Background(), Intent{TradeNo: "DRL-01", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller"})
	if !errors.Is(err, ErrChannelMissing) {
		t.Fatalf("Query error = %v, want ErrChannelMissing", err)
	}
}

func validConfig(service string) Config {
	return Config{ClientID: "id", ClientSecret: "client-secret", Username: "user", Password: "password", SigningKey: "signing-key", SellerCode: "seller", CreateService: service}
}

func intPtr(value int) *int { return &value }

// sandboxHTTPClient routes the fixed sandbox host to a local TLS server. The
// client therefore exercises its real request/response decoding path without
// any network access to KESS.
func sandboxHTTPClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	transport := server.Client().Transport.(*http.Transport).Clone()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12}
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	return &http.Client{Transport: transport}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
