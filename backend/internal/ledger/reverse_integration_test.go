package ledger_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
)

func TestReverseIntegration(t *testing.T) {
	e := newBusinessEnv(t)
	buyer := mustWallet(t, e, 61001, nil)
	merchantID := int64(62001)
	merchant := mustWallet(t, e, 61002, &merchantID)
	for _, account := range []int64{buyer.Available, merchant.Available, merchant.AdBudget} {
		setBalance(t, e, account, 20_000_000)
	}
	for _, kind := range []string{"PLATFORM_HELD", "PAYABLE_CARRIER", "SUSPENSE"} {
		setBalance(t, e, singletonID(t, e.ctx, e.admin, kind), 20_000_000)
	}
	setBalance(t, e, singletonID(t, e.ctx, e.admin, "BANK_DEDICATED"), 120_000_000)
	assertAccountingInvariant(t, e)

	type reversibleCase struct {
		name string
		call func() (ledger.PostResult, error)
	}
	cases := []reversibleCase{
		{"topup", func() (ledger.PostResult, error) {
			return e.appService.Topup(e.ctx, ledger.TopupInput{BankStatementLineID: 6101, BankConfirmed: true, WalletAccountID: buyer.Available, Amount: 101, ActorID: 1})
		}},
		{"storage_fee", func() (ledger.PostResult, error) {
			return e.appService.StorageFee(e.ctx, ledger.ChargeInput{ID: 6102, WalletAccountID: merchant.Available, Amount: 102, ActorID: 1})
		}},
		{"member_fee", func() (ledger.PostResult, error) {
			return e.appService.MemberFee(e.ctx, ledger.ChargeInput{ID: 6103, WalletAccountID: buyer.Available, Amount: 103, ActorID: 1}, "2026M10")
		}},
		{"ad_charge", func() (ledger.PostResult, error) {
			return e.appService.AdCharge(e.ctx, ledger.AdTransferInput{AdID: 6104, WalletAvailable: merchant.Available, AdBudget: merchant.AdBudget, Amount: 104, ActorID: 1})
		}},
		{"ad_spend", func() (ledger.PostResult, error) {
			return e.appService.AdSpend(e.ctx, ledger.AdSpendInput{AdID: 6105, AdBudget: merchant.AdBudget, Amount: 105, ActorID: 1, Bucket: "20261014H10"})
		}},
		{"ad_refund", func() (ledger.PostResult, error) {
			return e.appService.AdRefund(e.ctx, ledger.AdTransferInput{AdID: 6106, WalletAvailable: merchant.Available, AdBudget: merchant.AdBudget, Amount: 106, ActorID: 1})
		}},
		{"bank_diff", func() (ledger.PostResult, error) {
			return e.appService.BankDiff(e.ctx, ledger.BankDiffInput{BankLineID: 6107, Amount: 107, ActorID: 1, Direction: "IN", Reason: "test difference"})
		}},
		{"sweep", func() (ledger.PostResult, error) {
			return e.appService.Sweep(e.ctx, ledger.ReviewedPaymentInput{ID: 6108, Amount: 108, ActorID: 1, ReviewerActorID: 2})
		}},
		{"carrier_pay", func() (ledger.PostResult, error) {
			return e.appService.CarrierPay(e.ctx, ledger.ReviewedPaymentInput{ID: 6109, Amount: 109, ActorID: 1, ReviewerActorID: 2})
		}},
		{"adjust", func() (ledger.PostResult, error) {
			platform := singletonID(t, e.ctx, e.admin, "PLATFORM_HELD")
			return ledger.PostForTest(e.ctx, e.appService, ledger.PostRequest{Type: "adjust", Amount: 110, IdemKey: "adjust:test:6110", ActorID: 1, Postings: []ledger.Posting{
				{AccountID: buyer.Available, Side: "DEBIT", Amount: 110},
				{AccountID: platform, Side: "CREDIT", Amount: 110},
			}})
		}},
	}

	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := allBalances(t, e)
			original, err := tc.call()
			if err != nil {
				t.Fatal(err)
			}
			reason := "correct " + tc.name
			reversal, err := e.appService.Reverse(e.ctx, ledger.ReverseInput{OriginalTxnID: original.TxnID, ActorID: int64(7000 + index), AuthorizedBy: 8000, Reason: reason})
			if err != nil {
				t.Fatal(err)
			}
			if reversal.Replayed {
				t.Fatal("first reversal reported as replay")
			}
			assertExactMirror(t, e, original.TxnID, reversal.TxnID)
			assertBalancesEqual(t, before, allBalances(t, e))
			assertAccountingInvariant(t, e)
			assertReversalEvent(t, e, original.TxnID, reversal.TxnID, int64(7000+index), 8000, reason, 1)

			got, err := e.appService.GetReversalOf(e.ctx, original.TxnID)
			if err != nil || got.TxnID != reversal.TxnID {
				t.Fatalf("GetReversalOf txn=%d err=%v", got.TxnID, err)
			}
			originalView, err := e.appService.GetByID(e.ctx, original.TxnID)
			if err != nil || originalView.ReversedByTxnID == nil || *originalView.ReversedByTxnID != reversal.TxnID {
				t.Fatalf("GetByID reversal=%v err=%v", originalView.ReversedByTxnID, err)
			}
			replay, err := e.appService.Reverse(e.ctx, ledger.ReverseInput{OriginalTxnID: original.TxnID, ActorID: int64(7000 + index), AuthorizedBy: 8000, Reason: reason})
			if err != nil || !replay.Replayed || replay.TxnID != reversal.TxnID {
				t.Fatalf("replay=%+v err=%v", replay, err)
			}
			assertReversalEvent(t, e, original.TxnID, reversal.TxnID, int64(7000+index), 8000, reason, 1)
		})
	}

	t.Run("validation and rejected transaction types", func(t *testing.T) {
		if _, err := e.appService.Reverse(e.ctx, ledger.ReverseInput{}); !errors.Is(err, ledger.ErrInvalidRequest) {
			t.Fatalf("invalid input error=%v", err)
		}
		if _, err := e.appService.Reverse(e.ctx, ledger.ReverseInput{OriginalTxnID: 99999999, ActorID: 1, AuthorizedBy: 2, Reason: "missing"}); !errors.Is(err, ledger.ErrTransactionNotFound) {
			t.Fatalf("missing transaction error=%v", err)
		}
		original, err := e.appService.Topup(e.ctx, ledger.TopupInput{BankStatementLineID: 6199, BankConfirmed: true, WalletAccountID: buyer.Available, Amount: 10, ActorID: 1})
		if err != nil {
			t.Fatal(err)
		}
		invalidInputs := []ledger.ReverseInput{
			{OriginalTxnID: original.TxnID, ActorID: 3, AuthorizedBy: 4},
			{OriginalTxnID: original.TxnID, AuthorizedBy: 4, Reason: "missing actor"},
			{OriginalTxnID: original.TxnID, ActorID: 3, Reason: "missing authorization"},
			{OriginalTxnID: original.TxnID, ActorID: 3, AuthorizedBy: 4, Reason: strings.Repeat("界", 501)},
		}
		for _, input := range invalidInputs {
			if _, err := e.appService.Reverse(e.ctx, input); !errors.Is(err, ledger.ErrInvalidRequest) {
				t.Fatalf("invalid input %+v error=%v", input, err)
			}
		}
		reversal, err := e.appService.Reverse(e.ctx, ledger.ReverseInput{OriginalTxnID: original.TxnID, ActorID: 3, AuthorizedBy: 4, Reason: "first"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.appService.Reverse(e.ctx, ledger.ReverseInput{OriginalTxnID: original.TxnID, ActorID: 3, AuthorizedBy: 4, Reason: "changed"}); !errors.Is(err, ledger.ErrAlreadyReversed) {
			t.Fatalf("changed duplicate error=%v", err)
		}
		if _, err := e.appService.Reverse(e.ctx, ledger.ReverseInput{OriginalTxnID: reversal.TxnID, ActorID: 3, AuthorizedBy: 4, Reason: "reverse reversal"}); !errors.Is(err, ledger.ErrNotReversible) {
			t.Fatalf("reverse reversal error=%v", err)
		}

		disallowed := []string{"freeze", "settle", "refund", "withdraw_req", "payout", "payout_fail", "delivery_hold", "delivery_release", "delivery_refund"}
		bank := singletonID(t, e.ctx, e.admin, "BANK_DEDICATED")
		for i, typ := range disallowed {
			result, err := ledger.PostForTest(e.ctx, e.appService, ledger.PostRequest{Type: typ, Amount: 1, IdemKey: fmt.Sprintf("blocked:test:%d", i), ActorID: 1, Postings: []ledger.Posting{
				{AccountID: bank, Side: "DEBIT", Amount: 1}, {AccountID: buyer.Available, Side: "CREDIT", Amount: 1},
			}})
			if err != nil {
				t.Fatalf("seed %s: %v", typ, err)
			}
			if _, err := e.appService.Reverse(e.ctx, ledger.ReverseInput{OriginalTxnID: result.TxnID, ActorID: 1, AuthorizedBy: 2, Reason: "blocked"}); !errors.Is(err, ledger.ErrReversalNotAllowed) {
				t.Fatalf("type %s error=%v", typ, err)
			}
		}
	})

	t.Run("insufficient funds leaves no reversal residue", func(t *testing.T) {
		empty := mustWallet(t, e, 63001, nil)
		setBalance(t, e, empty.Available, 0)
		original, err := e.appService.Topup(e.ctx, ledger.TopupInput{BankStatementLineID: 6301, BankConfirmed: true, WalletAccountID: empty.Available, Amount: 50, ActorID: 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.appService.StorageFee(e.ctx, ledger.ChargeInput{ID: 6302, WalletAccountID: empty.Available, Amount: 50, ActorID: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.appService.Reverse(e.ctx, ledger.ReverseInput{OriginalTxnID: original.TxnID, ActorID: 3, AuthorizedBy: 4, Reason: "cannot overdraw"}); !errors.Is(err, ledger.ErrInsufficientFunds) {
			t.Fatalf("error=%v", err)
		}
		assertTxnCounts(t, e.ctx, e.admin, fmt.Sprintf("reversal:txn:%d", original.TxnID), 0, 0, 0)
	})
}

func TestReverseConcurrencyIntegration(t *testing.T) {
	e := newBusinessEnv(t)
	wallet := mustWallet(t, e, 64001, nil)
	setBalance(t, e, wallet.Available, 1_000_000)
	setBalance(t, e, singletonID(t, e.ctx, e.admin, "BANK_DEDICATED"), 1_000_000)
	before := allBalances(t, e)
	original, err := e.appService.Topup(e.ctx, ledger.TopupInput{BankStatementLineID: 6401, BankConfirmed: true, WalletAccountID: wallet.Available, Amount: 1000, ActorID: 1})
	if err != nil {
		t.Fatal(err)
	}

	results := make(chan ledger.PostResult, 100)
	errs := make(chan error, 100)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := e.appService.Reverse(e.ctx, ledger.ReverseInput{OriginalTxnID: original.TxnID, ActorID: 2, AuthorizedBy: 3, Reason: "concurrent correction"})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	var firstID int64
	var fresh, replayed int
	for result := range results {
		if firstID == 0 {
			firstID = result.TxnID
		} else if result.TxnID != firstID {
			t.Fatalf("transaction IDs differ: %d and %d", firstID, result.TxnID)
		}
		if result.Replayed {
			replayed++
		} else {
			fresh++
		}
	}
	if fresh != 1 || replayed != 99 {
		t.Fatalf("fresh=%d replayed=%d", fresh, replayed)
	}
	assertBalancesEqual(t, before, allBalances(t, e))
	assertTxnCounts(t, e.ctx, e.admin, fmt.Sprintf("reversal:txn:%d", original.TxnID), 1, 2, 2)
	assertReversalEvent(t, e, original.TxnID, firstID, 2, 3, "concurrent correction", 1)
}

func TestReverseRandomSequenceIntegration(t *testing.T) {
	e := newBusinessEnv(t)
	wallet := mustWallet(t, e, 65001, nil)
	bank := singletonID(t, e.ctx, e.admin, "BANK_DEDICATED")
	platform := singletonID(t, e.ctx, e.admin, "PLATFORM_HELD")
	setBalance(t, e, wallet.Available, 1_000_000)
	setBalance(t, e, bank, 2_000_000)
	setBalance(t, e, platform, 1_000_000)

	rng := rand.New(rand.NewSource(606))
	reversible := make([]int64, 0, 200)
	for step := range 200 {
		amount := int64(rng.Intn(50) + 1)
		var result ledger.PostResult
		var err error
		if len(reversible) > 0 && rng.Intn(3) == 0 {
			index := rng.Intn(len(reversible))
			result, err = e.appService.Reverse(e.ctx, ledger.ReverseInput{OriginalTxnID: reversible[index], ActorID: 10, AuthorizedBy: 11, Reason: fmt.Sprintf("random step %d", step)})
			reversible = append(reversible[:index], reversible[index+1:]...)
		} else if rng.Intn(2) == 0 {
			result, err = e.appService.Topup(e.ctx, ledger.TopupInput{BankStatementLineID: int64(65000 + step), BankConfirmed: true, WalletAccountID: wallet.Available, Amount: amount, ActorID: 10})
			if err == nil {
				reversible = append(reversible, result.TxnID)
			}
		} else {
			result, err = ledger.PostForTest(e.ctx, e.appService, ledger.PostRequest{Type: "adjust", Amount: amount, IdemKey: fmt.Sprintf("adjust:random:%d", step), ActorID: 10, Postings: []ledger.Posting{
				{AccountID: wallet.Available, Side: "DEBIT", Amount: amount}, {AccountID: platform, Side: "CREDIT", Amount: amount},
			}})
			if err == nil {
				reversible = append(reversible, result.TxnID)
			}
		}
		if err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
		assertAccountingInvariant(t, e)
	}
}

type postingMirror struct {
	accountID int64
	side      string
	amount    int64
	role      string
	category  string
}

func allBalances(t *testing.T, e businessEnv) map[int64]int64 {
	t.Helper()
	rows, err := e.admin.Query(e.ctx, `SELECT account_id,balance FROM ledger.balance ORDER BY account_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := map[int64]int64{}
	for rows.Next() {
		var id, balance int64
		if err := rows.Scan(&id, &balance); err != nil {
			t.Fatal(err)
		}
		result[id] = balance
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertBalancesEqual(t *testing.T, want, got map[int64]int64) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("balances=%v want=%v", got, want)
	}
}

func assertExactMirror(t *testing.T, e businessEnv, originalID, reversalID int64) {
	t.Helper()
	read := func(txnID int64, invert bool) []postingMirror {
		rows, err := e.admin.Query(e.ctx, `SELECT account_id,side,amount,COALESCE(role,''),COALESCE(category,'') FROM ledger.posting WHERE txn_id=$1`, txnID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var result []postingMirror
		for rows.Next() {
			var p postingMirror
			if err := rows.Scan(&p.accountID, &p.side, &p.amount, &p.role, &p.category); err != nil {
				t.Fatal(err)
			}
			if invert {
				if p.side == "DEBIT" {
					p.side = "CREDIT"
				} else {
					p.side = "DEBIT"
				}
			}
			result = append(result, p)
		}
		sort.Slice(result, func(i, j int) bool { return fmt.Sprint(result[i]) < fmt.Sprint(result[j]) })
		return result
	}
	if want, got := read(originalID, true), read(reversalID, false); fmt.Sprint(want) != fmt.Sprint(got) {
		t.Fatalf("reversal postings=%v want=%v", got, want)
	}
	var originalAmount, reversalAmount int64
	if err := e.admin.QueryRow(e.ctx, `SELECT (SELECT amount FROM ledger.txn WHERE id=$1),(SELECT amount FROM ledger.txn WHERE id=$2)`, originalID, reversalID).Scan(&originalAmount, &reversalAmount); err != nil {
		t.Fatal(err)
	}
	if originalAmount != reversalAmount {
		t.Fatalf("reversal amount=%d want=%d", reversalAmount, originalAmount)
	}
}

func assertReversalEvent(t *testing.T, e businessEnv, originalID, reversalID, actorID, authorizedBy int64, reason string, wantCount int) {
	t.Helper()
	rows, err := e.admin.Query(e.ctx, `SELECT payload FROM ledger.outbox WHERE txn_id=$1 AND topic='ledger.txn.reversed'`, reversalID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var payload struct {
			OriginalTxnID int64  `json:"original_txn_id"`
			ReversalTxnID int64  `json:"reversal_txn_id"`
			ActorID       int64  `json:"actor_id"`
			AuthorizedBy  int64  `json:"authorized_by"`
			Reason        string `json:"reason"`
			OccurredAt    string `json:"occurred_at"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.OriginalTxnID != originalID || payload.ReversalTxnID != reversalID || payload.ActorID != actorID || payload.AuthorizedBy != authorizedBy || payload.Reason != reason || payload.OccurredAt == "" {
			t.Fatalf("event=%+v", payload)
		}
		if strings.Contains(string(raw), "payee") {
			t.Fatalf("event contains unexpected sensitive field: %s", raw)
		}
	}
	if count != wantCount {
		t.Fatalf("reversal event count=%d want=%d", count, wantCount)
	}
}
