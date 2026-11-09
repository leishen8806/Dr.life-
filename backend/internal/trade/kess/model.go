// Package kess implements the server-side KESS payment adapter.
// It persists and confirms channel facts only; it never posts ledger entries.
package kess

import (
	"context"
	"errors"
	"time"
)

const ChannelKESS = "kess"

type Status string

const (
	StatusPending Status = "PENDING"
	StatusWaiting Status = "WAITING"
	StatusSuccess Status = "SUCCESS"
	StatusFailed  Status = "FAILED"
	StatusClosed  Status = "CLOSED"
	StatusUnknown Status = "UNKNOWN"
	StatusReview  Status = "REVIEW"
	StatusRefund  Status = "REFUND"
)

var (
	ErrNotFound       = errors.New("kess payment intent not found")
	ErrInvalidChannel = errors.New("invalid KESS channel response")
	ErrConflict       = errors.New("kess payment intent state conflict")
	ErrChannelMissing = errors.New("KESS order not found")
	ErrRequestNotSent = errors.New("KESS request was not sent")
)

// Intent is the server-created receivable snapshot. AmountE4 is USD in 0.0001
// units and must be a whole cent before it crosses the KESS boundary.
type Intent struct {
	ID                 int64
	CompanyID          int64
	Channel            string
	TradeNo            string
	SourceRef          string
	AttemptNo          int
	AmountE4           int64
	Currency           string
	ExpectedSellerCode string
	Status             Status
	CreateAttempts     int
	CreatedAt          time.Time
	LastCreateSentAt   *time.Time
	ExpiresAt          *time.Time
	PaymentURL         string
}

// ReceivableSnapshot is produced by the server-side order total calculation.
// It deliberately has no browser-facing representation.
type ReceivableSnapshot struct {
	CompanyID  int64
	SourceRef  string
	AttemptNo  int
	AmountE4   int64
	Currency   string
	SellerCode string
	ExpiresAt  *time.Time
}

type ChannelOrder struct {
	TradeNo       string
	SellerCode    string
	AmountE4      int64
	Currency      string
	Status        string
	TransactionID string
	PaymentURL    string
}

type ChannelResult struct {
	Status        Status
	ChannelStatus string
	TransactionID string
	PaymentURL    string
	CreateSent    bool
}

type Store interface {
	Intent(ctx context.Context, id int64) (Intent, error)
	ClaimPending(ctx context.Context, id int64) (Intent, bool, error)
	ReleasePendingClaim(ctx context.Context, id int64) error
	RecordChannelResult(ctx context.Context, id int64, from Status, result ChannelResult) error
}
