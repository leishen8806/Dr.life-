package ledger

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

var (
	ErrWithdrawalNotFound    = errors.New("ledger withdrawal not found")
	ErrWithdrawalState       = errors.New("ledger withdrawal state does not allow this operation")
	ErrAccountGuardRequired  = errors.New("ledger withdrawal account guard is required")
	ErrPayeeCipherRequired   = errors.New("ledger withdrawal payee cipher is required")
	ErrSecondFactorRequired  = errors.New("ledger withdrawal second factor is required")
	ErrBankReferenceRequired = errors.New("ledger withdrawal bank reference is required")
)

type AccountGuard interface {
	CanWithdraw(context.Context, int64) error
}

type PayeeCipher interface {
	Encrypt(context.Context, []byte, string) ([]byte, error)
	Decrypt(context.Context, []byte, string) ([]byte, error)
}

type WithdrawalMetrics interface {
	WithdrawalRequested()
	WithdrawalReviewed()
	WithdrawalPaid()
	WithdrawalFailed()
	WithdrawalCancelled()
	WithdrawalOverdue(int)
}

type Withdrawal struct {
	ID                int64
	PublicID          string
	RequestKey        string
	AccountID         int64
	Amount            int64
	PayeeAccountLast4 string
	Status            string
	RequesterActor    int64
	MakerActor        *int64
	CheckerActor      *int64
	RequiresReview    bool
	ReqTxnID          *int64
	PaidTxnID         *int64
	BankRef           *string
	RequestedAt       time.Time
	ReviewedAt        *time.Time
	PaidAt            *time.Time
	FailedReason      *string
}

type RequestWithdrawalInput struct {
	RequestKey           string
	RequesterAccountID   int64
	WalletAvailable      int64
	Amount               int64
	PayeeAccount         string
	ActorID              int64
	SecondFactorVerified bool
}

type RequestWithdrawalResult struct {
	Withdrawal Withdrawal
	Post       PostResult
}

type StartReviewInput struct{ WithdrawalID, MakerActor int64 }
type ApproveReviewInput struct{ WithdrawalID, CheckerActor int64 }
type ConfirmPayoutInput struct {
	WithdrawalID int64
	BankRef      string
	ActorID      int64
}
type FailWithdrawalInput struct {
	WithdrawalID int64
	Reason       string
	ActorID      int64
}
type CancelWithdrawalInput struct {
	WithdrawalID int64
	Reason       string
	ActorID      int64
}

type PendingWithdrawal struct {
	ID         int64
	PublicID   string
	Amount     int64
	Status     string
	WaitingFor time.Duration
}

type PendingPayoutMismatch struct {
	Balance       int64
	OpenAmountSum int64
}

type withdrawalEventPayload struct {
	PublicID     string    `json:"public_id"`
	Amount       int64     `json:"amount"`
	ActorID      int64     `json:"actor_id,omitempty"`
	MakerActor   int64     `json:"maker_actor,omitempty"`
	CheckerActor int64     `json:"checker_actor,omitempty"`
	OccurredAt   time.Time `json:"occurred_at"`
}

// RequestWithdrawal requires the API layer to complete real second-factor
// verification first. The boolean is only a fail-closed boundary marker.
func (s *Service) RequestWithdrawal(ctx context.Context, in RequestWithdrawalInput) (RequestWithdrawalResult, error) {
	if len(in.RequestKey) == 0 || len(in.RequestKey) > 100 || !keySegmentPattern.MatchString(in.RequestKey) ||
		in.RequesterAccountID <= 0 || in.WalletAvailable <= 0 || in.Amount <= 0 || in.ActorID <= 0 || strings.TrimSpace(in.PayeeAccount) == "" {
		return RequestWithdrawalResult{}, ErrInvalidRequest
	}
	if !in.SecondFactorVerified {
		return RequestWithdrawalResult{}, ErrSecondFactorRequired
	}
	if s.accountGuard == nil {
		return RequestWithdrawalResult{}, ErrAccountGuardRequired
	}
	if s.payeeCipher == nil {
		return RequestWithdrawalResult{}, ErrPayeeCipherRequired
	}
	if existing, err := queryWithdrawal(s.db.QueryRow(ctx, withdrawalSelect+` WHERE request_key=$1`, in.RequestKey)); err == nil {
		return s.replayWithdrawalRequest(ctx, s.db, existing, in)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return RequestWithdrawalResult{}, err
	}
	if err := s.accountGuard.CanWithdraw(ctx, in.RequesterAccountID); err != nil {
		return RequestWithdrawalResult{}, err
	}
	publicID, err := ids.NewULID()
	if err != nil {
		return RequestWithdrawalResult{}, err
	}
	ciphertext, err := s.payeeCipher.Encrypt(ctx, []byte(in.PayeeAccount), withdrawalPayeeAAD(publicID))
	if err != nil {
		return RequestWithdrawalResult{}, err
	}
	var withdrawalID int64
	if err := s.db.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('ledger.withdrawal','id'))`).Scan(&withdrawalID); err != nil {
		return RequestWithdrawalResult{}, err
	}
	requiresReview := in.Amount >= s.largeWithdrawalThreshold
	status := "approved"
	if requiresReview {
		status = "requested"
	}
	last4 := payeeLast4(in.PayeeAccount)
	idsByKind, err := s.singletonAccounts(ctx)
	if err != nil {
		return RequestWithdrawalResult{}, err
	}
	req := businessRequest("withdraw_req", in.Amount, "withdraw_req:request:"+in.RequestKey, "withdrawal", &withdrawalID, in.ActorID,
		Posting{AccountID: in.WalletAvailable, Side: "DEBIT", Amount: in.Amount},
		Posting{AccountID: idsByKind["PENDING_PAYOUT"], Side: "CREDIT", Amount: in.Amount})
	check := requireKinds(map[int64]string{in.WalletAvailable: "WALLET_AVAIL", idsByKind["PENDING_PAYOUT"]: "PENDING_PAYOUT"}, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, in.RequestKey); err != nil {
			return err
		}
		if existing, err := queryWithdrawal(tx.QueryRow(ctx, withdrawalSelect+` WHERE request_key=$1 FOR UPDATE`, in.RequestKey)); err == nil {
			replayed, err := s.replayWithdrawalRequest(ctx, tx, existing, in)
			if err != nil {
				return err
			}
			return &transactionReplay{result: replayed.Post}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := checkPayoutFreeze(ctx, tx, in.WalletAvailable); err != nil {
			return err
		}
		var owner *int64
		if err := tx.QueryRow(ctx, `SELECT owner_account FROM ledger.account WHERE id=$1`, in.WalletAvailable).Scan(&owner); err != nil {
			return err
		}
		if owner == nil || *owner != in.RequesterAccountID {
			return ErrAccountOwnerMismatch
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO ledger.withdrawal(id,public_id,request_key,account_id,amount,payee_account_text,payee_account_last4,status,requester_actor,requires_review)
			OVERRIDING SYSTEM VALUE VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, withdrawalID, publicID, in.RequestKey, in.RequesterAccountID,
			in.Amount, base64.RawStdEncoding.EncodeToString(ciphertext), last4, status, in.ActorID, requiresReview)
		return err
	})
	hooks := transactionHooks{skipInitialReplay: true, check: check, after: func(ctx context.Context, tx pgx.Tx, result PostResult) error {
		command, err := tx.Exec(ctx, `UPDATE ledger.withdrawal SET req_txn_id=$2 WHERE id=$1`, withdrawalID, result.TxnID)
		if err != nil {
			return err
		}
		if command.RowsAffected() != 1 {
			return ErrWithdrawalState
		}
		return nil
	}}
	post, err := s.postWithHooks(ctx, req, hooks)
	if err != nil {
		return RequestWithdrawalResult{}, err
	}
	withdrawal, err := s.getWithdrawalByRequestKey(ctx, in.RequestKey)
	if err != nil {
		return RequestWithdrawalResult{}, err
	}
	if !post.Replayed && s.withdrawalMetrics != nil {
		s.withdrawalMetrics.WithdrawalRequested()
	}
	return RequestWithdrawalResult{Withdrawal: withdrawal, Post: post}, nil
}

func (s *Service) replayWithdrawalRequest(ctx context.Context, db rowQuerier, existing Withdrawal, in RequestWithdrawalInput) (RequestWithdrawalResult, error) {
	if existing.AccountID != in.RequesterAccountID || existing.Amount != in.Amount || existing.RequesterActor != in.ActorID {
		return RequestWithdrawalResult{}, ErrIdempotencyConflict
	}
	var wallet int64
	if err := db.QueryRow(ctx, `SELECT id FROM ledger.account WHERE owner_account=$1 AND kind='WALLET_AVAIL'`, existing.AccountID).Scan(&wallet); err != nil {
		return RequestWithdrawalResult{}, err
	}
	if wallet != in.WalletAvailable {
		return RequestWithdrawalResult{}, ErrIdempotencyConflict
	}
	var encoded string
	if err := db.QueryRow(ctx, `SELECT payee_account_text FROM ledger.withdrawal WHERE id=$1`, existing.ID).Scan(&encoded); err != nil {
		return RequestWithdrawalResult{}, err
	}
	blob, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return RequestWithdrawalResult{}, ErrIdempotencyConflict
	}
	plaintext, err := s.payeeCipher.Decrypt(ctx, blob, withdrawalPayeeAAD(existing.PublicID))
	if err != nil || subtle.ConstantTimeCompare(plaintext, []byte(in.PayeeAccount)) != 1 {
		return RequestWithdrawalResult{}, ErrIdempotencyConflict
	}
	post, err := queryResult(ctx, db, `
		SELECT t.id,t.txn_no,o.payload
		FROM ledger.txn t
		LEFT JOIN LATERAL (
			SELECT payload FROM ledger.outbox WHERE txn_id=t.id AND topic='ledger.txn.posted' ORDER BY id LIMIT 1
		) o ON true
		WHERE t.idem_key=$1`, "withdraw_req:request:"+in.RequestKey)
	if err != nil {
		return RequestWithdrawalResult{}, err
	}
	if existing.ReqTxnID == nil || post.TxnID != *existing.ReqTxnID {
		return RequestWithdrawalResult{}, ErrIdempotencyConflict
	}
	post.Replayed = true
	return RequestWithdrawalResult{Withdrawal: existing, Post: post}, nil
}

func (s *Service) StartReview(ctx context.Context, in StartReviewInput) (Withdrawal, error) {
	if in.WithdrawalID <= 0 || in.MakerActor <= 0 {
		return Withdrawal{}, ErrInvalidRequest
	}
	withdrawal, err := s.updateWithdrawal(ctx, in.WithdrawalID, func(ctx context.Context, tx pgx.Tx, current Withdrawal) error {
		if current.Status == "review" && current.MakerActor != nil {
			if *current.MakerActor == in.MakerActor {
				return nil
			}
			return ErrIdempotencyConflict
		}
		if !current.RequiresReview || current.Status != "requested" {
			return ErrWithdrawalState
		}
		at := s.now()
		if _, err := tx.Exec(ctx, `UPDATE ledger.withdrawal SET status='review',maker_actor=$2 WHERE id=$1`, current.ID, in.MakerActor); err != nil {
			return err
		}
		if err := insertWithdrawalEvent(ctx, tx, nil, "ledger.withdrawal.review_started", withdrawalEventPayload{
			PublicID: current.PublicID, Amount: current.Amount, MakerActor: in.MakerActor, OccurredAt: at,
		}); err != nil {
			return err
		}
		return nil
	})
	return withdrawal, err
}

func (s *Service) ApproveReview(ctx context.Context, in ApproveReviewInput) (Withdrawal, error) {
	if in.WithdrawalID <= 0 || in.CheckerActor <= 0 {
		return Withdrawal{}, ErrInvalidRequest
	}
	changed := false
	withdrawal, err := s.updateWithdrawal(ctx, in.WithdrawalID, func(ctx context.Context, tx pgx.Tx, current Withdrawal) error {
		if current.Status == "approved" && current.RequiresReview && current.CheckerActor != nil {
			if *current.CheckerActor == in.CheckerActor {
				return nil
			}
			return ErrIdempotencyConflict
		}
		if current.Status != "review" || current.MakerActor == nil {
			return ErrWithdrawalState
		}
		if in.CheckerActor == *current.MakerActor || in.CheckerActor == current.RequesterActor {
			return ErrSeparationDuty
		}
		at := s.now()
		if _, err := tx.Exec(ctx, `UPDATE ledger.withdrawal SET status='approved',checker_actor=$2,reviewed_at=$3 WHERE id=$1`, current.ID, in.CheckerActor, at); err != nil {
			return err
		}
		if err := insertWithdrawalEvent(ctx, tx, nil, "ledger.withdrawal.approved", withdrawalEventPayload{
			PublicID: current.PublicID, Amount: current.Amount, MakerActor: *current.MakerActor, CheckerActor: in.CheckerActor, OccurredAt: at,
		}); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err == nil && changed && s.withdrawalMetrics != nil {
		s.withdrawalMetrics.WithdrawalReviewed()
	}
	return withdrawal, err
}

// closeWithdrawal follows the L-04 global order: balance rows are locked in
// ascending account_id order before the withdrawal row is locked by check.
func (s *Service) ConfirmPayout(ctx context.Context, in ConfirmPayoutInput) (PostResult, error) {
	if in.WithdrawalID <= 0 || in.ActorID <= 0 || strings.TrimSpace(in.BankRef) == "" {
		if strings.TrimSpace(in.BankRef) == "" {
			return PostResult{}, ErrBankReferenceRequired
		}
		return PostResult{}, ErrInvalidRequest
	}
	snapshot, err := s.GetWithdrawal(ctx, in.WithdrawalID)
	if err != nil {
		return PostResult{}, err
	}
	if snapshot.Status != "approved" && snapshot.Status != "paid" {
		return PostResult{}, ErrWithdrawalState
	}
	if snapshot.RequiresReview && snapshot.CheckerActor == nil {
		return PostResult{}, ErrSeparationDuty
	}
	idsByKind, err := s.singletonAccounts(ctx)
	if err != nil {
		return PostResult{}, err
	}
	req := businessRequest("payout", snapshot.Amount, fmt.Sprintf("payout:withdrawal:%d", snapshot.ID), "withdrawal", &snapshot.ID, in.ActorID,
		Posting{AccountID: idsByKind["PENDING_PAYOUT"], Side: "DEBIT", Amount: snapshot.Amount},
		Posting{AccountID: idsByKind["BANK_DEDICATED"], Side: "CREDIT", Amount: snapshot.Amount})
	req.Note = "bank_ref:" + strings.TrimSpace(in.BankRef)
	at := s.now()
	hooks := s.withdrawalCloseHooks(snapshot, "paid", req, func(ctx context.Context, tx pgx.Tx, result PostResult) error {
		if _, err := tx.Exec(ctx, `UPDATE ledger.withdrawal SET status='paid',paid_txn_id=$2,bank_ref=$3,paid_at=$4 WHERE id=$1`, snapshot.ID, result.TxnID, strings.TrimSpace(in.BankRef), at); err != nil {
			return err
		}
		return insertWithdrawalEvent(ctx, tx, result.TxnID, "ledger.withdrawal.paid", withdrawalEventPayload{
			PublicID: snapshot.PublicID, Amount: snapshot.Amount, ActorID: in.ActorID,
			MakerActor: valueOrZero(snapshot.MakerActor), CheckerActor: valueOrZero(snapshot.CheckerActor), OccurredAt: at,
		})
	})
	result, err := s.postWithHooks(ctx, req, hooks)
	if err == nil && !result.Replayed && s.withdrawalMetrics != nil {
		s.withdrawalMetrics.WithdrawalPaid()
	}
	return result, err
}

func (s *Service) FailWithdrawal(ctx context.Context, in FailWithdrawalInput) (PostResult, error) {
	return s.refundWithdrawal(ctx, in.WithdrawalID, "failed", in.Reason, in.ActorID)
}

// CancelWithdrawal can cancel an approved withdrawal, but that transition can
// race with a bank payment. The surrounding process must forbid cancellation
// after a bank transfer has been submitted.
func (s *Service) CancelWithdrawal(ctx context.Context, in CancelWithdrawalInput) (PostResult, error) {
	return s.refundWithdrawal(ctx, in.WithdrawalID, "cancelled", in.Reason, in.ActorID)
}

func (s *Service) refundWithdrawal(ctx context.Context, withdrawalID int64, target, reason string, actorID int64) (PostResult, error) {
	if withdrawalID <= 0 || actorID <= 0 || strings.TrimSpace(reason) == "" {
		return PostResult{}, ErrInvalidRequest
	}
	snapshot, err := s.GetWithdrawal(ctx, withdrawalID)
	if err != nil {
		return PostResult{}, err
	}
	allowed := snapshot.Status == "approved"
	if target == "cancelled" {
		allowed = snapshot.Status == "requested" || snapshot.Status == "review" || snapshot.Status == "approved"
	}
	if snapshot.Status != target && !allowed {
		return PostResult{}, ErrWithdrawalState
	}
	wallet, err := s.availableWalletForOwner(ctx, snapshot.AccountID)
	if err != nil {
		return PostResult{}, err
	}
	idsByKind, err := s.singletonAccounts(ctx)
	if err != nil {
		return PostResult{}, err
	}
	req := businessRequest("payout_fail", snapshot.Amount, fmt.Sprintf("payout_fail:withdrawal:%d", snapshot.ID), "withdrawal", &snapshot.ID, actorID,
		Posting{AccountID: idsByKind["PENDING_PAYOUT"], Side: "DEBIT", Amount: snapshot.Amount},
		Posting{AccountID: wallet, Side: "CREDIT", Amount: snapshot.Amount})
	req.Note = target + ":" + strings.TrimSpace(reason)
	at := s.now()
	hooks := s.withdrawalCloseHooks(snapshot, target, req, func(ctx context.Context, tx pgx.Tx, result PostResult) error {
		if _, err := tx.Exec(ctx, `UPDATE ledger.withdrawal SET status=$2,failed_reason=$3 WHERE id=$1`, snapshot.ID, target, strings.TrimSpace(reason)); err != nil {
			return err
		}
		return insertWithdrawalEvent(ctx, tx, result.TxnID, "ledger.withdrawal."+target, withdrawalEventPayload{
			PublicID: snapshot.PublicID, Amount: snapshot.Amount, ActorID: actorID,
			MakerActor: valueOrZero(snapshot.MakerActor), CheckerActor: valueOrZero(snapshot.CheckerActor), OccurredAt: at,
		})
	})
	result, err := s.postWithHooks(ctx, req, hooks)
	if err == nil && !result.Replayed && s.withdrawalMetrics != nil {
		if target == "failed" {
			s.withdrawalMetrics.WithdrawalFailed()
		} else {
			s.withdrawalMetrics.WithdrawalCancelled()
		}
	}
	return result, err
}

func (s *Service) withdrawalCloseHooks(snapshot Withdrawal, target string, request PostRequest, after transactionAfter) transactionHooks {
	check := requireKinds(nil, func(ctx context.Context, tx pgx.Tx) error {
		if target == "paid" {
			var walletID int64
			if err := tx.QueryRow(ctx, `SELECT id FROM ledger.account WHERE owner_account=$1 AND kind='WALLET_AVAIL'`, snapshot.AccountID).Scan(&walletID); err != nil {
				return err
			}
			if err := checkPayoutFreeze(ctx, tx, walletID); err != nil {
				return err
			}
		}
		locked, err := queryWithdrawal(tx.QueryRow(ctx, withdrawalSelect+` WHERE id=$1 FOR UPDATE`, snapshot.ID))
		if err != nil {
			return mapWithdrawalNotFound(err)
		}
		if !sameWithdrawalSnapshot(locked, snapshot) {
			result, replayErr := loadExisting(ctx, tx, request)
			if replayErr == nil {
				return &transactionReplay{result: result}
			}
			if !errors.Is(replayErr, pgx.ErrNoRows) {
				return replayErr
			}
			return ErrWithdrawalState
		}
		return nil
	})
	return transactionHooks{check: check, after: after, replayCheck: func(ctx context.Context, tx pgx.Tx, result PostResult) error {
		current, err := queryWithdrawal(tx.QueryRow(ctx, withdrawalSelect+` WHERE id=$1 FOR UPDATE`, snapshot.ID))
		if err != nil || current.Status != target {
			return ErrIdempotencyConflict
		}
		if target == "paid" && (current.PaidTxnID == nil || *current.PaidTxnID != result.TxnID) {
			return ErrIdempotencyConflict
		}
		return nil
	}}
}

func (s *Service) GetWithdrawal(ctx context.Context, id int64) (Withdrawal, error) {
	if id <= 0 {
		return Withdrawal{}, ErrInvalidRequest
	}
	withdrawal, err := queryWithdrawal(s.db.QueryRow(ctx, withdrawalSelect+` WHERE id=$1`, id))
	return withdrawal, mapWithdrawalNotFound(err)
}

func (s *Service) getWithdrawalByRequestKey(ctx context.Context, requestKey string) (Withdrawal, error) {
	if len(requestKey) == 0 || len(requestKey) > 100 || !keySegmentPattern.MatchString(requestKey) {
		return Withdrawal{}, ErrInvalidRequest
	}
	withdrawal, err := queryWithdrawal(s.db.QueryRow(ctx, withdrawalSelect+` WHERE request_key=$1`, requestKey))
	return withdrawal, mapWithdrawalNotFound(err)
}

func (s *Service) DecryptWithdrawalPayee(ctx context.Context, id, actorID int64, purpose string) ([]byte, error) {
	if id <= 0 || actorID <= 0 || strings.TrimSpace(purpose) == "" {
		return nil, ErrInvalidRequest
	}
	if s.payeeCipher == nil {
		return nil, ErrPayeeCipherRequired
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var publicID, encoded string
	if err := tx.QueryRow(ctx, `SELECT public_id,payee_account_text FROM ledger.withdrawal WHERE id=$1 FOR SHARE`, id).Scan(&publicID, &encoded); err != nil {
		return nil, mapWithdrawalNotFound(err)
	}
	blob, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	plaintext, err := s.payeeCipher.Decrypt(ctx, blob, withdrawalPayeeAAD(publicID))
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(struct {
		PublicID   string    `json:"public_id"`
		ActorID    int64     `json:"actor"`
		Purpose    string    `json:"purpose"`
		OccurredAt time.Time `json:"occurred_at"`
	}{PublicID: publicID, ActorID: actorID, Purpose: strings.TrimSpace(purpose), OccurredAt: s.now()})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger.outbox(topic,payload) VALUES('ledger.withdrawal.payee_accessed',$1)`, payload); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return plaintext, nil
}

func (s *Service) ListOverdueWithdrawals(ctx context.Context, now time.Time, olderThan time.Duration) ([]PendingWithdrawal, error) {
	if olderThan <= 0 {
		olderThan = 24 * time.Hour
	}
	result, err := s.listPending(ctx, `status IN ('requested','review','approved') AND requested_at < $1`, now.Add(-olderThan), now)
	if err == nil && s.withdrawalMetrics != nil {
		s.withdrawalMetrics.WithdrawalOverdue(len(result))
	}
	return result, err
}

func (s *Service) ListLargePending(ctx context.Context) ([]PendingWithdrawal, error) {
	return s.listPending(ctx, `requires_review AND status IN ('requested','review','approved')`, nil, s.now())
}

func (s *Service) listPending(ctx context.Context, where string, arg any, now time.Time) ([]PendingWithdrawal, error) {
	query := `SELECT id,public_id,amount,status,requested_at FROM ledger.withdrawal WHERE ` + where + ` ORDER BY requested_at,id`
	var rows pgx.Rows
	var err error
	if arg == nil {
		rows, err = s.db.Query(ctx, query)
	} else {
		rows, err = s.db.Query(ctx, query, arg)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PendingWithdrawal
	for rows.Next() {
		var item PendingWithdrawal
		var requestedAt time.Time
		if err := rows.Scan(&item.ID, &item.PublicID, &item.Amount, &item.Status, &requestedAt); err != nil {
			return nil, err
		}
		item.WaitingFor = now.Sub(requestedAt)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) VerifyPendingPayoutMatchesWithdrawals(ctx context.Context) ([]PendingPayoutMismatch, error) {
	var mismatch PendingPayoutMismatch
	err := s.db.QueryRow(ctx, `
		SELECT b.balance,COALESCE((SELECT sum(amount) FROM ledger.withdrawal WHERE status IN ('requested','review','approved')),0)
		FROM ledger.account a JOIN ledger.balance b ON b.account_id=a.id
		WHERE a.kind='PENDING_PAYOUT' AND a.owner_account IS NULL
		  AND b.balance <> COALESCE((SELECT sum(amount) FROM ledger.withdrawal WHERE status IN ('requested','review','approved')),0)`).Scan(&mismatch.Balance, &mismatch.OpenAmountSum)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []PendingPayoutMismatch{mismatch}, nil
}

func (s *Service) updateWithdrawal(ctx context.Context, id int64, update func(context.Context, pgx.Tx, Withdrawal) error) (Withdrawal, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Withdrawal{}, err
	}
	defer tx.Rollback(ctx)
	current, err := queryWithdrawal(tx.QueryRow(ctx, withdrawalSelect+` WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return Withdrawal{}, mapWithdrawalNotFound(err)
	}
	if err := update(ctx, tx, current); err != nil {
		return Withdrawal{}, err
	}
	updated, err := queryWithdrawal(tx.QueryRow(ctx, withdrawalSelect+` WHERE id=$1`, id))
	if err != nil {
		return Withdrawal{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Withdrawal{}, err
	}
	return updated, nil
}

func (s *Service) availableWalletForOwner(ctx context.Context, owner int64) (int64, error) {
	var wallet int64
	err := s.db.QueryRow(ctx, `SELECT id FROM ledger.account WHERE owner_account=$1 AND kind='WALLET_AVAIL'`, owner).Scan(&wallet)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrAccountOwnerMismatch
	}
	return wallet, err
}

const withdrawalSelect = `SELECT id,public_id,request_key,account_id,amount,COALESCE(payee_account_last4,''),status,requester_actor,maker_actor,checker_actor,requires_review,req_txn_id,paid_txn_id,bank_ref,requested_at,reviewed_at,paid_at,failed_reason FROM ledger.withdrawal`

func queryWithdrawal(row rowScanner) (Withdrawal, error) {
	var withdrawal Withdrawal
	err := row.Scan(&withdrawal.ID, &withdrawal.PublicID, &withdrawal.RequestKey, &withdrawal.AccountID, &withdrawal.Amount, &withdrawal.PayeeAccountLast4,
		&withdrawal.Status, &withdrawal.RequesterActor, &withdrawal.MakerActor, &withdrawal.CheckerActor, &withdrawal.RequiresReview,
		&withdrawal.ReqTxnID, &withdrawal.PaidTxnID, &withdrawal.BankRef, &withdrawal.RequestedAt, &withdrawal.ReviewedAt,
		&withdrawal.PaidAt, &withdrawal.FailedReason)
	return withdrawal, err
}

func mapWithdrawalNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWithdrawalNotFound
	}
	return err
}

func sameWithdrawalSnapshot(a, b Withdrawal) bool {
	return a.ID == b.ID && a.RequestKey == b.RequestKey && a.AccountID == b.AccountID && a.Amount == b.Amount && a.Status == b.Status &&
		sameInt64(a.ReqTxnID, b.ReqTxnID) && sameInt64(a.MakerActor, b.MakerActor) && sameInt64(a.CheckerActor, b.CheckerActor)
}

func withdrawalPayeeAAD(publicID string) string { return "withdrawal_payee:" + publicID }

func insertWithdrawalEvent(ctx context.Context, tx pgx.Tx, txnID any, topic string, payload withdrawalEventPayload) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO ledger.outbox(txn_id,topic,payload) VALUES($1,$2,$3)`, txnID, topic, encoded)
	return err
}

func payeeLast4(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= 4 {
		return string(runes)
	}
	return string(runes[len(runes)-4:])
}
