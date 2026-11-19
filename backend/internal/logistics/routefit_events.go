package logistics

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrUnsupportedRouteFitEvent = errors.New("unsupported RouteFit fulfillment event")

// FulfillmentUpdate is the small, evidence-safe projection consumed by the
// trade adapter. It carries references only; evidence bytes never cross this
// boundary.
type FulfillmentUpdate struct {
	ExternalOrderID string
	FulfillmentID   string
	State           string
	EvidenceRefs    []string
	OccurredAt      time.Time
}

// ProjectRouteFitEvent maps the contract event names to Dr.life order states.
// Unknown events are retained by the inbox but are not applied to an order.
func ProjectRouteFitEvent(event RouteFitEvent, now time.Time, maxAge time.Duration) (FulfillmentUpdate, error) {
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	if event.OccurredAt.IsZero() || now.Sub(event.OccurredAt) > maxAge || event.OccurredAt.Sub(now) > 5*time.Minute {
		return FulfillmentUpdate{}, errors.New("routefit event timestamp is outside the acceptance window")
	}
	state, ok := map[string]string{
		"fulfillment.accepted":           "accepted",
		"fulfillment.rejected":           "rejected",
		"fulfillment.picked":             "in_fulfillment",
		"fulfillment.packed":             "in_fulfillment",
		"fulfillment.shipped":            "shipped",
		"fulfillment.delivery_reported":  "shipped",
		"fulfillment.delivered_verified": "delivered_verified",
		"fulfillment.delivery_failed":    "delivery_failed",
		"fulfillment.cancelled":          "cancelled",
		"fulfillment.returned":           "returned",
	}[event.EventType]
	if !ok {
		return FulfillmentUpdate{}, ErrUnsupportedRouteFitEvent
	}
	var payload struct {
		ExternalOrderID string `json:"externalOrderId"`
		FulfillmentID   string `json:"fulfillmentId"`
		Evidence        []struct {
			ID string `json:"evidenceId"`
		} `json:"evidenceSummaries"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil || strings.TrimSpace(payload.ExternalOrderID) == "" || strings.TrimSpace(payload.FulfillmentID) == "" {
		return FulfillmentUpdate{}, errors.New("routefit event payload is invalid")
	}
	refs := make([]string, 0, len(payload.Evidence))
	for _, evidence := range payload.Evidence {
		if strings.TrimSpace(evidence.ID) != "" {
			refs = append(refs, evidence.ID)
		}
	}
	return FulfillmentUpdate{ExternalOrderID: payload.ExternalOrderID, FulfillmentID: payload.FulfillmentID, State: state, EvidenceRefs: refs, OccurredAt: event.OccurredAt}, nil
}
