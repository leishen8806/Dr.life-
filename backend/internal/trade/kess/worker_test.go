package kess

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"
)

func TestCreateTimeoutMarksIntentUnknownWithoutChangingTradeNumber(t *testing.T) {
	intent := Intent{
		ID:                 1,
		CompanyID:          7,
		Channel:            ChannelKESS,
		TradeNo:            "DRL-01KESS000000000000000001",
		AmountE4:           123400,
		Currency:           "USD",
		ExpectedSellerCode: "seller-test",
		Status:             StatusPending,
	}
	store := &memoryStore{intent: intent}
	worker := NewWorker(store, timeoutClient{}, 24*time.Hour)

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

func TestCreateDialFailureReleasesClaimForRetry(t *testing.T) {
	intent := Intent{ID: 6, TradeNo: "DRL-ORDER-4-1", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller", Status: StatusPending}
	store := &memoryStore{intent: intent}
	worker := NewWorker(store, dialFailureClient{}, 24*time.Hour)

	if err := worker.Create(context.Background(), intent.ID); err == nil {
		t.Fatal("Create error = nil, want dial failure")
	}
	if store.intent.Status != StatusPending || store.claimed {
		t.Fatalf("status=%q claimed=%v, want PENDING and released", store.intent.Status, store.claimed)
	}
}

func TestRecoverUnknownQueriesTheOriginalTradeNumber(t *testing.T) {
	intent := Intent{
		ID:                 2,
		CompanyID:          7,
		Channel:            ChannelKESS,
		TradeNo:            "DRL-01KESS000000000000000002",
		AmountE4:           10000,
		Currency:           "USD",
		ExpectedSellerCode: "seller-test",
		Status:             StatusUnknown,
	}
	store := &memoryStore{intent: intent}
	client := &queryClient{result: ChannelOrder{TradeNo: intent.TradeNo, SellerCode: intent.ExpectedSellerCode, AmountE4: intent.AmountE4, Currency: intent.Currency, Status: "WAITING"}}
	worker := NewWorker(store, client, 24*time.Hour)

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

func TestCreateTerminalResponseBecomesUnknownUntilQueryConfirms(t *testing.T) {
	intent := Intent{ID: 3, TradeNo: "DRL-ORDER-1-1", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller", Status: StatusPending}
	store := &memoryStore{intent: intent}
	client := &queryClient{createResult: ChannelOrder{TradeNo: intent.TradeNo, SellerCode: "seller", AmountE4: 10000, Currency: "USD", Status: "SUCCESS", TransactionID: "tx-1"}}

	if err := NewWorker(store, client, 24*time.Hour).Create(context.Background(), intent.ID); err != nil {
		t.Fatal(err)
	}
	if store.intent.Status != StatusUnknown {
		t.Fatalf("status = %q, want UNKNOWN", store.intent.Status)
	}
}

func TestRecoverUnknownKeepsUnknownOnMismatchedChannelFacts(t *testing.T) {
	intent := Intent{ID: 4, TradeNo: "DRL-ORDER-2-1", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller", Status: StatusUnknown}
	store := &memoryStore{intent: intent}
	client := &queryClient{result: ChannelOrder{TradeNo: intent.TradeNo, SellerCode: "seller", AmountE4: 20000, Currency: "USD", Status: "SUCCESS", TransactionID: "tx-2"}}

	if err := NewWorker(store, client, 24*time.Hour).RecoverUnknown(context.Background(), intent.ID); !errors.Is(err, ErrInvalidChannel) {
		t.Fatalf("RecoverUnknown error = %v, want ErrInvalidChannel", err)
	}
	if store.intent.Status != StatusUnknown {
		t.Fatalf("status = %q, want UNKNOWN", store.intent.Status)
	}
}

func TestRecoverUnknownClosesMissingOrderOnlyAfterRecoveryWindow(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	intent := Intent{ID: 5, TradeNo: "DRL-ORDER-3-1", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller", Status: StatusUnknown, CreatedAt: now.Add(-25 * time.Hour)}
	store := &memoryStore{intent: intent}
	client := &queryClient{queryErr: ErrChannelMissing}
	worker := NewWorker(store, client, 24*time.Hour)
	worker.now = func() time.Time { return now }

	if err := worker.RecoverUnknown(context.Background(), intent.ID); err != nil {
		t.Fatal(err)
	}
	if store.intent.Status != StatusClosed {
		t.Fatalf("status = %q, want CLOSED", store.intent.Status)
	}
}

func TestRecoverUnknownKeepsMissingOrderBeforeRecoveryWindow(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	intent := Intent{ID: 7, TradeNo: "DRL-ORDER-5-1", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller", Status: StatusUnknown, CreatedAt: now.Add(-23 * time.Hour)}
	store := &memoryStore{intent: intent}
	worker := NewWorker(store, &queryClient{queryErr: ErrChannelMissing}, 24*time.Hour)
	worker.now = func() time.Time { return now }

	if err := worker.RecoverUnknown(context.Background(), intent.ID); !errors.Is(err, ErrChannelMissing) {
		t.Fatalf("RecoverUnknown error = %v, want ErrChannelMissing", err)
	}
	if store.intent.Status != StatusUnknown {
		t.Fatalf("status = %q, want UNKNOWN", store.intent.Status)
	}
}

type memoryStore struct {
	intent  Intent
	claimed bool
}

func (s *memoryStore) Intent(_ context.Context, id int64) (Intent, error) {
	if id != s.intent.ID {
		return Intent{}, ErrNotFound
	}
	return s.intent, nil
}

func (s *memoryStore) ClaimPending(_ context.Context, id int64) (Intent, bool, error) {
	if id != s.intent.ID {
		return Intent{}, false, ErrNotFound
	}
	if s.intent.Status != StatusPending || s.claimed {
		return Intent{}, false, nil
	}
	s.claimed = true
	s.intent.CreateAttempts++
	return s.intent, true, nil
}

func (s *memoryStore) ReleasePendingClaim(_ context.Context, id int64) error {
	if id != s.intent.ID || !s.claimed || s.intent.Status != StatusPending {
		return ErrConflict
	}
	s.claimed = false
	return nil
}

func (s *memoryStore) RecordChannelResult(_ context.Context, id int64, from Status, result ChannelResult) error {
	if id != s.intent.ID {
		return ErrNotFound
	}
	if s.intent.Status != from {
		return ErrConflict
	}
	s.claimed = false
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

type dialFailureClient struct{}

func (dialFailureClient) Create(context.Context, Intent) (ChannelOrder, error) {
	return ChannelOrder{}, &url.Error{Op: "Post", URL: "https://devwebpayment.kesspay.io", Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("test dial failure")}}
}
func (dialFailureClient) Query(context.Context, Intent) (ChannelOrder, error) {
	return ChannelOrder{}, errors.New("unexpected query")
}

type queryClient struct {
	createResult   ChannelOrder
	result         ChannelOrder
	queryErr       error
	queriedTradeNo string
}

func (c *queryClient) Create(context.Context, Intent) (ChannelOrder, error) {
	if c.createResult.TradeNo == "" {
		return ChannelOrder{}, errors.New("unexpected create")
	}
	return c.createResult, nil
}
func (c *queryClient) Query(_ context.Context, intent Intent) (ChannelOrder, error) {
	c.queriedTradeNo = intent.TradeNo
	return c.result, c.queryErr
}
