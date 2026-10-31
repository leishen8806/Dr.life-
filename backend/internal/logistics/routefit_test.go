package logistics

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
)

func TestRouteFitReceiverRejectsMalformedEventAfterVerification(t *testing.T) {
	body := []byte(`{"eventId":"evt_01"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/integrations/routefit/events", bytes.NewReader(body))
	request.Header.Set("X-RouteFit-Timestamp", "1793260800")
	request.Header.Set("X-RouteFit-Signature", "v1="+routeFitSignature("secret", "1793260800", body))

	receiver := RouteFitReceiver{
		Verifier: RouteFitWebhookVerifier{Secrets: [][]byte{[]byte("secret")}, Now: func() time.Time { return time.Unix(1_793_260_800, 0).UTC() }},
		Record:   func(*http.Request, RouteFitEvent, []byte) error { return nil },
	}
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, request.WithContext(context.Background()))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestRouteFitReceiverRejectsTrailingJSONAfterVerification(t *testing.T) {
	body := []byte(`{"eventId":"evt_01","eventType":"fulfillment.shipped","schemaVersion":1,"occurredAt":"2026-10-29T08:00:00Z","aggregate":{"type":"fulfillment","id":"ful_01","version":2},"payload":{}}{}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/integrations/routefit/events", bytes.NewReader(body))
	request.Header.Set("X-RouteFit-Timestamp", "1793260800")
	request.Header.Set("X-RouteFit-Signature", "v1="+routeFitSignature("secret", "1793260800", body))

	receiver := RouteFitReceiver{Verifier: RouteFitWebhookVerifier{Secrets: [][]byte{[]byte("secret")}, Now: func() time.Time { return time.Unix(1_793_260_800, 0).UTC() }}}
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

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

func TestRouteFitReconciliationConfigDefaults(t *testing.T) {
	config := (RouteFitReconciliationConfig{}).normalized()
	if config.ScanEvery != 10*time.Minute || config.NoEventFor != 30*time.Minute {
		t.Fatalf("defaults = %+v", config)
	}
}

func TestRouteFitReconciliationScheduleAndHandler(t *testing.T) {
	registry := queue.NewRegistry()
	if err := RegisterRouteFitReconciliationTask(registry); err != nil {
		t.Fatalf("RegisterRouteFitReconciliationTask() error = %v", err)
	}
	config := RouteFitReconciliationConfig{Schedule: "*/7 * * * *", NoEventFor: 45 * time.Minute}
	task := RouteFitReconciliationScheduledTask(config)
	if task.Cron != "*/7 * * * *" || task.Type != RouteFitReconciliationTaskType {
		t.Fatalf("task = %+v", task)
	}
	payload := task.Payload(time.Date(2026, 10, 29, 8, 7, 0, 0, time.UTC))
	var decoded RouteFitReconciliationTaskPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("payload JSON = %v", err)
	}
	if decoded.NoEventFor != "45m0s" || decoded.Window != "2026-10-29T08:07:00Z" {
		t.Fatalf("payload = %+v", decoded)
	}

	runner := &routeFitReconciliationRunner{}
	if err := NewRouteFitReconciliationHandler(runner)(context.Background(), asynq.NewTask(RouteFitReconciliationTaskType, payload)); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if runner.noEventFor != 45*time.Minute {
		t.Fatalf("runner noEventFor = %s, want 45m", runner.noEventFor)
	}
}

func TestRouteFitReconciliationHandlerRejectsInvalidPayload(t *testing.T) {
	err := NewRouteFitReconciliationHandler(&routeFitReconciliationRunner{})(context.Background(), asynq.NewTask(RouteFitReconciliationTaskType, []byte(`{"no_event_for":"nope"}`)))
	if err == nil || !strings.Contains(err.Error(), asynq.SkipRetry.Error()) {
		t.Fatalf("handler error = %v, want SkipRetry", err)
	}
}

type routeFitReconciliationRunner struct{ noEventFor time.Duration }

func (r *routeFitReconciliationRunner) ReconcileRouteFitStaleShipments(_ context.Context, noEventFor time.Duration) error {
	r.noEventFor = noEventFor
	return nil
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

func TestRouteFitWebhookVerifierRejectsExtremeFutureTimestamp(t *testing.T) {
	body := []byte(`{}`)
	const timestamp = "9223372036854"
	request := httptest.NewRequest(http.MethodPost, "/v1/integrations/routefit/events", bytes.NewReader(body))
	request.Header.Set("X-RouteFit-Timestamp", timestamp)
	request.Header.Set("X-RouteFit-Signature", "v1="+routeFitSignature("secret", timestamp, body))
	verifier := RouteFitWebhookVerifier{Secrets: [][]byte{[]byte("secret")}, Now: func() time.Time { return time.Unix(1_793_260_800, 0).UTC() }}
	if err := verifier.Verify(request, body); err == nil {
		t.Fatal("Verify() accepted an extreme future timestamp")
	}
}

func routeFitSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
