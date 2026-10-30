package kess

import (
	"context"
	"errors"
	"testing"
)

func TestCreateTimeoutMarksIntentUnknownWithoutChangingTradeNumber(t *testing.T) {
	intent := Intent{
		ID:                 1,
		CompanyID:          7,
		Channel:            ChannelKESS,
		TradeNo:            "DRL-01KESS000000000000000001",
		Amount:             1234,
		Currency:           "USD",
		ExpectedSellerCode: "seller-test",
		Status:             StatusPending,
	}
	store := &memoryStore{intent: intent}
	worker := NewWorker(store, timeoutClient{})

	err := worker.Create(context.Background(), intent.ID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Create error = %v, want context deadline exceeded", err)
	}
	if store.intent.Status != StatusUnknown {
		t.Fatalf("status = %q, want %q", store.intent.Status, StatusUnknown)
	}
	if store.intent.TradeNo != intent.TradeNo {
		t.Fatalf("trade number changed from %q to %q", intent.TradeNo, store.intent.TradeNo)
	}
	if store.intent.CreateAttempts != 1 {
		t.Fatalf("create attempts = %d, want 1", store.intent.CreateAttempts)
	}
}

func TestRecoverUnknownQueriesTheOriginalTradeNumber(t *testing.T) {
	intent := Intent{
		ID:                 2,
		CompanyID:          7,
		Channel:            ChannelKESS,
		TradeNo:            "DRL-01KESS000000000000000002",
		Amount:             100,
		Currency:           "KHR",
		ExpectedSellerCode: "seller-test",
		Status:             StatusUnknown,
	}
	store := &memoryStore{intent: intent}
	client := &queryClient{result: ChannelOrder{TradeNo: intent.TradeNo, SellerCode: intent.ExpectedSellerCode, Amount: intent.Amount, Currency: intent.Currency, Status: "WAITING"}}
	worker := NewWorker(store, client)

	if err := worker.RecoverUnknown(context.Background(), intent.ID); err != nil {
		t.Fatal(err)
	}
	if client.queriedTradeNo != intent.TradeNo {
		t.Fatalf("queried trade number = %q, want %q", client.queriedTradeNo, intent.TradeNo)
	}
	if store.intent.Status != StatusWaiting {
		t.Fatalf("status = %q, want %q", store.intent.Status, StatusWaiting)
	}
}

type memoryStore struct{ intent Intent }

func (s *memoryStore) Intent(_ context.Context, id int64) (Intent, error) {
	if id != s.intent.ID {
		return Intent{}, ErrNotFound
	}
	return s.intent, nil
}

func (s *memoryStore) RecordCreateAttempt(_ context.Context, id int64) error {
	if id != s.intent.ID {
		return ErrNotFound
	}
	s.intent.CreateAttempts++
	return nil
}

func (s *memoryStore) RecordChannelResult(_ context.Context, id int64, result ChannelResult) error {
	if id != s.intent.ID {
		return ErrNotFound
	}
	s.intent.Status = result.Status
	return nil
}

type timeoutClient struct{}

func (timeoutClient) Create(context.Context, Intent) (ChannelOrder, error) {
	return ChannelOrder{}, context.DeadlineExceeded
}
func (timeoutClient) Query(context.Context, Intent) (ChannelOrder, error) {
	return ChannelOrder{}, errors.New("unexpected query")
}

type queryClient struct {
	result         ChannelOrder
	queriedTradeNo string
}

func (*queryClient) Create(context.Context, Intent) (ChannelOrder, error) {
	return ChannelOrder{}, errors.New("unexpected create")
}
func (c *queryClient) Query(_ context.Context, intent Intent) (ChannelOrder, error) {
	c.queriedTradeNo = intent.TradeNo
	return c.result, nil
}
