package ledger_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
)

func TestAdjustIntegration(t *testing.T) {
	e := newBusinessEnv(t)
	platform := singletonID(t, e.ctx, e.admin, "PLATFORM_HELD")
	suspense := singletonID(t, e.ctx, e.admin, "SUSPENSE")
	payable := singletonID(t, e.ctx, e.admin, "PAYABLE_CARRIER")
	bank := singletonID(t, e.ctx, e.admin, "BANK_DEDICATED")
	escrow := singletonID(t, e.ctx, e.admin, "DELIVERY_ESCROW")
	pending := singletonID(t, e.ctx, e.admin, "PENDING_PAYOUT")
	merchantID := int64(71200)
	wallet := mustWallet(t, e, 71100, &merchantID)
	for _, accountID := range []int64{platform, suspense, payable, bank, escrow, pending, wallet.Available, wallet.Frozen, wallet.AdBudget} {
		setBalance(t, e, accountID, 10_000)
	}

	t.Run("app_ledger posts an adjustment and one business event", func(t *testing.T) {
		var currentUser string
		if err := e.app.QueryRow(e.ctx, `SELECT current_user`).Scan(&currentUser); err != nil || currentUser != "app_ledger" {
			t.Fatalf("current_user=%q error=%v", currentUser, err)
		}
		input := ledger.AdjustInput{
			ReasonCode: "recon_correction", Reason: "clear reviewed variance", RefType: "recon", RefID: 71001,
			DebitAccountID: platform, CreditAccountID: suspense, Amount: 125, ActorID: 101, ReviewerID: 202,
		}
		beforeDebit, beforeCredit := getBalance(t, e.ctx, e.admin, platform), getBalance(t, e.ctx, e.admin, suspense)
		result, err := e.appService.Adjust(e.ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if result.Replayed {
			t.Fatal("first adjustment reported as replay")
		}
		if got := getBalance(t, e.ctx, e.admin, platform); got != beforeDebit-input.Amount {
			t.Fatalf("debit account balance=%d", got)
		}
		if got := getBalance(t, e.ctx, e.admin, suspense); got != beforeCredit+input.Amount {
			t.Fatalf("credit account balance=%d", got)
		}
		assertAdjustmentPostings(t, e, result.TxnID, input)
		assertAdjustmentEvent(t, e, result.TxnID, input, 1)

		replay, err := e.appService.Adjust(e.ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if !replay.Replayed || replay.TxnID != result.TxnID {
			t.Fatalf("replay=%+v original=%+v", replay, result)
		}
		assertAdjustmentEvent(t, e, result.TxnID, input, 1)
		assertTxnCounts(t, e.ctx, e.admin, "adjust:recon-71001", 1, 2, 2)

		changed := input
		changed.Amount++
		if _, err := e.appService.Adjust(e.ctx, changed); !errors.Is(err, ledger.ErrIdempotencyConflict) {
			t.Fatalf("conflicting replay error=%v", err)
		}
	})

	t.Run("validation and forbidden account kinds leave no residue", func(t *testing.T) {
		base := ledger.AdjustInput{
			ReasonCode: "manual_review", Reason: "reviewed adjustment", RefType: "case",
			DebitAccountID: platform, CreditAccountID: suspense, Amount: 10, ActorID: 301, ReviewerID: 302,
		}
		invalid := []struct {
			name string
			edit func(*ledger.AdjustInput)
			want error
		}{
			{"same actor", func(in *ledger.AdjustInput) { in.ReviewerID = in.ActorID }, ledger.ErrSeparationDuty},
			{"empty reason code", func(in *ledger.AdjustInput) { in.ReasonCode = " " }, ledger.ErrInvalidRequest},
			{"empty reason", func(in *ledger.AdjustInput) { in.Reason = " " }, ledger.ErrInvalidRequest},
			{"zero amount", func(in *ledger.AdjustInput) { in.Amount = 0 }, ledger.ErrInvalidRequest},
			{"bank", func(in *ledger.AdjustInput) { in.DebitAccountID = bank }, ledger.ErrInvalidRequest},
			{"wallet", func(in *ledger.AdjustInput) { in.CreditAccountID = wallet.Available }, ledger.ErrInvalidRequest},
			{"frozen", func(in *ledger.AdjustInput) { in.CreditAccountID = wallet.Frozen }, ledger.ErrInvalidRequest},
			{"ad budget", func(in *ledger.AdjustInput) { in.CreditAccountID = wallet.AdBudget }, ledger.ErrInvalidRequest},
			{"escrow", func(in *ledger.AdjustInput) { in.CreditAccountID = escrow }, ledger.ErrInvalidRequest},
			{"pending payout", func(in *ledger.AdjustInput) { in.CreditAccountID = pending }, ledger.ErrInvalidRequest},
		}
		for i, tc := range invalid {
			t.Run(tc.name, func(t *testing.T) {
				input := base
				input.RefID = int64(71100 + i)
				tc.edit(&input)
				if _, err := e.appService.Adjust(e.ctx, input); !errors.Is(err, tc.want) {
					t.Fatalf("error=%v, want %v", err, tc.want)
				}
				assertNoAdjustmentResidue(t, e, fmt.Sprintf("adjust:case-%d", input.RefID))
			})
		}
	})

	t.Run("one hundred concurrent retries post once", func(t *testing.T) {
		input := ledger.AdjustInput{
			ReasonCode: "batch_fix", Reason: "approved batch correction", RefType: "case", RefID: 71200,
			DebitAccountID: platform, CreditAccountID: payable, Amount: 7, ActorID: 401, ReviewerID: 402,
		}
		beforeDebit, beforeCredit := getBalance(t, e.ctx, e.admin, platform), getBalance(t, e.ctx, e.admin, payable)
		var fresh atomic.Int64
		var wg sync.WaitGroup
		errs := make(chan error, 100)
		ids := make(chan int64, 100)
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := e.appService.Adjust(e.ctx, input)
				if err != nil {
					errs <- err
					return
				}
				if !result.Replayed {
					fresh.Add(1)
				}
				ids <- result.TxnID
			}()
		}
		wg.Wait()
		close(errs)
		close(ids)
		for err := range errs {
			t.Error(err)
		}
		var txnID int64
		for id := range ids {
			if txnID == 0 {
				txnID = id
			} else if id != txnID {
				t.Errorf("txn id=%d, want %d", id, txnID)
			}
		}
		if fresh.Load() != 1 {
			t.Fatalf("fresh adjustments=%d", fresh.Load())
		}
		if getBalance(t, e.ctx, e.admin, platform) != beforeDebit-input.Amount || getBalance(t, e.ctx, e.admin, payable) != beforeCredit+input.Amount {
			t.Fatal("concurrent replay changed balances more than once")
		}
		assertAdjustmentEvent(t, e, txnID, input, 1)
		assertTxnCounts(t, e.ctx, e.admin, "adjust:case-71200", 1, 2, 2)
	})

	t.Run("random adjustment sequence preserves invariants", func(t *testing.T) {
		accounts := []int64{platform, suspense, payable}
		for _, accountID := range accounts {
			setBalance(t, e, accountID, 100_000)
		}
		var liabilities int64
		if err := e.admin.QueryRow(e.ctx, `SELECT COALESCE(sum(b.balance),0) FROM ledger.balance b JOIN ledger.account a ON a.id=b.account_id WHERE a.normal_side='CREDIT'`).Scan(&liabilities); err != nil {
			t.Fatal(err)
		}
		setBalance(t, e, bank, liabilities)
		before := platformAccountTotal(t, e, accounts)
		random := rand.New(rand.NewSource(712))
		for step := range 100 {
			from := random.Intn(len(accounts))
			to := random.Intn(len(accounts) - 1)
			if to >= from {
				to++
			}
			_, err := e.appService.Adjust(e.ctx, ledger.AdjustInput{
				ReasonCode: "random_invariant", Reason: "test sequence", RefType: "random", RefID: int64(step + 1),
				DebitAccountID: accounts[from], CreditAccountID: accounts[to], Amount: int64(random.Intn(20) + 1), ActorID: 501, ReviewerID: 502,
			})
			if err != nil {
				t.Fatalf("step %d: %v", step, err)
			}
		}
		if after := platformAccountTotal(t, e, accounts); after != before {
			t.Fatalf("platform total=%d, want %d", after, before)
		}
		assertJournalBalanced(t, e.ctx, e.admin)
		assertAccountingInvariant(t, e)
	})
}

func assertAdjustmentPostings(t *testing.T, e businessEnv, txnID int64, input ledger.AdjustInput) {
	t.Helper()
	rows, err := e.admin.Query(e.ctx, `SELECT account_id,side,amount FROM ledger.posting WHERE txn_id=$1 ORDER BY side`, txnID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got [][3]any
	for rows.Next() {
		var accountID, amount int64
		var side string
		if err := rows.Scan(&accountID, &side, &amount); err != nil {
			t.Fatal(err)
		}
		got = append(got, [3]any{accountID, side, amount})
	}
	want := [][3]any{{input.CreditAccountID, "CREDIT", input.Amount}, {input.DebitAccountID, "DEBIT", input.Amount}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("postings=%v, want %v", got, want)
	}
}

func assertAdjustmentEvent(t *testing.T, e businessEnv, txnID int64, input ledger.AdjustInput, wantCount int) {
	t.Helper()
	var count int
	var payload []byte
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*),COALESCE(max(payload::text),'')::bytea FROM ledger.outbox WHERE txn_id=$1 AND topic='ledger.txn.adjusted'`, txnID).Scan(&count, &payload); err != nil {
		t.Fatal(err)
	}
	if count != wantCount {
		t.Fatalf("adjusted event count=%d, want %d", count, wantCount)
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	wantKeys := []string{"actor_id", "amount", "credit_account_id", "debit_account_id", "occurred_at", "reason", "reason_code", "reviewer_id"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("event fields=%v, want only %v", keys, wantKeys)
	}
	if fields["reason_code"] != input.ReasonCode || fields["reason"] != input.Reason {
		t.Fatalf("event payload=%s", payload)
	}
	for field, want := range map[string]int64{
		"actor_id": input.ActorID, "reviewer_id": input.ReviewerID, "amount": input.Amount,
		"debit_account_id": input.DebitAccountID, "credit_account_id": input.CreditAccountID,
	} {
		if got, ok := fields[field].(float64); !ok || int64(got) != want {
			t.Fatalf("event %s=%v, want %d; payload=%s", field, fields[field], want, payload)
		}
	}
}

func assertNoAdjustmentResidue(t *testing.T, e businessEnv, idemKey string) {
	t.Helper()
	var txns, postings, events int
	if err := e.admin.QueryRow(e.ctx, `
		SELECT count(*),
			(SELECT count(*) FROM ledger.posting p JOIN ledger.txn t ON t.id=p.txn_id WHERE t.idem_key=$1),
			(SELECT count(*) FROM ledger.outbox o JOIN ledger.txn t ON t.id=o.txn_id WHERE t.idem_key=$1)
		FROM ledger.txn WHERE idem_key=$1`, idemKey).Scan(&txns, &postings, &events); err != nil {
		t.Fatal(err)
	}
	if txns != 0 || postings != 0 || events != 0 {
		t.Fatalf("residue txn/posting/outbox=%d/%d/%d", txns, postings, events)
	}
}

func platformAccountTotal(t *testing.T, e businessEnv, accountIDs []int64) int64 {
	t.Helper()
	var total int64
	if err := e.admin.QueryRow(e.ctx, `SELECT COALESCE(sum(balance),0) FROM ledger.balance WHERE account_id=ANY($1)`, accountIDs).Scan(&total); err != nil {
		t.Fatal(err)
	}
	return total
}
