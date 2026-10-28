package ledger_test

import (
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
)

func TestFinancialCenterUsesLedgerAsItsOnlySource(t *testing.T) {
	e := newBusinessEnv(t)
	service := ledger.New(e.app, ledger.Options{AccountGuard: withdrawalGuard{}, PayeeCipher: withdrawalCipher(t)})
	reader := ledger.NewReadService(e.app)

	merchantAID, merchantBID := int64(7101), int64(7102)
	merchantA := mustWallet(t, e, 7101, &merchantAID)
	merchantB := mustWallet(t, e, 7102, &merchantBID)
	buyer := mustWallet(t, e, 7103, nil)
	for _, account := range []int64{merchantA.Available, merchantA.AdBudget, merchantB.Available, buyer.Available} {
		setBalance(t, e, account, 10_000)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if _, err := service.StorageFee(e.ctx, ledger.ChargeInput{ID: 1, WalletAccountID: merchantA.Available, Amount: 120, ActorID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdSpend(e.ctx, ledger.AdSpendInput{AdID: 2, AdBudget: merchantA.AdBudget, Amount: 80, ActorID: 1, Bucket: "20261018H02"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.MemberFee(e.ctx, ledger.ChargeInput{ID: 3, WalletAccountID: buyer.Available, Amount: 20, ActorID: 1}, "2026M10"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RequestWithdrawal(e.ctx, ledger.RequestWithdrawalInput{RequestKey: "finance-summary", RequesterAccountID: 7101, WalletAvailable: merchantA.Available, Amount: 50, PayeeAccount: "TEST-ACCOUNT-7101", ActorID: 9, SecondFactorVerified: true}); err != nil {
		t.Fatal(err)
	}

	// A valid historical posting must not leak into the selected reporting period.
	insertFinancialPosting(t, e, "historic-storage", "storage_fee", now.Add(-48*time.Hour), merchantA.Available, 999, "storage")

	// The position is a current-balance view. Seed an exact identity and rank here
	// so the assertions do not depend on unrelated posting scenarios.
	setBalance(t, e, singletonID(t, e.ctx, e.admin, "BANK_DEDICATED"), 1_300)
	setBalance(t, e, merchantA.Available, 400)
	setBalance(t, e, merchantB.Available, 300)
	setBalance(t, e, buyer.Available, 100)
	setBalance(t, e, merchantA.Frozen, 50)
	setBalance(t, e, merchantB.Frozen, 40)
	setBalance(t, e, singletonID(t, e.ctx, e.admin, "PENDING_PAYOUT"), 50)
	setBalance(t, e, singletonID(t, e.ctx, e.admin, "DELIVERY_ESCROW"), 60)
	setBalance(t, e, merchantA.AdBudget, 70)
	setBalance(t, e, singletonID(t, e.ctx, e.admin, "PLATFORM_HELD"), 80)
	setBalance(t, e, singletonID(t, e.ctx, e.admin, "PAYABLE_CARRIER"), 90)
	setBalance(t, e, singletonID(t, e.ctx, e.admin, "SUSPENSE"), 60)

	report, err := reader.FinancialCenter(e.ctx, ledger.FinancialCenterFilter{From: now.Add(-time.Hour), To: now.Add(time.Hour), MerchantLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Revenue) != 5 {
		t.Fatalf("revenue categories=%+v", report.Revenue)
	}
	wantRevenue := map[string]int64{"storage": 120, "ads": 80, "member": 20, "delivery_margin": 0, "settle_fee": 0}
	for _, item := range report.Revenue {
		if item.Amount != wantRevenue[item.Category] {
			t.Errorf("revenue %s=%d, want %d", item.Category, item.Amount, wantRevenue[item.Category])
		}
	}
	if report.RevenueTotal != 220 {
		t.Errorf("revenue total=%d, want 220", report.RevenueTotal)
	}
	if report.Position.InternalTotal != 1_300 || report.Position.BankDedicated != 1_300 || !report.Position.MatchesBank {
		t.Errorf("position=%+v", report.Position)
	}
	if report.Withdrawals.PendingCount != 1 || report.Withdrawals.PendingAmount != 50 {
		t.Errorf("withdrawals=%+v", report.Withdrawals)
	}
	if len(report.MerchantBalances) != 2 || report.MerchantBalances[0].MerchantID != merchantAID || report.MerchantBalances[0].Available != 400 {
		t.Errorf("merchant balances=%+v", report.MerchantBalances)
	}
}

func TestFinancialCenterRejectsInvalidPeriod(t *testing.T) {
	e := newBusinessEnv(t)
	service := ledger.NewReadService(e.app)
	now := time.Now()
	if _, err := service.FinancialCenter(e.ctx, ledger.FinancialCenterFilter{From: now, To: now}); err == nil {
		t.Fatal("expected invalid period error")
	}
}

func insertFinancialPosting(t *testing.T, e businessEnv, key, typ string, occurred time.Time, debit, amount int64, category string) {
	t.Helper()
	platform := singletonID(t, e.ctx, e.admin, "PLATFORM_HELD")
	tx, err := e.admin.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(e.ctx)
	var txnID int64
	if err := tx.QueryRow(e.ctx, `INSERT INTO ledger.txn(txn_no,type,amount,idem_key,actor_id,occurred_at) VALUES($1,$2,$3,$4,1,$5) RETURNING id`, key, typ, amount, key, occurred).Scan(&txnID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(e.ctx, `INSERT INTO ledger.posting(txn_id,account_id,side,amount,category) VALUES($1,$2,'DEBIT',$4,NULL),($1,$3,'CREDIT',$4,$5)`, txnID, debit, platform, amount, category); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(e.ctx); err != nil {
		t.Fatal(err)
	}
}
