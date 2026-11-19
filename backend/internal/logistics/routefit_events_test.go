package logistics

import (
	"testing"
	"time"
)

func TestProjectRouteFitEventMapsStateAndOnlyEvidenceRefs(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	event := RouteFitEvent{
		EventID: "evt-1", EventType: "fulfillment.delivered_verified", SchemaVersion: 1,
		OccurredAt: now.Add(-time.Minute), Aggregate: RouteFitAggregate{Type: "fulfillment", ID: "ful-1", Version: 2},
		Payload: []byte(`{"externalOrderId":"order-1","fulfillmentId":"ful-1","evidenceSummaries":[{"evidenceId":"ev-1","url":"https://must-not-cross-boundary"}]}`),
	}
	update, err := ProjectRouteFitEvent(event, now, time.Hour)
	if err != nil || update.State != "delivered_verified" || update.ExternalOrderID != "order-1" || len(update.EvidenceRefs) != 1 || update.EvidenceRefs[0] != "ev-1" {
		t.Fatalf("update=%#v err=%v", update, err)
	}
}

func TestProjectRouteFitEventRejectsUnknownAndExpiredEvents(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	base := RouteFitEvent{EventID: "evt-1", SchemaVersion: 1, OccurredAt: now, Aggregate: RouteFitAggregate{Type: "fulfillment", ID: "ful-1", Version: 1}, Payload: []byte(`{"externalOrderId":"order-1","fulfillmentId":"ful-1"}`)}
	base.EventType = "fulfillment.unknown"
	if _, err := ProjectRouteFitEvent(base, now, time.Hour); err != ErrUnsupportedRouteFitEvent {
		t.Fatalf("unknown error=%v", err)
	}
	base.EventType = "fulfillment.shipped"
	base.OccurredAt = now.Add(-2 * time.Hour)
	if _, err := ProjectRouteFitEvent(base, now, time.Hour); err == nil {
		t.Fatal("expired event accepted")
	}
}
