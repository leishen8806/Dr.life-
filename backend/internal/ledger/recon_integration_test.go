package ledger_test

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
)

type reconMetrics struct {
	mu         sync.Mutex
	runs       []bool
	mismatches map[string]int
}

func reconciliationService(e businessEnv, options ledger.Options) *ledger.Service {
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().AddDate(0, 0, 2) }
	}
	return ledger.New(e.app, options)
}

func (m *reconMetrics) ReconRun(ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs = append(m.runs, ok)
}
func (m *reconMetrics) ReconMismatch(code string, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mismatches == nil {
		m.mismatches = make(map[string]int)
	}
	m.mismatches[code] += count
}
func (m *reconMetrics) ReconSpotCheckDropped() {}

func TestReconciliationNormalAndForcedRuns(t *testing.T) {
	e := newBusinessEnv(t)
	metrics := &reconMetrics{}
	service := reconciliationService(e, ledger.Options{ReconciliationMetrics: metrics})
	wallet := mustWallet(t, e, 9101, nil)
	if _, err := service.Topup(e.ctx, ledger.TopupInput{ManualConfirmation: "recon-normal", BankConfirmed: true, WalletAccountID: wallet.Available, Amount: 1_000, ActorID: 1}); err != nil {
		t.Fatal(err)
	}
	day := time.Now()
	first, err := service.RunDailyReconciliation(e.ctx, day, ledger.ReconciliationOptions{InitiatedBy: 11})
	if err != nil || first.Status != "ok" || len(first.Issues) != 0 {
		t.Fatalf("first run=%+v err=%v", first, err)
	}
	assertSQLState(t, execErr(e.ctx, e.admin, `UPDATE ledger.recon_run SET summary='{"changed":1}' WHERE id=$1`, first.ID), "55000")
	assertSQLState(t, execErr(e.ctx, e.admin, `DELETE FROM ledger.recon_run WHERE id=$1`, first.ID), "55000")
	replayed, err := service.RunDailyReconciliation(e.ctx, day, ledger.ReconciliationOptions{})
	if err != nil || replayed.ID != first.ID {
		t.Fatalf("default repeat run=%+v err=%v", replayed, err)
	}
	if _, err := service.RunDailyReconciliation(e.ctx, day, ledger.ReconciliationOptions{Force: true}); !errors.Is(err, ledger.ErrReconciliationForceReason) {
		t.Fatalf("force without reason err=%v", err)
	}
	forced, err := service.RunDailyReconciliation(e.ctx, day, ledger.ReconciliationOptions{Force: true, Reason: "verified repair", InitiatedBy: 12})
	if err != nil || forced.Status != "ok" || forced.ID == first.ID || forced.Attempt != 2 {
		t.Fatalf("forced run=%+v err=%v", forced, err)
	}
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	if len(metrics.runs) != 2 || !metrics.runs[0] || !metrics.runs[1] {
		t.Fatalf("run metrics=%v", metrics.runs)
	}
}

func TestReconciliationRejectsOpenBusinessDayWithoutSideEffects(t *testing.T) {
	e := newBusinessEnv(t)
	location := time.FixedZone("recon-test", 7*60*60)
	now := time.Date(2026, time.October, 26, 8, 0, 0, 0, location)
	service := ledger.New(e.app, ledger.Options{ReconciliationLocation: location, Now: func() time.Time { return now }})
	today := time.Date(2026, time.October, 26, 0, 0, 0, 0, location)

	for _, day := range []time.Time{today, today.AddDate(0, 0, 1)} {
		if _, err := service.SnapshotDaily(e.ctx, day); !errors.Is(err, ledger.ErrReconciliationDayOpen) {
			t.Fatalf("snapshot %s err=%v", day, err)
		}
		if _, err := service.RunDailyReconciliation(e.ctx, day, ledger.ReconciliationOptions{}); !errors.Is(err, ledger.ErrReconciliationDayOpen) {
			t.Fatalf("reconciliation %s err=%v", day, err)
		}
	}
	var snapshots, runs, freezes int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM ledger.snapshot_daily`).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM ledger.recon_run`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM ledger.payout_freeze`).Scan(&freezes); err != nil {
		t.Fatal(err)
	}
	if snapshots != 0 || runs != 0 || freezes != 0 {
		t.Fatalf("open day created side effects: snapshots=%d runs=%d freezes=%d", snapshots, runs, freezes)
	}
}

func TestReconciliationAccountFreeze(t *testing.T) {
	e := newBusinessEnv(t)
	metrics := &reconMetrics{}
	service := reconciliationService(e, ledger.Options{
		AccountGuard: withdrawalGuard{}, PayeeCipher: withdrawalCipher(t),
		ReconciliationMetrics: metrics,
	})
	walletA := mustWallet(t, e, 9201, nil)
	walletB := mustWallet(t, e, 9202, nil)
	walletC := mustWallet(t, e, 9203, nil)
	walletClean := mustWallet(t, e, 9204, nil)
	for i, wallet := range []ledger.WalletAccounts{walletA, walletB, walletC, walletClean} {
		if _, err := service.Topup(e.ctx, ledger.TopupInput{ManualConfirmation: fmt.Sprintf("recon-account-%d", i), BankConfirmed: true, WalletAccountID: wallet.Available, Amount: 1_000, ActorID: 1}); err != nil {
			t.Fatal(err)
		}
	}
	approved := requestWithdrawal(t, e, service, walletA.Available, 9201, 10, "RECON-A-1111", 21)
	if _, err := e.admin.Exec(e.ctx, `UPDATE ledger.balance SET balance=balance+1,version=version+7 WHERE account_id=$1`, walletA.Available); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `UPDATE ledger.balance SET balance=balance-1 WHERE account_id=$1`, walletB.Available); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `ALTER TABLE ledger.balance DISABLE TRIGGER trg_balance_policy`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `UPDATE ledger.balance SET allow_negative=true,balance=-1 WHERE account_id=$1`, walletC.Available); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `ALTER TABLE ledger.balance ENABLE TRIGGER trg_balance_policy`); err != nil {
		t.Fatal(err)
	}
	run, err := service.RunDailyReconciliation(e.ctx, time.Now(), ledger.ReconciliationOptions{InitiatedBy: 31})
	if err != nil || run.Status != "failed" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	issue := findIssue(t, run.Issues, ledger.CheckBalanceCache, walletA.Available)
	if issue.Expected == nil || issue.Actual == nil || *issue.Actual != *issue.Expected+1 {
		t.Fatalf("balance issue=%+v", issue)
	}
	if hasIssue(run.Issues, ledger.CheckIdentity) {
		t.Fatalf("wallet cache drift should remain account scoped: %+v", run.Issues)
	}
	findIssue(t, run.Issues, ledger.CheckBalanceCache, walletB.Available)
	findIssue(t, run.Issues, ledger.CheckBalanceCache, walletC.Available)
	assertSQLState(t, execErr(e.ctx, e.admin, `UPDATE ledger.recon_issue SET detail='changed' WHERE run_id=$1`, run.ID), "55000")
	assertSQLState(t, execErr(e.ctx, e.app, `DELETE FROM ledger.recon_issue WHERE run_id=$1`, run.ID), "42501")
	if _, err := service.RequestWithdrawal(e.ctx, ledger.RequestWithdrawalInput{RequestKey: "blocked-account", RequesterAccountID: 9201, WalletAvailable: walletA.Available, Amount: 10, PayeeAccount: "A-2222", ActorID: 22, SecondFactorVerified: true}); !errors.Is(err, ledger.ErrPayoutFrozen) {
		t.Fatalf("request on frozen account err=%v", err)
	}
	if _, err := service.ConfirmPayout(e.ctx, ledger.ConfirmPayoutInput{WithdrawalID: approved.Withdrawal.ID, BankRef: "RECON-REF-A", ActorID: 23}); !errors.Is(err, ledger.ErrPayoutFrozen) {
		t.Fatalf("confirm on frozen account err=%v", err)
	}
	if _, err := service.RequestWithdrawal(e.ctx, ledger.RequestWithdrawalInput{RequestKey: "other-account", RequesterAccountID: 9204, WalletAvailable: walletClean.Available, Amount: 10, PayeeAccount: "B-3333", ActorID: 24, SecondFactorVerified: true}); err != nil {
		t.Fatalf("other account blocked: %v", err)
	}
	freezes, err := service.ListPayoutFreezes(e.ctx, true)
	if err != nil || len(freezes) != 3 {
		t.Fatalf("freezes=%+v err=%v", freezes, err)
	}
}

func TestReconciliationGlobalFreezeAndRelease(t *testing.T) {
	e := newBusinessEnv(t)
	service := reconciliationService(e, ledger.Options{AccountGuard: withdrawalGuard{}, PayeeCipher: withdrawalCipher(t)})
	wallet := mustWallet(t, e, 9301, nil)
	if _, err := service.Topup(e.ctx, ledger.TopupInput{ManualConfirmation: "recon-global", BankConfirmed: true, WalletAccountID: wallet.Available, Amount: 2_000, ActorID: 1}); err != nil {
		t.Fatal(err)
	}
	toCancel := requestWithdrawal(t, e, service, wallet.Available, 9301, 10, "RECON-CANCEL-1", 31)
	toFail := requestWithdrawal(t, e, service, wallet.Available, 9301, 10, "RECON-FAIL-2", 32)
	var bankID int64
	if err := e.admin.QueryRow(e.ctx, `SELECT id FROM ledger.account WHERE kind='BANK_DEDICATED'`).Scan(&bankID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `UPDATE ledger.balance SET balance=balance+1 WHERE account_id=$1`, bankID); err != nil {
		t.Fatal(err)
	}
	run, err := service.RunDailyReconciliation(e.ctx, time.Now(), ledger.ReconciliationOptions{InitiatedBy: 41})
	if err != nil || !hasIssue(run.Issues, ledger.CheckIdentity) {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	for name, call := range map[string]func() error{
		"request": func() error {
			_, err := service.RequestWithdrawal(e.ctx, ledger.RequestWithdrawalInput{RequestKey: "global-block", RequesterAccountID: 9301, WalletAvailable: wallet.Available, Amount: 10, PayeeAccount: "G-3333", ActorID: 33, SecondFactorVerified: true})
			return err
		},
		"confirm": func() error {
			_, err := service.ConfirmPayout(e.ctx, ledger.ConfirmPayoutInput{WithdrawalID: toCancel.Withdrawal.ID, BankRef: "GLOBAL-REF", ActorID: 34})
			return err
		},
		"sweep": func() error {
			_, err := service.Sweep(e.ctx, ledger.ReviewedPaymentInput{ID: 1, Amount: 1, ActorID: 50, ReviewerActorID: 51})
			return err
		},
		"carrier": func() error {
			_, err := service.CarrierPay(e.ctx, ledger.ReviewedPaymentInput{ID: 1, Amount: 1, ActorID: 50, ReviewerActorID: 51})
			return err
		},
	} {
		if err := call(); !errors.Is(err, ledger.ErrPayoutFrozen) {
			t.Fatalf("%s err=%v", name, err)
		}
	}
	if _, err := service.CancelWithdrawal(e.ctx, ledger.CancelWithdrawalInput{WithdrawalID: toCancel.Withdrawal.ID, Reason: "return funds", ActorID: 35}); err != nil {
		t.Fatalf("cancel under freeze: %v", err)
	}
	if _, err := service.FailWithdrawal(e.ctx, ledger.FailWithdrawalInput{WithdrawalID: toFail.Withdrawal.ID, Reason: "bank rejected", ActorID: 36}); err != nil {
		t.Fatalf("fail under freeze: %v", err)
	}
	freezes, err := service.ListPayoutFreezes(e.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	var global ledger.PayoutFreeze
	for _, freeze := range freezes {
		if freeze.Scope == "global" {
			global = freeze
		}
	}
	if global.ID == 0 {
		t.Fatalf("global freeze missing: %+v", freezes)
	}
	if _, err := service.ReleasePayoutFreeze(e.ctx, global.ID, 41, "same actor"); !errors.Is(err, ledger.ErrFreezeReleaseSeparation) {
		t.Fatalf("same actor release err=%v", err)
	}
	if _, err := service.ReleasePayoutFreeze(e.ctx, global.ID, 42, "finance verified"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RequestWithdrawal(e.ctx, ledger.RequestWithdrawalInput{RequestKey: "after-release", RequesterAccountID: 9301, WalletAvailable: wallet.Available, Amount: 10, PayeeAccount: "G-4444", ActorID: 37, SecondFactorVerified: true}); err != nil {
		t.Fatalf("request after global release: %v", err)
	}
	var failedEvents, releasedEvents int
	var failedPayload string
	if err := e.admin.QueryRow(e.ctx, `SELECT payload::text FROM ledger.outbox WHERE topic='ledger.recon.failed'`).Scan(&failedPayload); err != nil {
		t.Fatal(err)
	}
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FILTER (WHERE topic='ledger.recon.failed'),count(*) FILTER (WHERE topic='ledger.payout_freeze.released') FROM ledger.outbox`).Scan(&failedEvents, &releasedEvents); err != nil {
		t.Fatal(err)
	}
	if failedEvents != 1 || releasedEvents != 1 {
		t.Fatalf("failed events=%d released events=%d", failedEvents, releasedEvents)
	}
	for _, secret := range []string{"RECON-CANCEL-1", "RECON-FAIL-2", "G-3333"} {
		if strings.Contains(failedPayload, secret) {
			t.Fatalf("failed event leaked %q: %s", secret, failedPayload)
		}
	}
}

func TestSnapshotDailyImmutableAndDetectsDrift(t *testing.T) {
	e := newBusinessEnv(t)
	service := reconciliationService(e, ledger.Options{})
	wallet := mustWallet(t, e, 9401, nil)
	if _, err := service.Topup(e.ctx, ledger.TopupInput{ManualConfirmation: "snapshot-drift", BankConfirmed: true, WalletAccountID: wallet.Available, Amount: 100, ActorID: 1}); err != nil {
		t.Fatal(err)
	}
	day := time.Now()
	first, err := service.SnapshotDaily(e.ctx, day)
	if err != nil || len(first.Drifts) != 0 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var before int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM ledger.snapshot_daily WHERE day=$1`, first.Day).Scan(&before); err != nil {
		t.Fatal(err)
	}
	second, err := service.SnapshotDaily(e.ctx, day)
	if err != nil || len(second.Drifts) != 0 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	var after int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM ledger.snapshot_daily WHERE day=$1`, first.Day).Scan(&after); err != nil || after != before {
		t.Fatalf("snapshot rows before=%d after=%d err=%v", before, after, err)
	}
	assertSQLState(t, execErr(e.ctx, e.admin, `UPDATE ledger.snapshot_daily SET balance=balance+1 WHERE day=$1 AND account_id=$2`, first.Day, wallet.Available), "55000")
	assertSQLState(t, execErr(e.ctx, e.admin, `DELETE FROM ledger.snapshot_daily WHERE day=$1`, first.Day), "55000")
	assertSQLState(t, execErr(e.ctx, e.admin, `TRUNCATE ledger.snapshot_daily`), "55000")
	assertSQLState(t, execErr(e.ctx, e.app, `UPDATE ledger.snapshot_daily SET balance=balance+1 WHERE day=$1 AND account_id=$2`, first.Day, wallet.Available), "42501")

	if _, err := e.admin.Exec(e.ctx, `ALTER TABLE ledger.posting DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `UPDATE ledger.posting SET amount=amount+1 WHERE account_id=$1`, wallet.Available); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `ALTER TABLE ledger.posting ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	drift, err := service.SnapshotDaily(e.ctx, day)
	if err != nil || !hasIssue(drift.Drifts, ledger.CheckSnapshotDrift) {
		t.Fatalf("drift=%+v err=%v", drift, err)
	}
}

func TestReconciliationDetectsTriggerBypass(t *testing.T) {
	e := newBusinessEnv(t)
	var bankID, platformID, suspenseID int64
	for kind, target := range map[string]*int64{"BANK_DEDICATED": &bankID, "PLATFORM_HELD": &platformID, "SUSPENSE": &suspenseID} {
		if err := e.admin.QueryRow(e.ctx, `SELECT id FROM ledger.account WHERE kind=$1`, kind).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.admin.Exec(e.ctx, `ALTER TABLE ledger.txn DISABLE TRIGGER USER; ALTER TABLE ledger.posting DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	var unbalancedID, originalID, reversalID int64
	if err := e.admin.QueryRow(e.ctx, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,actor_id) VALUES('DIRTY-UNBAL','adjust',10,'adjust:dirty:unbalanced',1) RETURNING id`).Scan(&unbalancedID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO ledger.posting(txn_id,account_id,side,amount) VALUES($1,$2,'DEBIT',10),($1,$3,'CREDIT',9)`, unbalancedID, bankID, platformID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,actor_id) VALUES('DIRTY-ORPHAN','adjust',5,'adjust:dirty:orphan',1)`); err != nil {
		t.Fatal(err)
	}
	if err := e.admin.QueryRow(e.ctx, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,actor_id) VALUES('DIRTY-ORIGINAL','adjust',7,'adjust:dirty:original',1) RETURNING id`).Scan(&originalID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO ledger.posting(txn_id,account_id,side,amount) VALUES($1,$2,'DEBIT',7),($1,$3,'CREDIT',7)`, originalID, bankID, platformID); err != nil {
		t.Fatal(err)
	}
	if err := e.admin.QueryRow(e.ctx, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,reverses_txn_id,actor_id) VALUES('DIRTY-REVERSAL','reversal',7,'reversal:dirty:original',$1,1) RETURNING id`, originalID).Scan(&reversalID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO ledger.posting(txn_id,account_id,side,amount) VALUES($1,$2,'DEBIT',7),($1,$3,'CREDIT',7)`, reversalID, bankID, suspenseID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `ALTER TABLE ledger.posting ENABLE TRIGGER USER; ALTER TABLE ledger.txn ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	run, err := reconciliationService(e, ledger.Options{}).RunDailyReconciliation(e.ctx, time.Now(), ledger.ReconciliationOptions{InitiatedBy: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{ledger.CheckTxnBalanced, ledger.CheckOrphan, ledger.CheckReversal} {
		if !hasIssue(run.Issues, code) {
			t.Fatalf("missing %s: %+v", code, run.Issues)
		}
	}
}

func TestReconciliationDetectsHoldAndPendingPayoutMismatch(t *testing.T) {
	e := newBusinessEnv(t)
	service := reconciliationService(e, ledger.Options{AccountGuard: withdrawalGuard{}, PayeeCipher: withdrawalCipher(t)})
	wallet := mustWallet(t, e, 9451, nil)
	if _, err := service.Topup(e.ctx, ledger.TopupInput{ManualConfirmation: "recon-linked", BankConfirmed: true, WalletAccountID: wallet.Available, Amount: 1_000, ActorID: 1}); err != nil {
		t.Fatal(err)
	}
	hold, err := service.FreezeHold(e.ctx, ledger.FreezeHoldInput{RefType: "order", RefID: 9451, WalletAvailable: wallet.Available, WalletFrozen: wallet.Frozen, Amount: 100, ActorID: 2})
	if err != nil {
		t.Fatal(err)
	}
	requestWithdrawal(t, e, service, wallet.Available, 9451, 10, "LINKED-1111", 3)
	if _, err := e.admin.Exec(e.ctx, `ALTER TABLE ledger.hold DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `UPDATE ledger.hold SET amount=amount+1 WHERE id=$1`, hold.Hold.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `ALTER TABLE ledger.hold ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `UPDATE ledger.balance SET balance=balance+1 WHERE account_id=(SELECT id FROM ledger.account WHERE kind='PENDING_PAYOUT')`); err != nil {
		t.Fatal(err)
	}
	run, err := service.RunDailyReconciliation(e.ctx, time.Now(), ledger.ReconciliationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{ledger.CheckHoldFrozen, ledger.CheckPendingPayout} {
		if !hasIssue(run.Issues, code) {
			t.Fatalf("missing %s: %+v", code, run.Issues)
		}
	}
	freezes, err := service.ListPayoutFreezes(e.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	var global, frozenWallet bool
	for _, freeze := range freezes {
		global = global || freeze.Scope == "global"
		frozenWallet = frozenWallet || (freeze.Scope == "account" && freeze.AccountID != nil && *freeze.AccountID == wallet.Available)
	}
	if !global || !frozenWallet {
		t.Fatalf("hold/payout mismatch freezes global=%t wallet=%t freezes=%+v", global, frozenWallet, freezes)
	}
}

func TestReconciliationHoldMismatchWithoutAvailableWalletFreezesGlobally(t *testing.T) {
	e := newBusinessEnv(t)
	service := reconciliationService(e, ledger.Options{})
	var frozenID int64
	if err := e.admin.QueryRow(e.ctx, `INSERT INTO ledger.account(kind,normal_side,owner_account) VALUES('WALLET_FROZEN','CREDIT',99001) RETURNING id`).Scan(&frozenID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `UPDATE ledger.balance SET balance=50 WHERE account_id=$1`, frozenID); err != nil {
		t.Fatal(err)
	}
	run, err := service.RunDailyReconciliation(e.ctx, time.Now(), ledger.ReconciliationOptions{})
	if err != nil || !hasIssue(run.Issues, ledger.CheckHoldFrozen) {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	freezes, err := service.ListPayoutFreezes(e.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, freeze := range freezes {
		if freeze.Scope == "global" {
			return
		}
	}
	t.Fatalf("missing global freeze: %+v", freezes)
}

func TestSpotCheckRetriesBehindFreshRunningAttempt(t *testing.T) {
	e := newBusinessEnv(t)
	wallet := mustWallet(t, e, 99002, nil)
	location := time.FixedZone("spot-test", 7*60*60)
	now := time.Now().In(location)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO ledger.recon_run(day,kind,attempt,started_at) VALUES($1,'spot',1,$2)`, day, now); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `UPDATE ledger.balance SET balance=balance+1 WHERE account_id=$1`, wallet.Available); err != nil {
		t.Fatal(err)
	}
	service := ledger.New(e.app, ledger.Options{SpotCheckEvery: 1, ReconciliationLocation: location, Now: func() time.Time { return now }})
	if _, err := service.Topup(e.ctx, ledger.TopupInput{ManualConfirmation: "spot-retry", BankConfirmed: true, WalletAccountID: wallet.Available, Amount: 1, ActorID: 1}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		freezes, err := service.ListPayoutFreezes(e.ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, freeze := range freezes {
			if freeze.Scope == "account" && freeze.AccountID != nil && *freeze.AccountID == wallet.Available {
				if err := service.StopSpotChecks(e.ctx); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = service.StopSpotChecks(e.ctx)
	t.Fatal("missing account freeze")
}

func TestReconciliationReplacesStaleRunningAttempt(t *testing.T) {
	e := newBusinessEnv(t)
	now := time.Date(2026, time.October, 26, 12, 0, 0, 0, time.UTC)
	service := ledger.New(e.app, ledger.Options{Now: func() time.Time { return now }, ReconciliationRunTimeout: time.Minute})
	day := now.AddDate(0, 0, -1)
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO ledger.recon_run(day,kind,attempt,started_at) VALUES($1,'daily',1,$2)`, day, now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	run, err := service.RunDailyReconciliation(e.ctx, day, ledger.ReconciliationOptions{})
	if err != nil || run.Attempt != 2 || run.Status != "ok" {
		t.Fatalf("replacement run=%+v err=%v", run, err)
	}
	var staleStatus string
	var staleSummary string
	if err := e.admin.QueryRow(e.ctx, `SELECT status,summary::text FROM ledger.recon_run WHERE day=$1 AND kind='daily' AND attempt=1`, day).Scan(&staleStatus, &staleSummary); err != nil {
		t.Fatal(err)
	}
	if staleStatus != "failed" || !strings.Contains(staleSummary, "STALE_RUN") {
		t.Fatalf("stale run status=%s summary=%s", staleStatus, staleSummary)
	}
}

func TestReconciliationBusinessSequenceHasNoFalsePositive(t *testing.T) {
	e := newBusinessEnv(t)
	service := reconciliationService(e, ledger.Options{AccountGuard: withdrawalGuard{}, PayeeCipher: withdrawalCipher(t)})
	buyer := mustWallet(t, e, 9471, nil)
	merchant := mustWallet(t, e, 9472, nil)
	topup, err := service.Topup(e.ctx, ledger.TopupInput{ManualConfirmation: "recon-sequence-buyer", BankConfirmed: true, WalletAccountID: buyer.Available, Amount: 2_000, ActorID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Topup(e.ctx, ledger.TopupInput{ManualConfirmation: "recon-sequence-merchant", BankConfirmed: true, WalletAccountID: merchant.Available, Amount: 1_000, ActorID: 1}); err != nil {
		t.Fatal(err)
	}
	hold, err := service.FreezeHold(e.ctx, ledger.FreezeHoldInput{RefType: "order", RefID: 9471, WalletAvailable: buyer.Available, WalletFrozen: buyer.Frozen, Amount: 100, ActorID: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RefundHold(e.ctx, ledger.RefundHoldInput{HoldID: hold.Hold.ID, ActorID: 3, PayerActorID: 2}); err != nil {
		t.Fatal(err)
	}
	withdrawal := requestWithdrawal(t, e, service, merchant.Available, 9472, 50, "SEQUENCE-2222", 4)
	if _, err := service.CancelWithdrawal(e.ctx, ledger.CancelWithdrawalInput{WithdrawalID: withdrawal.Withdrawal.ID, Reason: "sequence", ActorID: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Reverse(e.ctx, ledger.ReverseInput{OriginalTxnID: topup.TxnID, ActorID: 6, AuthorizedBy: 7, Reason: "sequence reversal"}); err != nil {
		t.Fatal(err)
	}
	run, err := service.RunDailyReconciliation(e.ctx, time.Now(), ledger.ReconciliationOptions{})
	if err != nil || run.Status != "ok" || len(run.Issues) != 0 {
		t.Fatalf("business sequence run=%+v err=%v", run, err)
	}
}

func TestReconciliationConcurrentPostingHasNoFalsePositive(t *testing.T) {
	e := newBusinessEnv(t)
	service := reconciliationService(e, ledger.Options{})
	wallet := mustWallet(t, e, 9501, nil)
	start := make(chan struct{})
	errs := make(chan error, 100)
	var group sync.WaitGroup
	for i := range 100 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := service.Topup(e.ctx, ledger.TopupInput{ManualConfirmation: fmt.Sprintf("recon-concurrent-%d", i), BankConfirmed: true, WalletAccountID: wallet.Available, Amount: 1, ActorID: int64(i + 1)})
			errs <- err
		}()
	}
	close(start)
	run, runErr := service.RunDailyReconciliation(e.ctx, time.Now(), ledger.ReconciliationOptions{})
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if runErr != nil || run.Status != "ok" || len(run.Issues) != 0 {
		t.Fatalf("concurrent run=%+v err=%v", run, runErr)
	}
	nextDay, err := service.RunDailyReconciliation(e.ctx, time.Now().AddDate(0, 0, 1), ledger.ReconciliationOptions{})
	if err != nil || nextDay.Status != "ok" || len(nextDay.Issues) != 0 {
		t.Fatalf("post-concurrency run=%+v err=%v", nextDay, err)
	}
}

func TestSpotCheckIsAsynchronousAndFreezesMismatch(t *testing.T) {
	e := newBusinessEnv(t)
	wallet := mustWallet(t, e, 9601, nil)
	base := reconciliationService(e, ledger.Options{})
	if _, err := base.Topup(e.ctx, ledger.TopupInput{ManualConfirmation: "spot-base", BankConfirmed: true, WalletAccountID: wallet.Available, Amount: 100, ActorID: 1}); err != nil {
		t.Fatal(err)
	}
	clean := reconciliationService(e, ledger.Options{SpotCheckEvery: 1})
	if _, err := clean.Topup(e.ctx, ledger.TopupInput{ManualConfirmation: "spot-clean", BankConfirmed: true, WalletAccountID: wallet.Available, Amount: 1, ActorID: 2}); err != nil {
		t.Fatal(err)
	}
	if err := clean.StopSpotChecks(e.ctx); err != nil {
		t.Fatal(err)
	}
	var cleanRuns int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM ledger.recon_run WHERE kind='spot'`).Scan(&cleanRuns); err != nil {
		t.Fatal(err)
	}
	if cleanRuns != 0 {
		t.Fatalf("clean spot check created %d reconciliation runs", cleanRuns)
	}
	if _, err := e.admin.Exec(e.ctx, `UPDATE ledger.balance SET balance=balance+1 WHERE account_id=$1`, wallet.Available); err != nil {
		t.Fatal(err)
	}
	metrics := &reconMetrics{}
	service := reconciliationService(e, ledger.Options{SpotCheckEvery: 1, ReconciliationMetrics: metrics})
	if _, err := service.Topup(e.ctx, ledger.TopupInput{ManualConfirmation: "spot-trigger", BankConfirmed: true, WalletAccountID: wallet.Available, Amount: 1, ActorID: 2}); err != nil {
		t.Fatalf("posting must not wait for or inherit spot-check failure: %v", err)
	}
	deadline := time.NewTimer(10 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("spot check did not create account freeze")
		case <-ticker.C:
			freezes, err := service.ListPayoutFreezes(e.ctx, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, freeze := range freezes {
				if freeze.AccountID != nil && *freeze.AccountID == wallet.Available {
					metrics.mu.Lock()
					got := metrics.mismatches[ledger.CheckBalanceCache]
					metrics.mu.Unlock()
					if got != 1 {
						t.Fatalf("spot mismatch metric=%d", got)
					}
					return
				}
			}
		}
	}
}

func TestReconciliationScale(t *testing.T) {
	if os.Getenv("RUN_LEDGER_SCALE") != "1" {
		t.Skip("set RUN_LEDGER_SCALE=1 for the 10k-account/1m-posting measurement")
	}
	e := newBusinessEnv(t)
	const accountCount = 9_994
	const transactionCount = 500_000
	var bankID int64
	if err := e.admin.QueryRow(e.ctx, `SELECT id FROM ledger.account WHERE kind='BANK_DEDICATED'`).Scan(&bankID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `
		INSERT INTO ledger.account(kind,normal_side,owner_account)
		SELECT 'WALLET_AVAIL','CREDIT',100000+g FROM generate_series(1,$1) g`, accountCount); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `ALTER TABLE ledger.txn DISABLE TRIGGER USER; ALTER TABLE ledger.posting DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `
		INSERT INTO ledger.txn(txn_no,type,amount,idem_key,ref_type,ref_id,actor_id)
		SELECT 'SCALE-'||g,'topup',1,'topup:scale:'||g,'scale',g,1
		FROM generate_series(1,$1) g`, transactionCount); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `
		INSERT INTO ledger.posting(txn_id,account_id,side,amount)
		SELECT t.id,$1,'DEBIT',1 FROM ledger.txn t WHERE t.ref_type='scale'
		UNION ALL
		SELECT t.id,a.id,'CREDIT',1
		FROM ledger.txn t JOIN ledger.account a
		  ON a.owner_account=100000+((t.ref_id-1)%$2)+1 AND a.kind='WALLET_AVAIL'
		WHERE t.ref_type='scale'`, bankID, accountCount); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `ALTER TABLE ledger.posting ENABLE TRIGGER USER; ALTER TABLE ledger.txn ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `
		UPDATE ledger.balance b SET balance=x.amount,version=x.amount
		FROM (
			SELECT account_id,count(*)::bigint amount FROM ledger.posting GROUP BY account_id
		) x WHERE x.account_id=b.account_id`); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	run, err := reconciliationService(e, ledger.Options{}).RunDailyReconciliation(e.ctx, time.Now(), ledger.ReconciliationOptions{})
	elapsed := time.Since(started)
	if err != nil || run.Status != "ok" || len(run.Issues) != 0 {
		t.Fatalf("scale run=%+v err=%v elapsed=%s", run, err, elapsed)
	}
	var accounts, postings int64
	if err := e.admin.QueryRow(e.ctx, `SELECT (SELECT count(*) FROM ledger.account),(SELECT count(*) FROM ledger.posting)`).Scan(&accounts, &postings); err != nil {
		t.Fatal(err)
	}
	if accounts != 10_000 || postings != 1_000_000 {
		t.Fatalf("scale rows accounts=%d postings=%d", accounts, postings)
	}
	t.Logf("L-07 scale: accounts=%d postings=%d elapsed=%s", accounts, postings, elapsed)
}

func findIssue(t *testing.T, issues []ledger.ReconIssue, code string, accountID int64) ledger.ReconIssue {
	t.Helper()
	for _, item := range issues {
		if item.CheckCode == code && item.AccountID != nil && *item.AccountID == accountID {
			return item
		}
	}
	t.Fatalf("issue %s account %d missing: %+v", code, accountID, issues)
	return ledger.ReconIssue{}
}

func hasIssue(issues []ledger.ReconIssue, code string) bool {
	for _, item := range issues {
		if item.CheckCode == code {
			return true
		}
	}
	return false
}
