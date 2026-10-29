// Package logistics owns RouteFit's shipping-facing integration boundary.
package logistics

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func RouteFitSecrets(raw string) [][]byte {
	values := strings.Split(raw, ",")
	secrets := make([][]byte, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			secrets = append(secrets, []byte(value))
		}
	}
	return secrets
}

var ErrInvalidRouteFitWebhook = errors.New("invalid RouteFit webhook")

var ErrInvalidRouteFitEvent = errors.New("invalid RouteFit event")

// RouteFitEvent is the external event envelope. Payload remains untyped until
// a registered domain handler owns a supported event type.
type RouteFitEvent struct {
	EventID       string            `json:"eventId"`
	EventType     string            `json:"eventType"`
	SchemaVersion int               `json:"schemaVersion"`
	OccurredAt    time.Time         `json:"occurredAt"`
	Aggregate     RouteFitAggregate `json:"aggregate"`
	CorrelationID string            `json:"correlationId"`
	Payload       json.RawMessage   `json:"payload"`
}

type RouteFitAggregate struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

func ParseRouteFitEvent(body []byte) (RouteFitEvent, error) {
	var event RouteFitEvent
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil || strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.EventType) == "" || event.SchemaVersion <= 0 || event.OccurredAt.IsZero() || strings.TrimSpace(event.Aggregate.Type) == "" || strings.TrimSpace(event.Aggregate.ID) == "" || event.Aggregate.Version < 0 || len(event.Payload) == 0 || !json.Valid(event.Payload) {
		return RouteFitEvent{}, ErrInvalidRouteFitEvent
	}
	return event, nil
}

// RouteFitWebhookVerifier verifies the RouteFit-specific signed wire format.
// Secrets are supplied by configuration and are deliberately never logged.
type RouteFitWebhookVerifier struct {
	Secrets   [][]byte
	Tolerance time.Duration
	Now       func() time.Time
}

func (v RouteFitWebhookVerifier) Verify(request *http.Request, body []byte) error {
	if len(v.Secrets) == 0 {
		return ErrInvalidRouteFitWebhook
	}
	if v.Tolerance <= 0 {
		v.Tolerance = 5 * time.Minute
	}
	if v.Now == nil {
		v.Now = time.Now
	}
	timestamp := request.Header.Get("X-RouteFit-Timestamp")
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || absDuration(v.Now().Sub(time.Unix(seconds, 0))) > v.Tolerance {
		return ErrInvalidRouteFitWebhook
	}
	signatures := routeFitSignatures(request.Header.Values("X-RouteFit-Signature"))
	if len(signatures) == 0 {
		return ErrInvalidRouteFitWebhook
	}
	for _, secret := range v.Secrets {
		if len(secret) == 0 {
			continue
		}
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write([]byte(timestamp))
		_, _ = mac.Write([]byte("."))
		_, _ = mac.Write(body)
		expected := mac.Sum(nil)
		for _, signature := range signatures {
			if subtle.ConstantTimeCompare(expected, signature) == 1 {
				return nil
			}
		}
	}
	return ErrInvalidRouteFitWebhook
}

func routeFitSignatures(values []string) [][]byte {
	var signatures [][]byte
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			key, encoded, found := strings.Cut(strings.TrimSpace(part), "=")
			if !found || key != "v1" {
				continue
			}
			signature, err := hex.DecodeString(encoded)
			if err == nil && len(signature) == sha256.Size {
				signatures = append(signatures, signature)
			}
		}
	}
	return signatures
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
