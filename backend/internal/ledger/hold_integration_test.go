package ledger_test

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
)

func TestHoldIntegration(t *testing.T) {
	e := newBusinessEnv(t)
	buyer := mustWallet(t, e, 41001, nil)
	payee := mustWallet(t, e, 41002, nil)
	setBalance(t, e, buyer.Available, 100_000)

	t.Run("freeze creates one hold in the posting transaction and replays once", func(t *testing.T) {
		input := ledger.FreezeHoldInput{RefType: "plan_batch", RefID: 1, WalletAvailable: buyer.Available, WalletFrozen: buyer.Frozen, Amount: 100, ActorID: 11}
		first, err := e.appService.FreezeHold(e.ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if first.Hold.Status != "held" || first.Hold.FreezeTxnID != first.Post.TxnID || first.Post.Replayed {
			t.Fatalf("unexpected freeze result: %+v", first)
		}
		assertBusinessTxn(t, e, first.Post.TxnID, "freeze", "freeze:hold:plan_batch-1", "plan_batch", 1,
			[]expectedPosting{{"WALLET_AVAIL", "DEBIT", 100, ""}, {"WALLET_FROZEN", "CREDIT", 100, ""}})
		second, err := e.appService.FreezeHold(e.ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if !second.Post.Replayed || second.Hold.ID != first.Hold.ID || second.Post.TxnID != first.Post.TxnID {
			t.Fatalf("unexpected replay: %+v", second)
		}
		assertTxnCounts(t, e.ctx, e.admin, "freeze:hold:plan_batch-1", 1, 2, 1)
		assertHoldInvariant(t, e)

		listed, err := e.appService.ListHoldsByAccount(e.ctx, buyer.Frozen, []string{"held"})
		if err != nil || len(listed) != 1 || listed[0].ID != first.Hold.ID {
			t.Fatalf("listed=%+v error=%v", listed, err)
		}
		all, err := e.appService.ListHoldsByAccount(e.ctx, buyer.Frozen, nil)
		if err != nil || len(all) != 1 {
			t.Fatalf("all holds=%+v error=%v", all, err)
		}
	})

	t.Run("dispute does not move money and resolution requires separation", func(t *testing.T) {
		frozen, err := e.appService.FreezeHold(e.ctx, ledger.FreezeHoldInput{RefType: "plan_batch", RefID: 2, WalletAvailable: buyer.Available, WalletFrozen: buyer.Frozen, Amount: 200, ActorID: 11})
		if err != nil {
			t.Fatal(err)
		}
		before := getBalance(t, e.ctx, e.admin, buyer.Frozen)
		disputed, err := e.appService.DisputeHold(e.ctx, ledger.DisputeHoldInput{HoldID: frozen.Hold.ID, ActorID: 20, Reason: "quality dispute"})
		if err != nil {
			t.Fatal(err)
		}
		if disputed.Status != "disputed" || getBalance(t, e.ctx, e.admin, buyer.Frozen) != before {
			t.Fatalf("dispute moved funds or state wrong: %+v", disputed)
		}
		if _, err := e.appService.SettleHold(e.ctx, ledger.SettleHoldInput{HoldID: frozen.Hold.ID, PayeeAvailable: payee.Available, ActorID: 21}); !errors.Is(err, ledger.ErrHoldDisputed) {
			t.Fatalf("settle disputed error=%v", err)
		}
		if _, err := e.appService.RefundHold(e.ctx, ledger.RefundHoldInput{HoldID: frozen.Hold.ID, ActorID: 21, PayerActorID: 22}); !errors.Is(err, ledger.ErrHoldDisputed) {
			t.Fatalf("refund disputed error=%v", err)
		}
		if _, err := e.appService.ResolveDispute(e.ctx, ledger.ResolveDisputeInput{HoldID: frozen.Hold.ID, Resolution: "release", ResolvedBy: 21, PayerActorID: 21, PayeeAvailable: payee.Available}); !errors.Is(err, ledger.ErrSeparationDuty) {
			t.Fatalf("same actor error=%v", err)
		}
		result, err := e.appService.ResolveDispute(e.ctx, ledger.ResolveDisputeInput{HoldID: frozen.Hold.ID, Resolution: "release", ResolvedBy: 21, PayerActorID: 22, PayeeAvailable: payee.Available})
		if err != nil {
			t.Fatal(err)
		}
		hold, err := e.appService.GetHold(e.ctx, frozen.Hold.ID)
		if err != nil || hold.Status != "released" || hold.CloseTxnID == nil || *hold.CloseTxnID != result.TxnID || hold.ResolvedBy == nil || *hold.ResolvedBy != 21 {
			t.Fatalf("resolved hold=%+v error=%v", hold, err)
		}
		if _, err := e.appService.ResolveDispute(e.ctx, ledger.ResolveDisputeInput{HoldID: frozen.Hold.ID, Resolution: "release", ResolvedBy: 21, PayerActorID: 23, PayeeAvailable: payee.Available}); !errors.Is(err, ledger.ErrIdempotencyConflict) {
			t.Fatalf("changed payer replay error=%v", err)
		}
		assertBusinessTxn(t, e, result.TxnID, "settle", fmt.Sprintf("settle:hold:%d", frozen.Hold.ID), "hold", frozen.Hold.ID,
			[]expectedPosting{{"WALLET_FROZEN", "DEBIT", 200, ""}, {"WALLET_AVAIL", "CREDIT", 200, ""}})
		assertHoldInvariant(t, e)
	})

	t.Run("refund derives amount and wallet from hold", func(t *testing.T) {
		frozen, err := e.appService.FreezeHold(e.ctx, ledger.FreezeHoldInput{RefType: "plan_batch", RefID: 3, WalletAvailable: buyer.Available, WalletFrozen: buyer.Frozen, Amount: 300, ActorID: 11})
		if err != nil {
			t.Fatal(err)
		}
		result, err := e.appService.RefundHold(e.ctx, ledger.RefundHoldInput{HoldID: frozen.Hold.ID, ActorID: 31, PayerActorID: 32})
		if err != nil {
			t.Fatal(err)
		}
		var note string
		if err := e.admin.QueryRow(e.ctx, `SELECT note FROM ledger.txn WHERE id=$1`, result.TxnID).Scan(&note); err != nil || note != "payer_actor:32" {
			t.Fatalf("note=%q error=%v", note, err)
		}
		replayed, err := e.appService.RefundHold(e.ctx, ledger.RefundHoldInput{HoldID: frozen.Hold.ID, ActorID: 31, PayerActorID: 32})
		if err != nil || !replayed.Replayed || replayed.TxnID != result.TxnID {
			t.Fatalf("refund replay=%+v error=%v", replayed, err)
		}
		if _, err := e.appService.FreezeHold(e.ctx, ledger.FreezeHoldInput{RefType: "plan_batch", RefID: 3, WalletAvailable: buyer.Available, WalletFrozen: buyer.Frozen, Amount: 300, ActorID: 11}); !errors.Is(err, ledger.ErrIdempotencyConflict) {
			t.Fatalf("freeze replay after state change error=%v", err)
		}
		assertHoldInvariant(t, e)
	})

	t.Run("concurrent close has one winner", func(t *testing.T) {
		frozen, err := e.appService.FreezeHold(e.ctx, ledger.FreezeHoldInput{RefType: "plan_batch", RefID: 4, WalletAvailable: buyer.Available, WalletFrozen: buyer.Frozen, Amount: 400, ActorID: 11})
		if err != nil {
			t.Fatal(err)
		}
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := e.appService.SettleHold(e.ctx, ledger.SettleHoldInput{HoldID: frozen.Hold.ID, PayeeAvailable: payee.Available, ActorID: 41})
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, err := e.appService.RefundHold(e.ctx, ledger.RefundHoldInput{HoldID: frozen.Hold.ID, ActorID: 42, PayerActorID: 43})
			errs <- err
		}()
		wg.Wait()
		close(errs)
		success, stateFailure := 0, 0
		for err := range errs {
			if err == nil {
				success++
			} else if errors.Is(err, ledger.ErrHoldState) {
				stateFailure++
			} else {
				t.Fatalf("unexpected concurrent error=%v", err)
			}
		}
		if success != 1 || stateFailure != 1 {
			t.Fatalf("success=%d state_failure=%d", success, stateFailure)
		}
		assertHoldInvariant(t, e)
	})

	t.Run("identical concurrent closes replay one transaction", func(t *testing.T) {
		t.Run("settle", func(t *testing.T) {
			frozen, err := e.appService.FreezeHold(e.ctx, ledger.FreezeHoldInput{RefType: "plan_batch", RefID: 8, WalletAvailable: buyer.Available, WalletFrozen: buyer.Frozen, Amount: 800, ActorID: 11})
			if err != nil {
				t.Fatal(err)
			}
			buyerFrozenBefore := getBalance(t, e.ctx, e.admin, buyer.Frozen)
			payeeBefore := getBalance(t, e.ctx, e.admin, payee.Available)
			assertConcurrentReplay(t, 30, func() (ledger.PostResult, error) {
				return e.appService.SettleHold(e.ctx, ledger.SettleHoldInput{HoldID: frozen.Hold.ID, PayeeAvailable: payee.Available, ActorID: 81})
			})
			if got := getBalance(t, e.ctx, e.admin, buyer.Frozen); got != buyerFrozenBefore-800 {
				t.Fatalf("frozen balance=%d want=%d", got, buyerFrozenBefore-800)
			}
			if got := getBalance(t, e.ctx, e.admin, payee.Available); got != payeeBefore+800 {
				t.Fatalf("payee balance=%d want=%d", got, payeeBefore+800)
			}
		})

		t.Run("refund", func(t *testing.T) {
			frozen, err := e.appService.FreezeHold(e.ctx, ledger.FreezeHoldInput{RefType: "plan_batch", RefID: 9, WalletAvailable: buyer.Available, WalletFrozen: buyer.Frozen, Amount: 900, ActorID: 11})
			if err != nil {
				t.Fatal(err)
			}
			availableBefore := getBalance(t, e.ctx, e.admin, buyer.Available)
			frozenBefore := getBalance(t, e.ctx, e.admin, buyer.Frozen)
			assertConcurrentReplay(t, 30, func() (ledger.PostResult, error) {
				return e.appService.RefundHold(e.ctx, ledger.RefundHoldInput{HoldID: frozen.Hold.ID, ActorID: 91, PayerActorID: 92})
			})
			if got := getBalance(t, e.ctx, e.admin, buyer.Available); got != availableBefore+900 {
				t.Fatalf("available balance=%d want=%d", got, availableBefore+900)
			}
			if got := getBalance(t, e.ctx, e.admin, buyer.Frozen); got != frozenBefore-900 {
				t.Fatalf("frozen balance=%d want=%d", got, frozenBefore-900)
			}
		})

		t.Run("resolve dispute", func(t *testing.T) {
			frozen, err := e.appService.FreezeHold(e.ctx, ledger.FreezeHoldInput{RefType: "plan_batch", RefID: 10, WalletAvailable: buyer.Available, WalletFrozen: buyer.Frozen, Amount: 1000, ActorID: 11})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.appService.DisputeHold(e.ctx, ledger.DisputeHoldInput{HoldID: frozen.Hold.ID, ActorID: 101, Reason: "concurrent resolution"}); err != nil {
				t.Fatal(err)
			}
			frozenBefore := getBalance(t, e.ctx, e.admin, buyer.Frozen)
			payeeBefore := getBalance(t, e.ctx, e.admin, payee.Available)
			assertConcurrentReplay(t, 30, func() (ledger.PostResult, error) {
				return e.appService.ResolveDispute(e.ctx, ledger.ResolveDisputeInput{HoldID: frozen.Hold.ID, Resolution: "release", ResolvedBy: 102, PayerActorID: 103, PayeeAvailable: payee.Available})
			})
			if got := getBalance(t, e.ctx, e.admin, buyer.Frozen); got != frozenBefore-1000 {
				t.Fatalf("frozen balance=%d want=%d", got, frozenBefore-1000)
			}
			if got := getBalance(t, e.ctx, e.admin, payee.Available); got != payeeBefore+1000 {
				t.Fatalf("payee balance=%d want=%d", got, payeeBefore+1000)
			}
		})
		assertHoldInvariant(t, e)
	})

	t.Run("one hundred concurrent freezes create one hold", func(t *testing.T) {
		input := ledger.FreezeHoldInput{RefType: "plan_batch", RefID: 5, WalletAvailable: buyer.Available, WalletFrozen: buyer.Frozen, Amount: 500, ActorID: 11}
		results := make(chan ledger.FreezeHoldResult, 100)
		errs := make(chan error, 100)
		var wg sync.WaitGroup
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				got, err := e.appService.FreezeHold(e.ctx, input)
				if err != nil {
					errs <- err
					return
				}
				results <- got
			}()
		}
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		var holdID, txnID int64
		fresh, replayed := 0, 0
		for result := range results {
			if holdID == 0 {
				holdID, txnID = result.Hold.ID, result.Post.TxnID
			} else if result.Hold.ID != holdID || result.Post.TxnID != txnID {
				t.Fatalf("different replay result: %+v", result)
			}
			if result.Post.Replayed {
				replayed++
			} else {
				fresh++
			}
		}
		if fresh != 1 || replayed != 99 {
			t.Fatalf("fresh=%d replayed=%d", fresh, replayed)
		}
		assertTxnCounts(t, e.ctx, e.admin, "freeze:hold:plan_batch-5", 1, 2, 1)
		var count int
		if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM ledger.hold WHERE ref_type='plan_batch' AND ref_id=5`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("hold count=%d error=%v", count, err)
		}
		assertHoldInvariant(t, e)
	})

	t.Run("insufficient funds leave no records", func(t *testing.T) {
		empty := mustWallet(t, e, 41999, nil)
		_, err := e.appService.FreezeHold(e.ctx, ledger.FreezeHoldInput{RefType: "plan_batch", RefID: 6, WalletAvailable: empty.Available, WalletFrozen: empty.Frozen, Amount: 1, ActorID: 11})
		if !errors.Is(err, ledger.ErrInsufficientFunds) {
			t.Fatalf("error=%v", err)
		}
		assertTxnCounts(t, e.ctx, e.admin, "freeze:hold:plan_batch-6", 0, 0, 0)
		var count int
		if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM ledger.hold WHERE ref_type='plan_batch' AND ref_id=6`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("hold count=%d error=%v", count, err)
		}
	})

	t.Run("database trigger and permissions guard holds", func(t *testing.T) {
		frozen, err := e.appService.FreezeHold(e.ctx, ledger.FreezeHoldInput{RefType: "plan_batch", RefID: 7, WalletAvailable: buyer.Available, WalletFrozen: buyer.Frozen, Amount: 700, ActorID: 11})
		if err != nil {
			t.Fatal(err)
		}
		assertState(t, execError(e.ctx, e.admin, `UPDATE ledger.hold SET amount=701 WHERE id=$1`, frozen.Hold.ID), "55000")
		assertState(t, execError(e.ctx, e.admin, `INSERT INTO ledger.hold(account_id,ref_type,ref_id,amount,status,freeze_txn_id) VALUES($1,'invalid_dispute',7001,1,'disputed',$2)`, buyer.Frozen, frozen.Post.TxnID), "55000")
		var nonFreezeTxnID int64
		if err := e.admin.QueryRow(e.ctx, `SELECT id FROM ledger.txn WHERE type<>'freeze' ORDER BY id LIMIT 1`).Scan(&nonFreezeTxnID); err != nil {
			t.Fatal(err)
		}
		for name, connection := range map[string]execer{"admin": e.admin, "app_ledger": e.app} {
			t.Run(name+" insert guard", func(t *testing.T) {
				assertState(t, execError(e.ctx, connection, `INSERT INTO ledger.hold(account_id,ref_type,ref_id,amount,status,freeze_txn_id,close_txn_id,closed_at) VALUES($1,'raw_released',$2,1,'released',$3,$3,now())`, buyer.Frozen, 7100+frozen.Hold.ID, frozen.Post.TxnID), "55000")
				assertState(t, execError(e.ctx, connection, `INSERT INTO ledger.hold(account_id,ref_type,ref_id,amount,status,freeze_txn_id) VALUES($1,'raw_account',$2,1,'held',$3)`, buyer.Available, 7200+frozen.Hold.ID, frozen.Post.TxnID), "55000")
				assertState(t, execError(e.ctx, connection, `INSERT INTO ledger.hold(account_id,ref_type,ref_id,amount,status,freeze_txn_id) VALUES($1,'raw_txn',$2,1,'held',$3)`, buyer.Frozen, 7300+frozen.Hold.ID, nonFreezeTxnID), "55000")
			})
		}
		assertState(t, execError(e.ctx, e.admin, `UPDATE ledger.hold SET status='released' WHERE id=$1`, frozen.Hold.ID), "55000")
		assertState(t, execError(e.ctx, e.admin, `DELETE FROM ledger.hold WHERE id=$1`, frozen.Hold.ID), "55000")
		assertState(t, execError(e.ctx, e.app, `UPDATE ledger.hold SET amount=701 WHERE id=$1`, frozen.Hold.ID), "42501")
		assertState(t, execError(e.ctx, e.app, `UPDATE ledger.hold SET status='released' WHERE id=$1`, frozen.Hold.ID), "55000")
		assertState(t, execError(e.ctx, e.app, `DELETE FROM ledger.hold WHERE id=$1`, frozen.Hold.ID), "42501")
		assertState(t, execError(e.ctx, e.admin, `TRUNCATE ledger.hold`), "55000")
		assertState(t, execError(e.ctx, e.app, `TRUNCATE ledger.hold`), "42501")
		if _, err := e.appService.SettleHold(e.ctx, ledger.SettleHoldInput{HoldID: frozen.Hold.ID, PayeeAvailable: payee.Available, ActorID: 70}); err != nil {
			t.Fatal(err)
		}
		assertState(t, execError(e.ctx, e.admin, `UPDATE ledger.hold SET status='refunded' WHERE id=$1`, frozen.Hold.ID), "55000")
		assertHoldInvariant(t, e)
	})
}

func TestHoldRandomSequenceMaintainsInvariant(t *testing.T) {
	e := newBusinessEnv(t)
	buyer := mustWallet(t, e, 42001, nil)
	payee := mustWallet(t, e, 42002, nil)
	setBalance(t, e, buyer.Available, 1_000_000)
	rng := rand.New(rand.NewSource(20261013))
	for step := range 200 {
		amount := int64(rng.Intn(1000) + 1)
		frozen, err := e.appService.FreezeHold(e.ctx, ledger.FreezeHoldInput{RefType: "random_batch", RefID: int64(step + 1), WalletAvailable: buyer.Available, WalletFrozen: buyer.Frozen, Amount: amount, ActorID: 1})
		if err != nil {
			t.Fatal(err)
		}
		assertHoldInvariant(t, e)
		switch rng.Intn(4) {
		case 0:
			_, err = e.appService.SettleHold(e.ctx, ledger.SettleHoldInput{HoldID: frozen.Hold.ID, PayeeAvailable: payee.Available, ActorID: 2})
		case 1:
			_, err = e.appService.RefundHold(e.ctx, ledger.RefundHoldInput{HoldID: frozen.Hold.ID, ActorID: 2, PayerActorID: 3})
		case 2:
			if _, err = e.appService.DisputeHold(e.ctx, ledger.DisputeHoldInput{HoldID: frozen.Hold.ID, ActorID: 4, Reason: "fixed-seed dispute"}); err == nil {
				_, err = e.appService.ResolveDispute(e.ctx, ledger.ResolveDisputeInput{HoldID: frozen.Hold.ID, Resolution: "release", ResolvedBy: 5, PayerActorID: 6, PayeeAvailable: payee.Available})
			}
		case 3:
			if _, err = e.appService.DisputeHold(e.ctx, ledger.DisputeHoldInput{HoldID: frozen.Hold.ID, ActorID: 4, Reason: "fixed-seed dispute"}); err == nil {
				_, err = e.appService.ResolveDispute(e.ctx, ledger.ResolveDisputeInput{HoldID: frozen.Hold.ID, Resolution: "refund", ResolvedBy: 5, PayerActorID: 6})
			}
		}
		if err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
		assertHoldInvariant(t, e)
	}
}

func assertHoldInvariant(t *testing.T, e businessEnv) {
	t.Helper()
	mismatches, err := e.appService.VerifyHoldsMatchFrozen(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(mismatches) != 0 {
		t.Fatalf("hold invariant mismatches: %s", fmt.Sprint(mismatches))
	}
}

func assertConcurrentReplay(t *testing.T, count int, call func() (ledger.PostResult, error)) int64 {
	t.Helper()
	results := make(chan ledger.PostResult, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := call()
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Errorf("concurrent request: %v", err)
	}
	var txnID int64
	fresh, replayed, total := 0, 0, 0
	for result := range results {
		total++
		if txnID == 0 {
			txnID = result.TxnID
		} else if result.TxnID != txnID {
			t.Errorf("txn=%d want=%d", result.TxnID, txnID)
		}
		if result.Replayed {
			replayed++
		} else {
			fresh++
		}
	}
	if total != count || fresh != 1 || replayed != count-1 {
		t.Fatalf("total=%d fresh=%d replayed=%d", total, fresh, replayed)
	}
	return txnID
}
