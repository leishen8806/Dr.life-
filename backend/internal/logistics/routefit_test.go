package logistics

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRouteFitWebhookVerifierAcceptsRotatedSignature(t *testing.T) {
	body := []byte(`{"eventId":"evt_01","eventType":"fulfillment.shipped","schemaVersion":1,"occurredAt":"2026-10-29T08:00:00Z","aggregate":{"type":"fulfillment","id":"ful_01","version":2},"payload":{}}`)
	now := time.Unix(1_793_260_800, 0).UTC()
	request := httptest.NewRequest(http.MethodPost, "/v1/integrations/routefit/events", strings.NewReader(string(body)))
	request.Header.Set("X-RouteFit-Timestamp", "1793260800")
	request.Header.Set("X-RouteFit-Signature", "v1=bad, v1="+routeFitSignature("new-secret", "1793260800", body))

	verifier := RouteFitWebhookVerifier{Secrets: [][]byte{[]byte("old-secret"), []byte("new-secret")}, Now: func() time.Time { return now }}
	if err := verifier.Verify(request, body); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestRouteFitWebhookVerifierRejectsExpiredAndWrongSignature(t *testing.T) {
	body := []byte(`{}`)
	now := time.Unix(1_793_260_800, 0).UTC()
	verifier := RouteFitWebhookVerifier{Secrets: [][]byte{[]byte("secret")}, Now: func() time.Time { return now }}
	for name, header := range map[string]string{
		"expired": "v1=" + routeFitSignature("secret", "1793260400", body),
		"wrong":   "v1=" + routeFitSignature("other", "1793260800", body),
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/integrations/routefit/events", strings.NewReader(string(body)))
			request.Header.Set("X-RouteFit-Timestamp", "1793260800")
			if name == "expired" {
				request.Header.Set("X-RouteFit-Timestamp", "1793260400")
			}
			request.Header.Set("X-RouteFit-Signature", header)
			if err := verifier.Verify(request, body); err == nil {
				t.Fatal("Verify() succeeded, want rejection")
			}
		})
	}
}

func routeFitSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
