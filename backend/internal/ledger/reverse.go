package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const maxReversalReasonLength = 500

var (
	ErrTransactionNotFound = errors.New("ledger transaction not found")
	ErrReversalNotFound    = errors.New("ledger reversal not found")
	ErrAlreadyReversed     = errors.New("ledger transaction already reversed")
	ErrNotReversible       = errors.New("ledger reversal transactions cannot be reversed")
	ErrReversalNotAllowed  = errors.New("ledger transaction type is not reversible")
)

var reversibleTxnTypes = map[string]struct{}{
	"topup": {}, "storage_fee": {}, "member_fee": {}, "ad_charge": {},
	"ad_spend": {}, "ad_refund": {}, "bank_diff": {}, "sweep": {},
	"carrier_pay": {}, "adjust": {},
}

type ReverseInput struct {
	OriginalTxnID int64
	ActorID       int64
	AuthorizedBy  int64
	Reason        string
}

type transactionSnapshot struct {
	id       int64
	typ      string
	amount   int64
	postings []storedPosting
}

type reversalEventPayload struct {
	OriginalTxnID int64     `json:"original_txn_id"`
	ReversalTxnID int64     `json:"reversal_txn_id"`
	Amount        int64     `json:"amount"`
	ActorID       int64     `json:"actor_id"`
	AuthorizedBy  int64     `json:"authorized_by"`
	Reason        string    `json:"reason"`
	OccurredAt    time.Time `json:"occurred_at"`
}

type transactionReader interface {
	rowQuerier
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (s *Service) Reverse(ctx context.Context, in ReverseInput) (PostResult, error) {
	reason := strings.TrimSpace(in.Reason)
	if in.OriginalTxnID <= 0 || in.ActorID <= 0 || in.AuthorizedBy <= 0 || reason == "" || utf8.RuneCountInString(reason) > maxReversalReasonLength {
		return PostResult{}, ErrInvalidRequest
	}
	original, err := loadTransactionSnapshot(ctx, s.db, in.OriginalTxnID)
	if err != nil {
		return PostResult{}, err
	}
	if original.typ == "reversal" {
		return PostResult{}, ErrNotReversible
	}
	if _, allowed := reversibleTxnTypes[original.typ]; !allowed {
		return PostResult{}, ErrReversalNotAllowed
	}

	postings := make([]Posting, len(original.postings))
	for i, posting := range original.postings {
		side := "DEBIT"
		if posting.side == "DEBIT" {
			side = "CREDIT"
		}
		postings[i] = Posting{AccountID: posting.accountID, Side: side, Amount: posting.amount, Role: posting.role, Category: posting.category}
	}
	request := PostRequest{
		Type: "reversal", Amount: original.amount, IdemKey: fmt.Sprintf("reversal:txn:%d", original.id),
		RefType: "txn", RefID: &original.id, Note: fmt.Sprintf("authorized_by:%d reason:%s", in.AuthorizedBy, reason),
		ActorID: in.ActorID, ReversesTxnID: &original.id, Postings: postings,
	}
	hooks := transactionHooks{
		check: func(ctx context.Context, tx pgx.Tx) error {
			var reversalID int64
			if err := tx.QueryRow(ctx, `SELECT id FROM ledger.txn WHERE reverses_txn_id=$1`, original.id).Scan(&reversalID); err == nil {
				result, replayErr := loadExisting(ctx, tx, request)
				if replayErr == nil {
					return &transactionReplay{result: result}
				}
				return ErrAlreadyReversed
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			current, err := loadTransactionSnapshot(ctx, tx, original.id)
			if err != nil {
				return err
			}
			if !sameTransactionSnapshot(original, current) {
				return ErrIdempotencyConflict
			}
			return nil
		},
		after: func(ctx context.Context, tx pgx.Tx, result PostResult) error {
			payload, err := json.Marshal(reversalEventPayload{
				OriginalTxnID: original.id, ReversalTxnID: result.TxnID, Amount: original.amount,
				ActorID: in.ActorID, AuthorizedBy: in.AuthorizedBy, Reason: reason, OccurredAt: s.now(),
			})
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO ledger.outbox(txn_id,topic,payload) VALUES($1,'ledger.txn.reversed',$2)`, result.TxnID, payload)
			return err
		},
	}
	result, err := s.postWithHooks(ctx, request, hooks)
	if errors.Is(err, ErrIdempotencyConflict) || isReversalUniqueViolation(err) {
		if _, lookupErr := s.GetReversalOf(ctx, original.id); lookupErr == nil {
			return PostResult{}, ErrAlreadyReversed
		}
	}
	return result, err
}

func (s *Service) GetReversalOf(ctx context.Context, originalTxnID int64) (PostResult, error) {
	if originalTxnID <= 0 {
		return PostResult{}, ErrInvalidRequest
	}
	result, err := queryResult(ctx, s.db, `
		SELECT t.id,t.txn_no,o.payload
		FROM ledger.txn t
		LEFT JOIN LATERAL (
			SELECT payload FROM ledger.outbox
			WHERE txn_id=t.id AND topic='ledger.txn.posted' ORDER BY id LIMIT 1
		) o ON true
		WHERE t.reverses_txn_id=$1`, originalTxnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PostResult{}, ErrReversalNotFound
	}
	return result, err
}

func loadTransactionSnapshot(ctx context.Context, db transactionReader, txnID int64) (transactionSnapshot, error) {
	var snapshot transactionSnapshot
	if err := db.QueryRow(ctx, `SELECT id,type,amount FROM ledger.txn WHERE id=$1`, txnID).Scan(&snapshot.id, &snapshot.typ, &snapshot.amount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return transactionSnapshot{}, ErrTransactionNotFound
		}
		return transactionSnapshot{}, err
	}
	rows, err := db.Query(ctx, `
		SELECT account_id,side,amount,COALESCE(role,''),COALESCE(category,'')
		FROM ledger.posting WHERE txn_id=$1 ORDER BY account_id,side,id`, txnID)
	if err != nil {
		return transactionSnapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var posting storedPosting
		if err := rows.Scan(&posting.accountID, &posting.side, &posting.amount, &posting.role, &posting.category); err != nil {
			return transactionSnapshot{}, err
		}
		snapshot.postings = append(snapshot.postings, posting)
	}
	if err := rows.Err(); err != nil {
		return transactionSnapshot{}, err
	}
	return snapshot, nil
}

func sameTransactionSnapshot(left, right transactionSnapshot) bool {
	if left.id != right.id || left.typ != right.typ || left.amount != right.amount || len(left.postings) != len(right.postings) {
		return false
	}
	for i := range left.postings {
		if left.postings[i] != right.postings[i] {
			return false
		}
	}
	return true
}

func isReversalUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "uq_txn_reverses_once"
}
