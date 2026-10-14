package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const maxAdjustmentReasonLength = 500

var adjustmentAccountKinds = map[string]struct{}{
	"PLATFORM_HELD":   {},
	"SUSPENSE":        {},
	"PAYABLE_CARRIER": {},
}

// AdjustInput describes a reviewed correction between two platform-owned
// accounts. RefType and RefID identify the source record for idempotency.
type AdjustInput struct {
	ReasonCode      string
	Reason          string
	RefType         string
	RefID           int64
	DebitAccountID  int64
	CreditAccountID int64
	Amount          int64
	ActorID         int64
	ReviewerID      int64
}

type adjustmentEventPayload struct {
	ReasonCode      string    `json:"reason_code"`
	Reason          string    `json:"reason"`
	ActorID         int64     `json:"actor_id"`
	ReviewerID      int64     `json:"reviewer_id"`
	Amount          int64     `json:"amount"`
	DebitAccountID  int64     `json:"debit_account_id"`
	CreditAccountID int64     `json:"credit_account_id"`
	OccurredAt      time.Time `json:"occurred_at"`
}

// Adjust records a two-person correction between platform-owned accounts.
// Customer funds, the dedicated bank account, escrow, ad budget, and pending
// payout accounts are deliberately outside this API.
func (s *Service) Adjust(ctx context.Context, in AdjustInput) (PostResult, error) {
	reasonCode := strings.TrimSpace(in.ReasonCode)
	reason := strings.TrimSpace(in.Reason)
	if !keySegmentPattern.MatchString(reasonCode) || !keySegmentPattern.MatchString(in.RefType) ||
		in.RefID <= 0 || in.DebitAccountID <= 0 || in.CreditAccountID <= 0 || in.Amount <= 0 ||
		in.ActorID <= 0 || in.ReviewerID <= 0 || reason == "" || utf8.RuneCountInString(reason) > maxAdjustmentReasonLength {
		return PostResult{}, ErrInvalidRequest
	}
	if in.ActorID == in.ReviewerID {
		return PostResult{}, ErrSeparationDuty
	}
	refID := in.RefID
	request := PostRequest{
		Type: "adjust", Amount: in.Amount,
		IdemKey: fmt.Sprintf("adjust:%s-%d", in.RefType, in.RefID),
		RefType: in.RefType, RefID: &refID,
		Note:    fmt.Sprintf("reason_code:%s reviewer_actor:%d reason:%s", reasonCode, in.ReviewerID, reason),
		ActorID: in.ActorID,
		Postings: []Posting{
			{AccountID: in.DebitAccountID, Side: "DEBIT", Amount: in.Amount},
			{AccountID: in.CreditAccountID, Side: "CREDIT", Amount: in.Amount},
		},
	}
	hooks := transactionHooks{
		check: func(ctx context.Context, tx pgx.Tx) error {
			for _, accountID := range []int64{in.DebitAccountID, in.CreditAccountID} {
				var kind string
				if err := tx.QueryRow(ctx, `SELECT kind FROM ledger.account WHERE id=$1`, accountID).Scan(&kind); err != nil {
					return err
				}
				if _, allowed := adjustmentAccountKinds[kind]; !allowed {
					return fmt.Errorf("%w: account %d cannot be adjusted", ErrInvalidRequest, accountID)
				}
			}
			return nil
		},
		after: func(ctx context.Context, tx pgx.Tx, result PostResult) error {
			payload, err := json.Marshal(adjustmentEventPayload{
				ReasonCode: reasonCode, Reason: reason, ActorID: in.ActorID, ReviewerID: in.ReviewerID,
				Amount: in.Amount, DebitAccountID: in.DebitAccountID, CreditAccountID: in.CreditAccountID,
				OccurredAt: s.now(),
			})
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO ledger.outbox(txn_id,topic,payload) VALUES($1,'ledger.txn.adjusted',$2)`, result.TxnID, payload)
			return err
		},
	}
	return s.postWithHooks(ctx, request, hooks)
}
