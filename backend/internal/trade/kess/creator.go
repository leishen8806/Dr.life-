package kess

import (
	"context"
	"errors"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/trade"
)

// Creator is the synchronous payment-link adapter used by the order HTTP
// endpoint. Scheduled query/recovery remains owned by the follow-up worker PR.
type Creator struct {
	Store      *PgStore
	Worker     *Worker
	CompanyID  int64
	SellerCode string
	TTL        time.Duration
	now        func() time.Time
}

func NewCreator(store *PgStore, worker *Worker, companyID int64, sellerCode string, ttl time.Duration) (*Creator, error) {
	if store == nil || worker == nil || companyID <= 0 || sellerCode == "" || ttl <= 0 {
		return nil, ErrConfig
	}
	return &Creator{Store: store, Worker: worker, CompanyID: companyID, SellerCode: sellerCode, TTL: ttl, now: time.Now}, nil
}

func (c *Creator) Create(ctx context.Context, sourceRef string, amountE4 int64, attemptNo int) (trade.PaymentResult, error) {
	expiresAt := c.now().Add(c.TTL)
	intent, err := c.Store.CreateIntent(ctx, ReceivableSnapshot{CompanyID: c.CompanyID, SourceRef: sourceRef, AttemptNo: attemptNo, AmountE4: amountE4, Currency: "USD", SellerCode: c.SellerCode, ExpiresAt: &expiresAt})
	if err != nil {
		return trade.PaymentResult{}, err
	}
	if err := c.Worker.Create(ctx, intent.ID); err != nil {
		return trade.PaymentResult{}, err
	}
	intent, err = c.Store.Intent(ctx, intent.ID)
	if err != nil {
		return trade.PaymentResult{}, err
	}
	if intent.Status != StatusWaiting || intent.PaymentURL == "" || intent.ExpiresAt == nil {
		return trade.PaymentResult{}, errors.New("kess: payment link is not available")
	}
	return trade.PaymentResult{PaymentNo: intent.TradeNo, PaymentLink: intent.PaymentURL, ExpiresAt: *intent.ExpiresAt}, nil
}
