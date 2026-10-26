package ledger_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
)

type withdrawalGuard struct{ err error }

func (g withdrawalGuard) CanWithdraw(context.Context, int64) error { return g.err }

type withdrawalMetrics struct {
	requested, reviewed, paid, failed, cancelled, overdue atomic.Int64
}

var withdrawalRequestKey atomic.Int64

func (m *withdrawalMetrics) WithdrawalRequested()    { m.requested.Add(1) }
func (m *withdrawalMetrics) WithdrawalReviewed()     { m.reviewed.Add(1) }
func (m *withdrawalMetrics) WithdrawalPaid()         { m.paid.Add(1) }
func (m *withdrawalMetrics) WithdrawalFailed()       { m.failed.Add(1) }
func (m *withdrawalMetrics) WithdrawalCancelled()    { m.cancelled.Add(1) }
func (m *withdrawalMetrics) WithdrawalOverdue(n int) { m.overdue.Add(int64(n)) }

func TestWithdrawalWorkflowIntegration(t *testing.T) {
	e := newBusinessEnv(t)
	cipher := withdrawalCipher(t)
	metrics := &withdrawalMetrics{}
	service := ledger.New(e.app, ledger.Options{
		AccountGuard: withdrawalGuard{}, PayeeCipher: cipher,
		LargeWithdrawalThreshold: 100, WithdrawalMetrics: metrics,
	})
	wallet := mustWallet(t, e, 8101, nil)
	setBalance(t, e, wallet.Available, 30_000_000)
	setSingletonBalance(t, e, "BANK_DEDICATED", 30_000_000)

	t.Run("fail closed boundaries and no residual rows", func(t *testing.T) {
		before := withdrawalCounts(t, e)
		base := ledger.RequestWithdrawalInput{RequestKey: "fail-closed-1", RequesterAccountID: 8101, WalletAvailable: wallet.Available, Amount: 10, PayeeAccount: "ACCT-SECRET-1234", ActorID: 11, SecondFactorVerified: true}
		if _, err := ledger.New(e.app, ledger.Options{PayeeCipher: cipher}).RequestWithdrawal(e.ctx, base); !errors.Is(err, ledger.ErrAccountGuardRequired) {
			t.Fatalf("missing guard error = %v", err)
		}
		if _, err := ledger.New(e.app, ledger.Options{AccountGuard: withdrawalGuard{}}).RequestWithdrawal(e.ctx, base); !errors.Is(err, ledger.ErrPayeeCipherRequired) {
			t.Fatalf("missing cipher error = %v", err)
		}
		base.SecondFactorVerified = false
		if _, err := service.RequestWithdrawal(e.ctx, base); !errors.Is(err, ledger.ErrSecondFactorRequired) {
			t.Fatalf("second factor error = %v", err)
		}
		base.SecondFactorVerified = true
		guardErr := errors.New("account frozen")
		blocked := ledger.New(e.app, ledger.Options{AccountGuard: withdrawalGuard{err: guardErr}, PayeeCipher: cipher})
		if _, err := blocked.RequestWithdrawal(e.ctx, base); !errors.Is(err, guardErr) {
			t.Fatalf("guard error = %v", err)
		}
		if after := withdrawalCounts(t, e); after != before {
			t.Fatalf("residual counts before=%v after=%v", before, after)
		}
		other := mustWallet(t, e, 8199, nil)
		base.WalletAvailable = other.Available
		if _, err := service.RequestWithdrawal(e.ctx, base); !errors.Is(err, ledger.ErrAccountOwnerMismatch) {
			t.Fatalf("owner mismatch error = %v", err)
		}
		if after := withdrawalCounts(t, e); after != before {
			t.Fatalf("owner mismatch left residual rows before=%v after=%v", before, after)
		}
		base.WalletAvailable = wallet.Available
		base.RequestKey = ""
		if _, err := service.RequestWithdrawal(e.ctx, base); !errors.Is(err, ledger.ErrInvalidRequest) {
			t.Fatalf("missing request key error = %v", err)
		}
		base.RequestKey = "bad key"
		if _, err := service.RequestWithdrawal(e.ctx, base); !errors.Is(err, ledger.ErrInvalidRequest) {
			t.Fatalf("invalid request key error = %v", err)
		}
		base.RequestKey = strings.Repeat("x", 101)
		if _, err := service.RequestWithdrawal(e.ctx, base); !errors.Is(err, ledger.ErrInvalidRequest) {
			t.Fatalf("long request key error = %v", err)
		}
	})

	t.Run("small request encrypts payee and cancellation restores balances", func(t *testing.T) {
		plaintext := "BANK-ACCOUNT-SECRET-1234"
		beforeWallet := accountBalance(t, e, wallet.Available)
		result, err := service.RequestWithdrawal(e.ctx, ledger.RequestWithdrawalInput{
			RequestKey: "small-cancel-1", RequesterAccountID: 8101, WalletAvailable: wallet.Available, Amount: 25,
			PayeeAccount: plaintext, ActorID: 11, SecondFactorVerified: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.Withdrawal.Status != "approved" || result.Withdrawal.RequiresReview || result.Withdrawal.PayeeAccountLast4 != "1234" {
			t.Fatalf("withdrawal = %+v", result.Withdrawal)
		}
		var stored string
		if err := e.admin.QueryRow(e.ctx, `SELECT payee_account_text FROM ledger.withdrawal WHERE id=$1`, result.Withdrawal.ID).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(stored, plaintext) {
			t.Fatal("ciphertext contains plaintext")
		}
		if strings.Contains(fmt.Sprint(result), plaintext) {
			t.Fatal("service result contains payee plaintext")
		}
		assertSQLState(t, execErr(e.ctx, e.admin, `UPDATE ledger.withdrawal SET request_key='changed-key' WHERE id=$1`, result.Withdrawal.ID), "55000")
		decrypted, err := service.DecryptWithdrawalPayee(e.ctx, result.Withdrawal.ID)
		if err != nil || string(decrypted) != plaintext {
			t.Fatalf("decrypt=%q err=%v", decrypted, err)
		}
		assertNoSensitiveOutbox(t, e, plaintext)
		if got := accountBalance(t, e, wallet.Available); got != beforeWallet-25 {
			t.Fatalf("wallet=%d", got)
		}
		if mismatches, err := service.VerifyPendingPayoutMatchesWithdrawals(e.ctx); err != nil || len(mismatches) != 0 {
			t.Fatalf("pending mismatch=%v err=%v", mismatches, err)
		}
		if _, err := service.CancelWithdrawal(e.ctx, ledger.CancelWithdrawalInput{WithdrawalID: result.Withdrawal.ID, Reason: "user request", ActorID: 12}); err != nil {
			t.Fatal(err)
		}
		if got := accountBalance(t, e, wallet.Available); got != beforeWallet {
			t.Fatalf("restored wallet=%d want=%d", got, beforeWallet)
		}
		if mismatches, err := service.VerifyPendingPayoutMatchesWithdrawals(e.ctx); err != nil || len(mismatches) != 0 {
			t.Fatalf("pending mismatch=%v err=%v", mismatches, err)
		}
	})

	t.Run("large review separation and paid state", func(t *testing.T) {
		result := requestWithdrawal(t, e, service, wallet.Available, 8101, 100, "LARGE-9876", 31)
		if result.Withdrawal.Status != "requested" || !result.Withdrawal.RequiresReview {
			t.Fatalf("withdrawal=%+v", result.Withdrawal)
		}
		if _, err := service.StartReview(e.ctx, ledger.StartReviewInput{WithdrawalID: result.Withdrawal.ID, MakerActor: 41}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.StartReview(e.ctx, ledger.StartReviewInput{WithdrawalID: result.Withdrawal.ID, MakerActor: 41}); err != nil {
			t.Fatalf("review replay: %v", err)
		}
		assertWithdrawalEvent(t, e, result.Withdrawal.PublicID, "ledger.withdrawal.review_started", "LARGE-9876", 41)
		if _, err := service.ApproveReview(e.ctx, ledger.ApproveReviewInput{WithdrawalID: result.Withdrawal.ID, CheckerActor: 41}); !errors.Is(err, ledger.ErrSeparationDuty) {
			t.Fatalf("same maker error=%v", err)
		}
		if _, err := service.ApproveReview(e.ctx, ledger.ApproveReviewInput{WithdrawalID: result.Withdrawal.ID, CheckerActor: 31}); !errors.Is(err, ledger.ErrSeparationDuty) {
			t.Fatalf("same requester error=%v", err)
		}
		assertSQLState(t, execErr(e.ctx, e.admin, `UPDATE ledger.withdrawal SET status='approved',checker_actor=maker_actor,reviewed_at=now() WHERE id=$1`, result.Withdrawal.ID), "23514")
		if _, err := service.ApproveReview(e.ctx, ledger.ApproveReviewInput{WithdrawalID: result.Withdrawal.ID, CheckerActor: 42}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.ApproveReview(e.ctx, ledger.ApproveReviewInput{WithdrawalID: result.Withdrawal.ID, CheckerActor: 42}); err != nil {
			t.Fatalf("approval replay: %v", err)
		}
		assertWithdrawalEvent(t, e, result.Withdrawal.PublicID, "ledger.withdrawal.approved", "LARGE-9876", 42)
		post, err := service.ConfirmPayout(e.ctx, ledger.ConfirmPayoutInput{WithdrawalID: result.Withdrawal.ID, BankRef: "BANK-REF-1", ActorID: 43})
		if err != nil || post.Replayed {
			t.Fatalf("confirm result=%+v err=%v", post, err)
		}
		replayed, err := service.ConfirmPayout(e.ctx, ledger.ConfirmPayoutInput{WithdrawalID: result.Withdrawal.ID, BankRef: "BANK-REF-1", ActorID: 43})
		if err != nil || !replayed.Replayed || replayed.TxnID != post.TxnID {
			t.Fatalf("confirm replay result=%+v err=%v", replayed, err)
		}
		assertWithdrawalEvent(t, e, result.Withdrawal.PublicID, "ledger.withdrawal.paid", "LARGE-9876", 43)
		if _, err := service.CancelWithdrawal(e.ctx, ledger.CancelWithdrawalInput{WithdrawalID: result.Withdrawal.ID, Reason: "too late", ActorID: 43}); !errors.Is(err, ledger.ErrWithdrawalState) {
			t.Fatalf("paid cancellation error=%v", err)
		}
	})

	t.Run("threshold boundaries and configurable default", func(t *testing.T) {
		for _, tc := range []struct {
			amount int64
			review bool
		}{{99, false}, {100, true}, {101, true}} {
			got := requestWithdrawal(t, e, service, wallet.Available, 8101, tc.amount, fmt.Sprintf("THRESHOLD-%04d", tc.amount), 51+tc.amount)
			if got.Withdrawal.RequiresReview != tc.review {
				t.Fatalf("amount=%d review=%v", tc.amount, got.Withdrawal.RequiresReview)
			}
		}
		defaultService := ledger.New(e.app, ledger.Options{AccountGuard: withdrawalGuard{}, PayeeCipher: cipher})
		below := requestWithdrawal(t, e, defaultService, wallet.Available, 8101, 9_999_999, "DEFAULT-0001", 61)
		at := requestWithdrawal(t, e, defaultService, wallet.Available, 8101, 10_000_000, "DEFAULT-0002", 62)
		if below.Withdrawal.RequiresReview || !at.Withdrawal.RequiresReview {
			t.Fatal("default threshold is not 10_000_000")
		}
	})

	t.Run("failure restores applicant wallet and amount cannot be tampered", func(t *testing.T) {
		before := accountBalance(t, e, wallet.Available)
		result := requestWithdrawal(t, e, service, wallet.Available, 8101, 40, "FAIL-4444", 71)
		assertSQLState(t, execErr(e.ctx, e.admin, `UPDATE ledger.withdrawal SET amount=amount+1 WHERE id=$1`, result.Withdrawal.ID), "55000")
		failed, err := service.FailWithdrawal(e.ctx, ledger.FailWithdrawalInput{WithdrawalID: result.Withdrawal.ID, Reason: "bank rejected", ActorID: 72})
		if err != nil || failed.Replayed {
			t.Fatalf("failure result=%+v err=%v", failed, err)
		}
		failedReplay, err := service.FailWithdrawal(e.ctx, ledger.FailWithdrawalInput{WithdrawalID: result.Withdrawal.ID, Reason: "bank rejected", ActorID: 72})
		if err != nil || !failedReplay.Replayed || failedReplay.TxnID != failed.TxnID {
			t.Fatalf("failure replay=%+v err=%v", failedReplay, err)
		}
		assertWithdrawalEvent(t, e, result.Withdrawal.PublicID, "ledger.withdrawal.failed", "FAIL-4444", 72)
		if got := accountBalance(t, e, wallet.Available); got != before {
			t.Fatalf("wallet=%d want=%d", got, before)
		}
		assertSQLState(t, execErr(e.ctx, e.app, `DELETE FROM ledger.withdrawal WHERE id=$1`, result.Withdrawal.ID), "42501")
	})

	t.Run("requested and review states can cancel but direct invalid inserts fail", func(t *testing.T) {
		requested := requestWithdrawal(t, e, service, wallet.Available, 8101, 100, "CANCEL-REQUESTED-1", 75)
		if _, err := service.FailWithdrawal(e.ctx, ledger.FailWithdrawalInput{WithdrawalID: requested.Withdrawal.ID, Reason: "too early", ActorID: 76}); !errors.Is(err, ledger.ErrWithdrawalState) {
			t.Fatalf("requested failure error=%v", err)
		}
		cancelled, err := service.CancelWithdrawal(e.ctx, ledger.CancelWithdrawalInput{WithdrawalID: requested.Withdrawal.ID, Reason: "cancel requested", ActorID: 76})
		if err != nil || cancelled.Replayed {
			t.Fatalf("cancellation result=%+v err=%v", cancelled, err)
		}
		cancelledReplay, err := service.CancelWithdrawal(e.ctx, ledger.CancelWithdrawalInput{WithdrawalID: requested.Withdrawal.ID, Reason: "cancel requested", ActorID: 76})
		if err != nil || !cancelledReplay.Replayed || cancelledReplay.TxnID != cancelled.TxnID {
			t.Fatalf("cancellation replay=%+v err=%v", cancelledReplay, err)
		}
		assertWithdrawalEvent(t, e, requested.Withdrawal.PublicID, "ledger.withdrawal.cancelled", "CANCEL-REQUESTED-1", 76)
		review := requestWithdrawal(t, e, service, wallet.Available, 8101, 100, "CANCEL-REVIEW-2", 77)
		if _, err := service.StartReview(e.ctx, ledger.StartReviewInput{WithdrawalID: review.Withdrawal.ID, MakerActor: 78}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.FailWithdrawal(e.ctx, ledger.FailWithdrawalInput{WithdrawalID: review.Withdrawal.ID, Reason: "too early", ActorID: 79}); !errors.Is(err, ledger.ErrWithdrawalState) {
			t.Fatalf("review failure error=%v", err)
		}
		reviewCancelled, err := service.CancelWithdrawal(e.ctx, ledger.CancelWithdrawalInput{WithdrawalID: review.Withdrawal.ID, Reason: "cancel review", ActorID: 79})
		if err != nil || reviewCancelled.Replayed {
			t.Fatalf("review cancellation result=%+v err=%v", reviewCancelled, err)
		}
		reviewCancelledReplay, err := service.CancelWithdrawal(e.ctx, ledger.CancelWithdrawalInput{WithdrawalID: review.Withdrawal.ID, Reason: "cancel review", ActorID: 79})
		if err != nil || !reviewCancelledReplay.Replayed || reviewCancelledReplay.TxnID != reviewCancelled.TxnID {
			t.Fatalf("review cancellation replay=%+v err=%v", reviewCancelledReplay, err)
		}
		assertWithdrawalEvent(t, e, review.Withdrawal.PublicID, "ledger.withdrawal.cancelled", "CANCEL-REVIEW-2", 79)
		assertSQLState(t, execErr(e.ctx, e.app, `INSERT INTO ledger.withdrawal(public_id,request_key,account_id,amount,payee_account_text,status,requester_actor,requires_review) VALUES('01ARZ3NDEKTSV4RRFFQ69G5FZZ','invalid-direct-insert',8101,1,'cipher','paid',80,false)`), "55000")
	})

	t.Run("bank reference is unique", func(t *testing.T) {
		first := requestWithdrawal(t, e, service, wallet.Available, 8101, 20, "BANKREF-1", 81)
		second := requestWithdrawal(t, e, service, wallet.Available, 8101, 20, "BANKREF-2", 82)
		if _, err := service.ConfirmPayout(e.ctx, ledger.ConfirmPayoutInput{WithdrawalID: first.Withdrawal.ID, BankRef: "DUP-REF", ActorID: 83}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.ConfirmPayout(e.ctx, ledger.ConfirmPayoutInput{WithdrawalID: second.Withdrawal.ID, BankRef: "DUP-REF", ActorID: 83}); err == nil {
			t.Fatal("duplicate bank reference accepted")
		}
	})

	t.Run("overdue and large pending omit payee", func(t *testing.T) {
		result := requestWithdrawal(t, e, service, wallet.Available, 8101, 100, "OVERDUE-SECRET-4321", 91)
		requestedAt := result.Withdrawal.RequestedAt
		atBoundary, err := service.ListOverdueWithdrawals(e.ctx, requestedAt.Add(24*time.Hour), 0)
		if err != nil || containsPending(atBoundary, result.Withdrawal.ID) {
			t.Fatalf("boundary list=%v err=%v", atBoundary, err)
		}
		overdue, err := service.ListOverdueWithdrawals(e.ctx, requestedAt.Add(24*time.Hour+time.Microsecond), 0)
		if err != nil || !containsPending(overdue, result.Withdrawal.ID) {
			t.Fatalf("overdue list=%v err=%v", overdue, err)
		}
		large, err := service.ListLargePending(e.ctx)
		if err != nil || !containsPending(large, result.Withdrawal.ID) {
			t.Fatalf("large list=%v err=%v", large, err)
		}
		if strings.Contains(fmt.Sprint(overdue, large), "OVERDUE-SECRET") {
			t.Fatal("pending list exposed payee")
		}
	})

	if metrics.requested.Load() == 0 || metrics.reviewed.Load() == 0 || metrics.paid.Load() == 0 || metrics.failed.Load() == 0 || metrics.cancelled.Load() == 0 || metrics.overdue.Load() == 0 {
		t.Fatalf("metrics not populated: %+v", metrics)
	}
}

func TestWithdrawalConcurrencyIntegration(t *testing.T) {
	e := newBusinessEnv(t)
	service := ledger.New(e.app, ledger.Options{AccountGuard: withdrawalGuard{}, PayeeCipher: withdrawalCipher(t), LargeWithdrawalThreshold: 1_000_000})
	wallet := mustWallet(t, e, 8201, nil)
	setBalance(t, e, wallet.Available, 1_000)
	setSingletonBalance(t, e, "BANK_DEDICATED", 1_000)

	t.Run("one hundred identical request keys create once", func(t *testing.T) {
		const workers = 100
		input := ledger.RequestWithdrawalInput{
			RequestKey: "same-request-100", RequesterAccountID: 8201, WalletAvailable: wallet.Available,
			Amount: 25, PayeeAccount: "IDEMPOTENT-ACCOUNT-7777", ActorID: 100, SecondFactorVerified: true,
		}
		beforeBalance := accountBalance(t, e, wallet.Available)
		results := make(chan ledger.RequestWithdrawalResult, workers)
		errorsCh := make(chan error, workers)
		start := make(chan struct{})
		var group sync.WaitGroup
		for range workers {
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				result, err := service.RequestWithdrawal(e.ctx, input)
				if err != nil {
					errorsCh <- err
					return
				}
				results <- result
			}()
		}
		close(start)
		group.Wait()
		close(errorsCh)
		close(results)
		for err := range errorsCh {
			t.Errorf("request error=%v", err)
		}
		var withdrawalID, txnID int64
		var first, replayed int
		for result := range results {
			if withdrawalID == 0 {
				withdrawalID, txnID = result.Withdrawal.ID, result.Post.TxnID
			}
			if result.Withdrawal.ID != withdrawalID || result.Post.TxnID != txnID {
				t.Fatalf("result withdrawal=%d txn=%d want withdrawal=%d txn=%d", result.Withdrawal.ID, result.Post.TxnID, withdrawalID, txnID)
			}
			if result.Post.Replayed {
				replayed++
			} else {
				first++
			}
		}
		if first != 1 || replayed != workers-1 {
			t.Fatalf("first=%d replayed=%d", first, replayed)
		}
		var withdrawals, txns int
		if err := e.admin.QueryRow(e.ctx, `
			SELECT (SELECT count(*) FROM ledger.withdrawal WHERE request_key=$1),
			       (SELECT count(*) FROM ledger.txn WHERE idem_key=$2)`, input.RequestKey, "withdraw_req:request:"+input.RequestKey).Scan(&withdrawals, &txns); err != nil {
			t.Fatal(err)
		}
		if withdrawals != 1 || txns != 1 || accountBalance(t, e, wallet.Available) != beforeBalance-input.Amount {
			t.Fatalf("withdrawals=%d txns=%d balance=%d", withdrawals, txns, accountBalance(t, e, wallet.Available))
		}

		conflict := input
		conflict.Amount++
		if _, err := service.RequestWithdrawal(e.ctx, conflict); !errors.Is(err, ledger.ErrIdempotencyConflict) {
			t.Fatalf("changed amount error=%v", err)
		}
		conflict = input
		conflict.PayeeAccount = "DIFFERENT-ACCOUNT-8888"
		if _, err := service.RequestWithdrawal(e.ctx, conflict); !errors.Is(err, ledger.ErrIdempotencyConflict) {
			t.Fatalf("changed payee error=%v", err)
		}
		other := mustWallet(t, e, 8299, nil)
		conflict = input
		conflict.WalletAvailable = other.Available
		if _, err := service.RequestWithdrawal(e.ctx, conflict); !errors.Is(err, ledger.ErrIdempotencyConflict) {
			t.Fatalf("changed wallet error=%v", err)
		}
		conflict = input
		conflict.RequesterAccountID = 8299
		if _, err := service.RequestWithdrawal(e.ctx, conflict); !errors.Is(err, ledger.ErrIdempotencyConflict) {
			t.Fatalf("changed applicant error=%v", err)
		}
		distinct := input
		distinct.RequestKey = "different-request-101"
		distinct.ActorID++
		created, err := service.RequestWithdrawal(e.ctx, distinct)
		if err != nil || created.Post.Replayed || created.Withdrawal.ID == withdrawalID {
			t.Fatalf("distinct result=%+v err=%v", created, err)
		}
	})

	t.Run("one hundred identical confirmations post once", func(t *testing.T) {
		wd := requestWithdrawal(t, e, service, wallet.Available, 8201, 100, "CONCURRENT-1000", 101)
		const workers = 100
		results := make(chan ledger.PostResult, workers)
		errs := make(chan error, workers)
		var group sync.WaitGroup
		for range workers {
			group.Add(1)
			go func() {
				defer group.Done()
				result, err := service.ConfirmPayout(e.ctx, ledger.ConfirmPayoutInput{WithdrawalID: wd.Withdrawal.ID, BankRef: "CONCURRENT-REF", ActorID: 102})
				results <- result
				errs <- err
			}()
		}
		group.Wait()
		close(results)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("confirm error=%v", err)
			}
		}
		var first, replayed int
		var txnID int64
		for result := range results {
			if txnID == 0 {
				txnID = result.TxnID
			}
			if result.TxnID != txnID {
				t.Fatalf("txn=%d want=%d", result.TxnID, txnID)
			}
			if result.Replayed {
				replayed++
			} else {
				first++
			}
		}
		if first != 1 || replayed != workers-1 {
			t.Fatalf("first=%d replayed=%d", first, replayed)
		}
	})

	t.Run("payout failure and cancellation have one winner", func(t *testing.T) {
		wd := requestWithdrawal(t, e, service, wallet.Available, 8201, 30, "RACE-3000", 111)
		calls := []func() error{
			func() error {
				_, err := service.ConfirmPayout(e.ctx, ledger.ConfirmPayoutInput{WithdrawalID: wd.Withdrawal.ID, BankRef: "RACE-REF", ActorID: 112})
				return err
			},
			func() error {
				_, err := service.CancelWithdrawal(e.ctx, ledger.CancelWithdrawalInput{WithdrawalID: wd.Withdrawal.ID, Reason: "cancel", ActorID: 113})
				return err
			},
			func() error {
				_, err := service.FailWithdrawal(e.ctx, ledger.FailWithdrawalInput{WithdrawalID: wd.Withdrawal.ID, Reason: "fail", ActorID: 114})
				return err
			},
		}
		var wins atomic.Int64
		var group sync.WaitGroup
		for _, call := range calls {
			group.Add(1)
			go func(call func() error) {
				defer group.Done()
				if call() == nil {
					wins.Add(1)
				}
			}(call)
		}
		group.Wait()
		if wins.Load() != 1 {
			t.Fatalf("winners=%d", wins.Load())
		}
	})

	t.Run("concurrent requests cannot overdraw", func(t *testing.T) {
		limited := mustWallet(t, e, 8202, nil)
		setBalance(t, e, limited.Available, 10)
		addSingletonBalance(t, e, "BANK_DEDICATED", 10)
		var success atomic.Int64
		var group sync.WaitGroup
		for i := range 100 {
			group.Add(1)
			go func(i int) {
				defer group.Done()
				_, err := service.RequestWithdrawal(e.ctx, ledger.RequestWithdrawalInput{RequestKey: fmt.Sprintf("overdraw-%04d", i), RequesterAccountID: 8202, WalletAvailable: limited.Available, Amount: 1, PayeeAccount: fmt.Sprintf("P-%04d", i), ActorID: int64(200 + i), SecondFactorVerified: true})
				if err == nil {
					success.Add(1)
				} else if !errors.Is(err, ledger.ErrInsufficientFunds) {
					t.Errorf("request error=%v", err)
				}
			}(i)
		}
		group.Wait()
		if success.Load() != 10 || accountBalance(t, e, limited.Available) != 0 {
			t.Fatalf("success=%d balance=%d", success.Load(), accountBalance(t, e, limited.Available))
		}
		if mismatches, err := service.VerifyPendingPayoutMatchesWithdrawals(e.ctx); err != nil || len(mismatches) != 0 {
			t.Fatalf("pending mismatch=%v err=%v", mismatches, err)
		}
	})

	t.Run("randomized three hundred step invariant", func(t *testing.T) {
		randomWallet := mustWallet(t, e, 8203, nil)
		setBalance(t, e, randomWallet.Available, 300)
		addSingletonBalance(t, e, "BANK_DEDICATED", 300)
		rng := rand.New(rand.NewSource(20261013))
		for step := range 300 {
			wd := requestWithdrawal(t, e, service, randomWallet.Available, 8203, 1, fmt.Sprintf("RANDOM-%04d", step), int64(1000+step))
			var err error
			switch rng.Intn(3) {
			case 0:
				_, err = service.ConfirmPayout(e.ctx, ledger.ConfirmPayoutInput{WithdrawalID: wd.Withdrawal.ID, BankRef: fmt.Sprintf("RND-REF-%04d", step), ActorID: 2001})
			case 1:
				_, err = service.FailWithdrawal(e.ctx, ledger.FailWithdrawalInput{WithdrawalID: wd.Withdrawal.ID, Reason: "random failure", ActorID: 2002})
			case 2:
				_, err = service.CancelWithdrawal(e.ctx, ledger.CancelWithdrawalInput{WithdrawalID: wd.Withdrawal.ID, Reason: "random cancel", ActorID: 2003})
			}
			if err != nil {
				t.Fatalf("step=%d close error=%v", step, err)
			}
			if mismatches, err := service.VerifyPendingPayoutMatchesWithdrawals(e.ctx); err != nil || len(mismatches) != 0 {
				t.Fatalf("step=%d pending mismatch=%v err=%v", step, mismatches, err)
			}
			assertAccountingInvariant(t, e)
		}
	})
}

func withdrawalCipher(t *testing.T) *secrets.Envelope {
	t.Helper()
	key := make([]byte, secrets.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	ring, err := secrets.NewKeyring(map[string][]byte{"test-key": key}, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewEnvelope(ring)
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

func requestWithdrawal(t *testing.T, e businessEnv, service *ledger.Service, wallet, owner, amount int64, payee string, actor int64) ledger.RequestWithdrawalResult {
	t.Helper()
	result, err := service.RequestWithdrawal(e.ctx, ledger.RequestWithdrawalInput{RequestKey: fmt.Sprintf("test-request-%d", withdrawalRequestKey.Add(1)), RequesterAccountID: owner, WalletAvailable: wallet, Amount: amount, PayeeAccount: payee, ActorID: actor, SecondFactorVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertWithdrawalEvent(t *testing.T, e businessEnv, publicID, topic, secret string, actorID int64) {
	t.Helper()
	var count int
	var payload string
	if err := e.admin.QueryRow(e.ctx, `
		SELECT count(*), min(payload::text)
		FROM ledger.outbox
		WHERE topic=$1 AND payload->>'public_id'=$2`, topic, publicID).Scan(&count, &payload); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("topic=%s public_id=%s event count=%d", topic, publicID, count)
	}
	if strings.Contains(payload, secret) {
		t.Fatalf("topic=%s leaked payee", topic)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatal(err)
	}
	amount, hasAmount := event["amount"].(float64)
	if event["public_id"] != publicID || event["occurred_at"] == nil || !hasAmount || amount <= 0 {
		t.Fatalf("topic=%s payload=%s", topic, payload)
	}
	foundActor := false
	for _, key := range []string{"actor_id", "maker_actor", "checker_actor"} {
		if value, ok := event[key].(float64); ok && int64(value) == actorID {
			foundActor = true
		}
	}
	if !foundActor {
		t.Fatalf("topic=%s payload lacks actor %d: %s", topic, actorID, payload)
	}
	if topic == "ledger.withdrawal.approved" && (event["maker_actor"] == nil || event["checker_actor"] == nil) {
		t.Fatalf("approval payload lacks maker/checker: %s", payload)
	}
	if strings.HasSuffix(topic, ".paid") || strings.HasSuffix(topic, ".failed") || strings.HasSuffix(topic, ".cancelled") {
		if event["actor_id"] == nil {
			t.Fatalf("financial payload lacks operator: %s", payload)
		}
	}
}

func setSingletonBalance(t *testing.T, e businessEnv, kind string, balance int64) {
	t.Helper()
	if _, err := e.admin.Exec(e.ctx, `UPDATE ledger.balance SET balance=$2 FROM ledger.account a WHERE ledger.balance.account_id=a.id AND a.kind=$1 AND a.owner_account IS NULL`, kind, balance); err != nil {
		t.Fatal(err)
	}
}

func addSingletonBalance(t *testing.T, e businessEnv, kind string, delta int64) {
	t.Helper()
	if _, err := e.admin.Exec(e.ctx, `UPDATE ledger.balance SET balance=balance+$2 FROM ledger.account a WHERE ledger.balance.account_id=a.id AND a.kind=$1 AND a.owner_account IS NULL`, kind, delta); err != nil {
		t.Fatal(err)
	}
}

func accountBalance(t *testing.T, e businessEnv, id int64) int64 {
	t.Helper()
	var balance int64
	if err := e.admin.QueryRow(e.ctx, `SELECT balance FROM ledger.balance WHERE account_id=$1`, id).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	return balance
}

func withdrawalCounts(t *testing.T, e businessEnv) [3]int64 {
	t.Helper()
	var result [3]int64
	if err := e.admin.QueryRow(e.ctx, `SELECT (SELECT count(*) FROM ledger.withdrawal),(SELECT count(*) FROM ledger.txn),(SELECT count(*) FROM ledger.outbox)`).Scan(&result[0], &result[1], &result[2]); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertNoSensitiveOutbox(t *testing.T, e businessEnv, plaintext string) {
	t.Helper()
	var leaked bool
	if err := e.admin.QueryRow(e.ctx, `SELECT EXISTS(SELECT 1 FROM ledger.outbox WHERE payload::text LIKE '%'||$1||'%')`, plaintext).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked {
		t.Fatal("outbox leaked payee plaintext")
	}
}

func containsPending(items []ledger.PendingWithdrawal, id int64) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
