// Package kess implements the server-side KESS payment adapter.
// It persists and confirms channel facts only; it never posts ledger entries.
package kess

import (
	"context"
	"errors"
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
	StatusRefund  Status = "REFUND"
)

var (
	ErrNotFound       = errors.New("kess payment intent not found")
	ErrInvalidChannel = errors.New("invalid KESS channel response")
)

// Intent is the server-created receivable snapshot. Amount is in the currency's
// minor unit (USD cents or whole KHR), never a browser-provided payment claim.
type Intent struct {
	ID                 int64
	CompanyID          int64
	Channel            string
	TradeNo            string
	Amount             int64
	Currency           string
	ExpectedSellerCode string
	Status             Status
	CreateAttempts     int
}

// ReceivableSnapshot is produced by the server-side order total calculation.
// It deliberately has no browser-facing representation.
type ReceivableSnapshot struct {
	CompanyID  int64
	Amount     int64
	Currency   string
	SellerCode string
}

type ChannelOrder struct {
	TradeNo       string
	SellerCode    string
	Amount        int64
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
}

type Store interface {
	Intent(ctx context.Context, id int64) (Intent, error)
	RecordCreateAttempt(ctx context.Context, id int64) error
	RecordChannelResult(ctx context.Context, id int64, result ChannelResult) error
}
