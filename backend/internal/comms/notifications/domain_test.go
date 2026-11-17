package notifications

import "testing"

func TestPlanUsesOnlyRecipientEndBot(t *testing.T) {
	event := Event{ID: "order-1", AccountID: 42, End: Buyer, Type: OrderCreated}
	plan, err := Plan(event, []Bot{{ID: 7, End: Merchant, Active: true}, {ID: 8, End: Buyer, Active: true}}, true)
	if err != nil || plan.TelegramBotID != 8 || !plan.InApp || plan.SkipReason != "" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestPlanFallsBackToInAppWhenNoMatchingBot(t *testing.T) {
	event := Event{ID: "order-2", AccountID: 42, End: Merchant, Type: OrderCreated}
	plan, err := Plan(event, []Bot{{ID: 8, End: Buyer, Active: true}}, true)
	if err != nil || plan.TelegramBotID != 0 || !plan.InApp || plan.SkipReason != "no_available_bot" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestPlanRejectsUnknownEvent(t *testing.T) {
	_, err := Plan(Event{ID: "x", AccountID: 1, End: Buyer, Type: "unknown"}, nil, true)
	if err == nil {
		t.Fatal("unknown event accepted")
	}
}
