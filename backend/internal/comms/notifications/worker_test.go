package notifications

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRepo struct {
	item       OutboxItem
	claimed    bool
	marked     string
	next       time.Time
	lastError  string
	deadLetter bool
}

func (r *fakeRepo) ClaimPending(context.Context) (OutboxItem, bool, error) {
	if r.claimed {
		return OutboxItem{}, false, nil
	}
	r.claimed = true
	return r.item, true, nil
}
func (r *fakeRepo) MarkDelivered(_ context.Context, id string) error { r.marked = id; return nil }
func (r *fakeRepo) MarkRetry(_ context.Context, id string, next time.Time, msg string, dead bool) error {
	r.marked, r.next, r.lastError, r.deadLetter = id, next, msg, dead
	return nil
}

type fakeInbox struct{ events []Event }

func (w *fakeInbox) PutOnce(_ context.Context, event Event) error {
	w.events = append(w.events, event)
	return nil
}

type fakeTelegram struct {
	ids []int64
	err error
}

func (w *fakeTelegram) SendOnce(_ context.Context, id int64, _ Event) error {
	if w.err == nil {
		w.ids = append(w.ids, id)
	}
	return w.err
}

func TestWorkerDeliversInAppAndMatchingBotOnce(t *testing.T) {
	repo := &fakeRepo{item: OutboxItem{Event: Event{ID: "event-1", AccountID: 42, End: Buyer, Type: OrderCreated}, Bots: []Bot{{ID: 9, End: Buyer, Active: true}, {ID: 10, End: Merchant, Active: true}}, TelegramEnabled: true}}
	inbox := &fakeInbox{}
	telegram := &fakeTelegram{}
	w := Worker{Repo: repo, Inbox: inbox, Telegram: telegram}
	processed, err := w.RunOnce(context.Background())
	if err != nil || !processed || repo.marked != "event-1" || len(inbox.events) != 1 || len(telegram.ids) != 1 || telegram.ids[0] != 9 {
		t.Fatalf("processed=%v err=%v repo=%+v inbox=%+v telegram=%+v", processed, err, repo, inbox, telegram)
	}
}

func TestWorkerRetriesWithSanitizedErrorAndDeadLettersAtLimit(t *testing.T) {
	repo := &fakeRepo{item: OutboxItem{Event: Event{ID: "event-2", AccountID: 42, End: Merchant, Type: ProductModerated}, Bots: []Bot{{ID: 11, End: Merchant, Active: true}}, TelegramEnabled: true, Attempts: 2}}
	telegram := &fakeTelegram{err: errors.New("provider response contains secret token")}
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	w := Worker{Repo: repo, Telegram: telegram, MaxAttempts: 3, Now: func() time.Time { return now }, Backoff: func(int) time.Duration { return 5 * time.Second }}
	processed, err := w.RunOnce(context.Background())
	if err != nil || !processed || !repo.deadLetter || repo.lastError != "notification delivery failed: *errors.errorString" || !repo.next.Equal(now) {
		t.Fatalf("processed=%v err=%v repo=%+v", processed, err, repo)
	}
}
