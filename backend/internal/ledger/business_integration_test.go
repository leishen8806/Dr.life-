package ledger_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/url"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type businessEnv struct {
	ctx          context.Context
	admin, app   *pgxpool.Pool
	adminService *ledger.Service
	appService   *ledger.Service
}

type expectedPosting struct {
	kind, side string
	amount     int64
	category   string
}

func newBusinessEnv(t *testing.T) businessEnv {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	t.Cleanup(cancel)
	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("drlife_business_test"), postgres.WithUsername("test_admin"),
		postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate postgres: %v", err)
		}
	})
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(database, migrationDirectory(t)); err != nil {
		t.Fatal(err)
	}
	adminConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	adminConfig.MaxConns = 64
	admin, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	if _, err := admin.Exec(ctx, `CREATE ROLE app_ledger LOGIN PASSWORD 'test-only'; GRANT ledger_rw TO app_ledger`); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword("app_ledger", "test-only")
	appConfig, err := pgxpool.ParseConfig(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	appConfig.MaxConns = 64
	app, err := pgxpool.NewWithConfig(ctx, appConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	return businessEnv{ctx: ctx, admin: admin, app: app, adminService: ledger.New(admin, ledger.Options{}), appService: ledger.New(app, ledger.Options{})}
}

func TestBusinessPostingIntegration(t *testing.T) {
	e := newBusinessEnv(t)

	t.Run("EnsureWallet is concurrent and idempotent", func(t *testing.T) {
		merchantID := int64(7001)
		results := make(chan ledger.WalletAccounts, 100)
		errs := make(chan error, 100)
		var wg sync.WaitGroup
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				got, err := e.appService.EnsureWallet(e.ctx, 70001, &merchantID)
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
		var first ledger.WalletAccounts
		for got := range results {
			if first.Available == 0 {
				first = got
			} else if got != first {
				t.Fatalf("wallets changed: first=%+v got=%+v", first, got)
			}
		}
		var count int
		if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM ledger.account WHERE owner_account=70001`).Scan(&count); err != nil || count != 3 {
			t.Fatalf("accounts=%d error=%v", count, err)
		}
	})

	buyer := mustWallet(t, e, 1001, nil)
	merchantID := int64(2002)
	merchant := mustWallet(t, e, 1002, &merchantID)
	driver := mustWallet(t, e, 1003, nil)
	payee := mustWallet(t, e, 1004, nil)
	for _, account := range []int64{buyer.Available, buyer.Frozen, merchant.Available, merchant.Frozen, merchant.AdBudget, driver.Available, payee.Available} {
		setBalance(t, e, account, 1_000_000)
	}
	for _, kind := range []string{"BANK_DEDICATED", "PENDING_PAYOUT", "PLATFORM_HELD", "PAYABLE_CARRIER"} {
		setBalance(t, e, singletonID(t, e.ctx, e.admin, kind), 1_000_000)
	}

	t.Run("max int delivery split stays exact", func(t *testing.T) {
		largeMerchant := mustWallet(t, e, 1100, nil)
		largeDriver := mustWallet(t, e, 1101, nil)
		setBalance(t, e, largeMerchant.Available, math.MaxInt64)
		if _, err := e.appService.DeliveryHold(e.ctx, ledger.ChargeInput{ID: 8000, WalletAccountID: largeMerchant.Available, Amount: math.MaxInt64, ActorID: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.appService.DeliveryRelease(e.ctx, ledger.DeliveryReleaseInput{OrderID: 8000, Amount: math.MaxInt64, ActorID: 2, DriverWallet: largeDriver.Available, DriverBP: 10000, Remainder: 0, ProofID: "proof_max"}); err != nil {
			t.Fatal(err)
		}
		if got := getBalance(t, e.ctx, e.admin, largeDriver.Available); got != math.MaxInt64 {
			t.Fatalf("driver balance=%d", got)
		}
	})

	t.Run("one unit delivery omits zero shares", func(t *testing.T) {
		if _, err := e.appService.DeliveryHold(e.ctx, ledger.ChargeInput{ID: 8001, WalletAccountID: merchant.Available, Amount: 1, ActorID: 1}); err != nil {
			t.Fatal(err)
		}
		result, err := e.appService.DeliveryRelease(e.ctx, ledger.DeliveryReleaseInput{OrderID: 8001, Amount: 1, ActorID: 2, DriverWallet: driver.Available, DriverBP: 0, PlatformBP: 0, CarrierBP: 10000, Remainder: 2, ProofID: "proof_one"})
		if err != nil {
			t.Fatal(err)
		}
		assertTxnCounts(t, e.ctx, e.admin, "delivery_release:order:8001", 1, 2, 1)
		var escrow int64
		if err := e.admin.QueryRow(e.ctx, `SELECT COALESCE(sum(CASE p.side WHEN 'CREDIT' THEN p.amount ELSE -p.amount END),0) FROM ledger.posting p JOIN ledger.txn x ON x.id=p.txn_id JOIN ledger.account a ON a.id=p.account_id WHERE a.kind='DELIVERY_ESCROW' AND x.ref_type='order' AND x.ref_id=8001`).Scan(&escrow); err != nil || escrow != 0 {
			t.Fatalf("txn=%d escrow=%d error=%v", result.TxnID, escrow, err)
		}
	})

	if _, err := e.appService.DeliveryHold(e.ctx, ledger.ChargeInput{ID: 9002, WalletAccountID: merchant.Available, Amount: 101, ActorID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.appService.DeliveryHold(e.ctx, ledger.ChargeInput{ID: 9003, WalletAccountID: merchant.Available, Amount: 100, ActorID: 1}); err != nil {
		t.Fatal(err)
	}

	type businessCase struct {
		name, typ, key, refType string
		refID                   int64
		call                    func() (ledger.PostResult, error)
		postings                []expectedPosting
	}
	cases := []businessCase{
		{"topup", "topup", "topup:bankline:101", "bankline", 101, func() (ledger.PostResult, error) {
			return e.appService.Topup(e.ctx, ledger.TopupInput{BankStatementLineID: 101, BankConfirmed: true, WalletAccountID: buyer.Available, Amount: 101, ActorID: 1})
		}, []expectedPosting{{"BANK_DEDICATED", "DEBIT", 101, ""}, {"WALLET_AVAIL", "CREDIT", 101, ""}}},
		{"freeze", "freeze", "freeze:hold:plan_batch-102", "plan_batch", 102, func() (ledger.PostResult, error) {
			return e.appService.Freeze(e.ctx, ledger.FreezeInput{RefType: "plan_batch", RefID: 102, WalletAvailable: buyer.Available, WalletFrozen: buyer.Frozen, Amount: 102, ActorID: 1})
		}, []expectedPosting{{"WALLET_AVAIL", "DEBIT", 102, ""}, {"WALLET_FROZEN", "CREDIT", 102, ""}}},
		{"settle", "settle", "settle:hold:103", "hold", 103, func() (ledger.PostResult, error) {
			return e.appService.Settle(e.ctx, ledger.SettleInput{HoldID: 103, BuyerFrozen: buyer.Frozen, PayeeAvailable: payee.Available, Amount: 103, Fee: 3, ActorID: 1})
		}, []expectedPosting{{"WALLET_FROZEN", "DEBIT", 103, ""}, {"WALLET_AVAIL", "CREDIT", 100, ""}, {"PLATFORM_HELD", "CREDIT", 3, "settle_fee"}}},
		{"refund frozen", "refund", "refund:hold:104", "hold", 104, func() (ledger.PostResult, error) {
			return e.appService.RefundFrozen(e.ctx, ledger.RefundFrozenInput{HoldID: 104, WalletFrozen: buyer.Frozen, WalletAvailable: buyer.Available, Amount: 104, ActorID: 2, PayerActorID: 1})
		}, []expectedPosting{{"WALLET_FROZEN", "DEBIT", 104, ""}, {"WALLET_AVAIL", "CREDIT", 104, ""}}},
		{"withdraw request", "withdraw_req", "withdraw_req:withdrawal:105", "withdrawal", 105, func() (ledger.PostResult, error) {
			return e.appService.WithdrawReq(e.ctx, ledger.WithdrawReqInput{WithdrawalID: 105, WalletAvailable: buyer.Available, Amount: 105, ActorID: 1})
		}, []expectedPosting{{"WALLET_AVAIL", "DEBIT", 105, ""}, {"PENDING_PAYOUT", "CREDIT", 105, ""}}},
		{"payout", "payout", "payout:withdrawal:106", "withdrawal", 106, func() (ledger.PostResult, error) {
			return e.appService.Payout(e.ctx, ledger.PayoutInput{WithdrawalID: 106, Amount: 106, RequestedAmount: 106, ActorID: 1})
		}, []expectedPosting{{"PENDING_PAYOUT", "DEBIT", 106, ""}, {"BANK_DEDICATED", "CREDIT", 106, ""}}},
		{"payout fail", "payout_fail", "payout_fail:withdrawal:107", "withdrawal", 107, func() (ledger.PostResult, error) {
			return e.appService.PayoutFail(e.ctx, ledger.PayoutFailInput{WithdrawalID: 107, WalletAvailable: buyer.Available, Amount: 107, ActorID: 1})
		}, []expectedPosting{{"PENDING_PAYOUT", "DEBIT", 107, ""}, {"WALLET_AVAIL", "CREDIT", 107, ""}}},
		{"storage fee", "storage_fee", "storage_fee:bill:108", "bill", 108, func() (ledger.PostResult, error) {
			return e.appService.StorageFee(e.ctx, ledger.ChargeInput{ID: 108, WalletAccountID: merchant.Available, Amount: 108, ActorID: 1})
		}, []expectedPosting{{"WALLET_AVAIL", "DEBIT", 108, ""}, {"PLATFORM_HELD", "CREDIT", 108, "storage"}}},
		{"delivery hold", "delivery_hold", "delivery_hold:order:109", "order", 109, func() (ledger.PostResult, error) {
			return e.appService.DeliveryHold(e.ctx, ledger.ChargeInput{ID: 109, WalletAccountID: merchant.Available, Amount: 109, ActorID: 1})
		}, []expectedPosting{{"WALLET_AVAIL", "DEBIT", 109, ""}, {"DELIVERY_ESCROW", "CREDIT", 109, ""}}},
		{"delivery release", "delivery_release", "delivery_release:order:9002", "order", 9002, func() (ledger.PostResult, error) {
			return e.appService.DeliveryRelease(e.ctx, ledger.DeliveryReleaseInput{OrderID: 9002, Amount: 101, ActorID: 1, DriverWallet: driver.Available, DriverBP: 3333, PlatformBP: 3333, CarrierBP: 3334, Remainder: 2, ProofID: "proof_9002"})
		}, []expectedPosting{{"DELIVERY_ESCROW", "DEBIT", 101, ""}, {"WALLET_AVAIL", "CREDIT", 33, ""}, {"PLATFORM_HELD", "CREDIT", 33, "delivery_margin"}, {"PAYABLE_CARRIER", "CREDIT", 35, ""}}},
		{"delivery refund", "delivery_refund", "delivery_refund:order:9003", "order", 9003, func() (ledger.PostResult, error) {
			return e.appService.DeliveryRefund(e.ctx, ledger.DeliveryRefundInput{OrderID: 9003, MerchantWallet: merchant.Available, Amount: 100, ActorID: 1})
		}, []expectedPosting{{"DELIVERY_ESCROW", "DEBIT", 100, ""}, {"WALLET_AVAIL", "CREDIT", 100, ""}}},
		{"ad charge", "ad_charge", "ad_charge:ad:112", "ad", 112, func() (ledger.PostResult, error) {
			return e.appService.AdCharge(e.ctx, ledger.AdTransferInput{AdID: 112, WalletAvailable: merchant.Available, AdBudget: merchant.AdBudget, Amount: 112, ActorID: 1})
		}, []expectedPosting{{"WALLET_AVAIL", "DEBIT", 112, ""}, {"AD_BUDGET", "CREDIT", 112, ""}}},
		{"ad spend", "ad_spend", "ad_spend:ad:113:20261013H09", "ad", 113, func() (ledger.PostResult, error) {
			return e.appService.AdSpend(e.ctx, ledger.AdSpendInput{AdID: 113, AdBudget: merchant.AdBudget, Amount: 113, ActorID: 1, Bucket: "20261013H09"})
		}, []expectedPosting{{"AD_BUDGET", "DEBIT", 113, ""}, {"PLATFORM_HELD", "CREDIT", 113, "ads"}}},
		{"ad refund", "ad_refund", "ad_refund:ad:114", "ad", 114, func() (ledger.PostResult, error) {
			return e.appService.AdRefund(e.ctx, ledger.AdTransferInput{AdID: 114, WalletAvailable: merchant.Available, AdBudget: merchant.AdBudget, Amount: 114, ActorID: 1})
		}, []expectedPosting{{"AD_BUDGET", "DEBIT", 114, ""}, {"WALLET_AVAIL", "CREDIT", 114, ""}}},
		{"member fee", "member_fee", "member_fee:member:115:2026M10", "member", 115, func() (ledger.PostResult, error) {
			return e.appService.MemberFee(e.ctx, ledger.ChargeInput{ID: 115, WalletAccountID: buyer.Available, Amount: 115, ActorID: 1}, "2026M10")
		}, []expectedPosting{{"WALLET_AVAIL", "DEBIT", 115, ""}, {"PLATFORM_HELD", "CREDIT", 115, "member"}}},
		{"sweep", "sweep", "sweep:sweep:116", "sweep", 116, func() (ledger.PostResult, error) {
			return e.appService.Sweep(e.ctx, ledger.ReviewedPaymentInput{ID: 116, Amount: 116, ActorID: 1, ReviewerActorID: 2})
		}, []expectedPosting{{"PLATFORM_HELD", "DEBIT", 116, ""}, {"BANK_DEDICATED", "CREDIT", 116, ""}}},
		{"carrier pay", "carrier_pay", "carrier_pay:statement:117", "statement", 117, func() (ledger.PostResult, error) {
			return e.appService.CarrierPay(e.ctx, ledger.ReviewedPaymentInput{ID: 117, Amount: 117, ActorID: 1, ReviewerActorID: 2})
		}, []expectedPosting{{"PAYABLE_CARRIER", "DEBIT", 117, ""}, {"BANK_DEDICATED", "CREDIT", 117, ""}}},
		{"bank difference", "bank_diff", "bank_diff:bankline:118", "bankline", 118, func() (ledger.PostResult, error) {
			return e.appService.BankDiff(e.ctx, ledger.BankDiffInput{BankLineID: 118, Amount: 118, ActorID: 1, Direction: "IN", Reason: "unidentified deposit"})
		}, []expectedPosting{{"BANK_DEDICATED", "DEBIT", 118, ""}, {"SUSPENSE", "CREDIT", 118, ""}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first, err := tc.call()
			if err != nil {
				t.Fatal(err)
			}
			assertBusinessTxn(t, e, first.TxnID, tc.typ, tc.key, tc.refType, tc.refID, tc.postings)
			var wg sync.WaitGroup
			errs := make(chan error, 100)
			for range 100 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					got, err := tc.call()
					if err != nil {
						errs <- err
						return
					}
					if got.TxnID != first.TxnID || !got.Replayed {
						errs <- fmt.Errorf("replay=%+v first=%+v", got, first)
					}
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Error(err)
			}
			assertTxnCounts(t, e.ctx, e.admin, tc.key, 1, len(tc.postings), 1)
		})
	}

	t.Run("same key with different content conflicts", func(t *testing.T) {
		_, err := e.appService.Topup(e.ctx, ledger.TopupInput{BankStatementLineID: 101, BankConfirmed: true, WalletAccountID: buyer.Available, Amount: 102, ActorID: 1})
		if !errors.Is(err, ledger.ErrIdempotencyConflict) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("business prerequisites reject before posting", func(t *testing.T) {
		if _, err := e.appService.Topup(e.ctx, ledger.TopupInput{BankStatementLineID: 1501, WalletAccountID: buyer.Available, Amount: 1, ActorID: 1}); !errors.Is(err, ledger.ErrInvalidRequest) {
			t.Errorf("unconfirmed topup error=%v", err)
		}
		if _, err := e.appService.Payout(e.ctx, ledger.PayoutInput{WithdrawalID: 1502, Amount: 2, RequestedAmount: 1, ActorID: 1}); !errors.Is(err, ledger.ErrInvalidAmount) {
			t.Errorf("payout mismatch error=%v", err)
		}
		if _, err := e.appService.Settle(e.ctx, ledger.SettleInput{HoldID: 1503, BuyerFrozen: buyer.Frozen, PayeeAvailable: payee.Available, Amount: 1, Fee: 2, ActorID: 1}); !errors.Is(err, ledger.ErrInvalidRequest) {
			t.Errorf("fee error=%v", err)
		}
		if _, err := e.appService.DeliveryRelease(e.ctx, ledger.DeliveryReleaseInput{OrderID: 1504, Amount: 1, ActorID: 1, DriverWallet: driver.Available, DriverBP: 10000, Remainder: 0}); !errors.Is(err, ledger.ErrInvalidRequest) {
			t.Errorf("missing proof error=%v", err)
		}
		if _, err := e.appService.BankDiff(e.ctx, ledger.BankDiffInput{BankLineID: 1505, Amount: 1, ActorID: 1, Direction: "SIDEWAYS", Reason: "bad"}); !errors.Is(err, ledger.ErrInvalidRequest) {
			t.Errorf("bank direction error=%v", err)
		}
		if _, err := e.appService.WithdrawReq(e.ctx, ledger.WithdrawReqInput{WithdrawalID: 0, WalletAvailable: buyer.Available, Amount: 1, ActorID: 1}); !errors.Is(err, ledger.ErrInvalidRequest) {
			t.Errorf("zero business id error=%v", err)
		}
	})

	t.Run("insufficient funds leave no trace", func(t *testing.T) {
		empty := mustWallet(t, e, 1200, nil)
		emptyMerchantID := int64(1201)
		emptyMerchant := mustWallet(t, e, 1201, &emptyMerchantID)
		checks := []struct {
			key  string
			call func() (ledger.PostResult, error)
		}{
			{"freeze:hold:empty-1", func() (ledger.PostResult, error) {
				return e.appService.Freeze(e.ctx, ledger.FreezeInput{RefType: "empty", RefID: 1, WalletAvailable: empty.Available, WalletFrozen: empty.Frozen, Amount: 1, ActorID: 1})
			}},
			{"withdraw_req:withdrawal:1201", func() (ledger.PostResult, error) {
				return e.appService.WithdrawReq(e.ctx, ledger.WithdrawReqInput{WithdrawalID: 1201, WalletAvailable: empty.Available, Amount: 1, ActorID: 1})
			}},
			{"storage_fee:bill:1202", func() (ledger.PostResult, error) {
				return e.appService.StorageFee(e.ctx, ledger.ChargeInput{ID: 1202, WalletAccountID: empty.Available, Amount: 1, ActorID: 1})
			}},
			{"delivery_hold:order:1203", func() (ledger.PostResult, error) {
				return e.appService.DeliveryHold(e.ctx, ledger.ChargeInput{ID: 1203, WalletAccountID: empty.Available, Amount: 1, ActorID: 1})
			}},
			{"ad_charge:ad:1204", func() (ledger.PostResult, error) {
				return e.appService.AdCharge(e.ctx, ledger.AdTransferInput{AdID: 1204, WalletAvailable: emptyMerchant.Available, AdBudget: emptyMerchant.AdBudget, Amount: 1, ActorID: 1})
			}},
			{"member_fee:member:1205:p1", func() (ledger.PostResult, error) {
				return e.appService.MemberFee(e.ctx, ledger.ChargeInput{ID: 1205, WalletAccountID: empty.Available, Amount: 1, ActorID: 1}, "p1")
			}},
		}
		for _, check := range checks {
			if _, err := check.call(); !errors.Is(err, ledger.ErrInsufficientFunds) {
				t.Errorf("%s error=%v", check.key, err)
			}
			assertTxnCounts(t, e.ctx, e.admin, check.key, 0, 0, 0)
		}
		platform := singletonID(t, e.ctx, e.admin, "PLATFORM_HELD")
		tooMuch := getBalance(t, e.ctx, e.admin, platform) + 1
		if _, err := e.appService.Sweep(e.ctx, ledger.ReviewedPaymentInput{ID: 1206, Amount: tooMuch, ActorID: 1, ReviewerActorID: 2}); !errors.Is(err, ledger.ErrInsufficientFunds) {
			t.Fatalf("sweep error=%v", err)
		}
		if getBalance(t, e.ctx, e.admin, empty.Available) != 0 {
			t.Fatal("empty wallet became negative")
		}
	})

	t.Run("separation of duties", func(t *testing.T) {
		if _, err := e.appService.RefundFrozen(e.ctx, ledger.RefundFrozenInput{HoldID: 1301, WalletFrozen: buyer.Frozen, WalletAvailable: buyer.Available, Amount: 1, ActorID: 7, PayerActorID: 7}); !errors.Is(err, ledger.ErrSeparationDuty) {
			t.Errorf("refund error=%v", err)
		}
		if _, err := e.appService.Sweep(e.ctx, ledger.ReviewedPaymentInput{ID: 1302, Amount: 1, ActorID: 7, ReviewerActorID: 7}); !errors.Is(err, ledger.ErrSeparationDuty) {
			t.Errorf("sweep error=%v", err)
		}
		if _, err := e.appService.CarrierPay(e.ctx, ledger.ReviewedPaymentInput{ID: 1303, Amount: 1, ActorID: 7, ReviewerActorID: 7}); !errors.Is(err, ledger.ErrSeparationDuty) {
			t.Errorf("carrier pay error=%v", err)
		}
	})

	t.Run("wallet owner mismatches leave no trace", func(t *testing.T) {
		other := mustWallet(t, e, 1600, nil)
		checks := []struct {
			key  string
			call func() (ledger.PostResult, error)
		}{
			{"freeze:hold:owner-1601", func() (ledger.PostResult, error) {
				return e.appService.Freeze(e.ctx, ledger.FreezeInput{RefType: "owner", RefID: 1601, WalletAvailable: buyer.Available, WalletFrozen: other.Frozen, Amount: 1, ActorID: 1})
			}},
			{"refund:hold:1602", func() (ledger.PostResult, error) {
				return e.appService.RefundFrozen(e.ctx, ledger.RefundFrozenInput{HoldID: 1602, WalletFrozen: buyer.Frozen, WalletAvailable: other.Available, Amount: 1, ActorID: 2, PayerActorID: 1})
			}},
		}
		for _, check := range checks {
			if _, err := check.call(); !errors.Is(err, ledger.ErrAccountOwnerMismatch) {
				t.Errorf("%s error=%v", check.key, err)
			}
			assertTxnCounts(t, e.ctx, e.admin, check.key, 0, 0, 0)
		}

		request := ledger.RefundFrozenInput{HoldID: 1603, WalletFrozen: buyer.Frozen, WalletAvailable: buyer.Available, Amount: 1, ActorID: 2, PayerActorID: 1}
		result, err := e.appService.RefundFrozen(e.ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		var note string
		if err := e.admin.QueryRow(e.ctx, `SELECT note FROM ledger.txn WHERE id=$1`, result.TxnID).Scan(&note); err != nil || note != "payer_actor:1" {
			t.Fatalf("note=%q error=%v", note, err)
		}
		request.PayerActorID = 3
		if _, err := e.appService.RefundFrozen(e.ctx, request); !errors.Is(err, ledger.ErrIdempotencyConflict) {
			t.Fatalf("changed payer replay error=%v", err)
		}
		assertTxnCounts(t, e.ctx, e.admin, "refund:hold:1603", 1, 2, 1)
	})

	t.Run("advertising owner mismatches leave no trace", func(t *testing.T) {
		otherMerchantID := int64(1610)
		other := mustWallet(t, e, 1610, &otherMerchantID)
		checks := []struct {
			key  string
			call func() (ledger.PostResult, error)
		}{
			{"ad_charge:ad:1611", func() (ledger.PostResult, error) {
				return e.appService.AdCharge(e.ctx, ledger.AdTransferInput{AdID: 1611, WalletAvailable: merchant.Available, AdBudget: other.AdBudget, Amount: 1, ActorID: 1})
			}},
			{"ad_refund:ad:1612", func() (ledger.PostResult, error) {
				return e.appService.AdRefund(e.ctx, ledger.AdTransferInput{AdID: 1612, WalletAvailable: other.Available, AdBudget: merchant.AdBudget, Amount: 1, ActorID: 1})
			}},
		}
		for _, check := range checks {
			if _, err := check.call(); !errors.Is(err, ledger.ErrAccountOwnerMismatch) {
				t.Errorf("%s error=%v", check.key, err)
			}
			assertTxnCounts(t, e.ctx, e.admin, check.key, 0, 0, 0)
		}
	})

	t.Run("delivery requires and preserves the original hold", func(t *testing.T) {
		if _, err := e.appService.DeliveryRelease(e.ctx, ledger.DeliveryReleaseInput{OrderID: 1620, Amount: 1, ActorID: 2, DriverWallet: driver.Available, DriverBP: 10000, Remainder: 0, ProofID: "proof_1620"}); !errors.Is(err, ledger.ErrAccountOwnerMismatch) {
			t.Fatalf("release without hold error=%v", err)
		}
		assertTxnCounts(t, e.ctx, e.admin, "delivery_release:order:1620", 0, 0, 0)
		if _, err := e.appService.DeliveryRefund(e.ctx, ledger.DeliveryRefundInput{OrderID: 1621, MerchantWallet: merchant.Available, Amount: 1, ActorID: 2}); !errors.Is(err, ledger.ErrAccountOwnerMismatch) {
			t.Fatalf("refund without hold error=%v", err)
		}
		assertTxnCounts(t, e.ctx, e.admin, "delivery_refund:order:1621", 0, 0, 0)

		if _, err := e.appService.DeliveryHold(e.ctx, ledger.ChargeInput{ID: 1622, WalletAccountID: merchant.Available, Amount: 5, ActorID: 1}); err != nil {
			t.Fatal(err)
		}
		otherMerchantID := int64(1622)
		other := mustWallet(t, e, 1622, &otherMerchantID)
		if _, err := e.appService.DeliveryRefund(e.ctx, ledger.DeliveryRefundInput{OrderID: 1622, MerchantWallet: other.Available, Amount: 5, ActorID: 2}); !errors.Is(err, ledger.ErrAccountOwnerMismatch) {
			t.Fatalf("refund to another wallet error=%v", err)
		}
		assertTxnCounts(t, e.ctx, e.admin, "delivery_refund:order:1622", 0, 0, 0)
		var held int64
		if err := e.admin.QueryRow(e.ctx, `
			SELECT COALESCE(sum(CASE p.side WHEN 'CREDIT' THEN p.amount ELSE -p.amount END),0)
			FROM ledger.posting p JOIN ledger.txn x ON x.id=p.txn_id JOIN ledger.account a ON a.id=p.account_id
			WHERE a.kind='DELIVERY_ESCROW' AND x.ref_type='order' AND x.ref_id=1622`).Scan(&held); err != nil || held != 5 {
			t.Fatalf("held=%d error=%v", held, err)
		}
	})

	t.Run("order escrow is isolated and release conflicts with refund", func(t *testing.T) {
		if _, err := e.appService.DeliveryHold(e.ctx, ledger.ChargeInput{ID: 1401, WalletAccountID: merchant.Available, Amount: 10, ActorID: 1}); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		errs := make(chan error, 2)
		go func() {
			<-start
			_, err := e.appService.DeliveryRelease(e.ctx, ledger.DeliveryReleaseInput{OrderID: 1401, Amount: 10, ActorID: 2, DriverWallet: driver.Available, DriverBP: 10000, Remainder: 0, ProofID: "proof_1401"})
			errs <- err
		}()
		go func() {
			<-start
			_, err := e.appService.DeliveryRefund(e.ctx, ledger.DeliveryRefundInput{OrderID: 1401, MerchantWallet: merchant.Available, Amount: 10, ActorID: 3})
			errs <- err
		}()
		close(start)
		successes, rejected := 0, 0
		for range 2 {
			err := <-errs
			switch {
			case err == nil:
				successes++
			case errors.Is(err, ledger.ErrEscrowExceeded):
				rejected++
			default:
				t.Errorf("unexpected error=%v", err)
			}
		}
		if successes != 1 || rejected != 1 {
			t.Fatalf("success=%d rejected=%d", successes, rejected)
		}
		if _, err := e.appService.DeliveryRelease(e.ctx, ledger.DeliveryReleaseInput{OrderID: 1402, Amount: 1, ActorID: 2, DriverWallet: driver.Available, DriverBP: 10000, Remainder: 0, ProofID: "proof_1402"}); !errors.Is(err, ledger.ErrAccountOwnerMismatch) {
			t.Fatalf("cross-order release error=%v", err)
		}
	})
}

func TestBusinessRandomSequenceMaintainsInvariant(t *testing.T) {
	e := newBusinessEnv(t)
	buyer := mustWallet(t, e, 2001, nil)
	merchantID := int64(2202)
	merchant := mustWallet(t, e, 2002, &merchantID)
	driver := mustWallet(t, e, 2003, nil)
	payee := mustWallet(t, e, 2004, nil)
	for i, wallet := range []int64{buyer.Available, merchant.Available} {
		if _, err := e.appService.Topup(e.ctx, ledger.TopupInput{BankStatementLineID: int64(20000 + i), BankConfirmed: true, WalletAccountID: wallet, Amount: 100_000, ActorID: 1}); err != nil {
			t.Fatal(err)
		}
		assertAccountingInvariant(t, e)
	}

	rng := rand.New(rand.NewSource(20261013))
	call := func(fn func() (ledger.PostResult, error)) bool {
		_, err := fn()
		if err != nil && !errors.Is(err, ledger.ErrInsufficientFunds) && !errors.Is(err, ledger.ErrEscrowExceeded) {
			t.Fatalf("unexpected business error: %v", err)
		}
		assertAccountingInvariant(t, e)
		return err == nil
	}
	for step := range 300 {
		id := int64(30000 + step*10)
		amount := int64(rng.Intn(500) + 1)
		switch rng.Intn(11) {
		case 0:
			call(func() (ledger.PostResult, error) {
				return e.appService.Topup(e.ctx, ledger.TopupInput{BankStatementLineID: id, BankConfirmed: true, WalletAccountID: buyer.Available, Amount: amount, ActorID: 1})
			})
		case 1:
			if call(func() (ledger.PostResult, error) {
				return e.appService.Freeze(e.ctx, ledger.FreezeInput{RefType: "batch", RefID: id, WalletAvailable: buyer.Available, WalletFrozen: buyer.Frozen, Amount: amount, ActorID: 1})
			}) {
				if rng.Intn(2) == 0 {
					call(func() (ledger.PostResult, error) {
						return e.appService.Settle(e.ctx, ledger.SettleInput{HoldID: id, BuyerFrozen: buyer.Frozen, PayeeAvailable: payee.Available, Amount: amount, Fee: amount % 7, ActorID: 2})
					})
				} else {
					call(func() (ledger.PostResult, error) {
						return e.appService.RefundFrozen(e.ctx, ledger.RefundFrozenInput{HoldID: id, WalletFrozen: buyer.Frozen, WalletAvailable: buyer.Available, Amount: amount, ActorID: 2, PayerActorID: 1})
					})
				}
			}
		case 2:
			if call(func() (ledger.PostResult, error) {
				return e.appService.WithdrawReq(e.ctx, ledger.WithdrawReqInput{WithdrawalID: id, WalletAvailable: buyer.Available, Amount: amount, ActorID: 1})
			}) {
				if rng.Intn(2) == 0 {
					call(func() (ledger.PostResult, error) {
						return e.appService.Payout(e.ctx, ledger.PayoutInput{WithdrawalID: id, Amount: amount, RequestedAmount: amount, ActorID: 2})
					})
				} else {
					call(func() (ledger.PostResult, error) {
						return e.appService.PayoutFail(e.ctx, ledger.PayoutFailInput{WithdrawalID: id, WalletAvailable: buyer.Available, Amount: amount, ActorID: 2})
					})
				}
			}
		case 3:
			call(func() (ledger.PostResult, error) {
				return e.appService.StorageFee(e.ctx, ledger.ChargeInput{ID: id, WalletAccountID: merchant.Available, Amount: amount, ActorID: 1})
			})
		case 4:
			if call(func() (ledger.PostResult, error) {
				return e.appService.DeliveryHold(e.ctx, ledger.ChargeInput{ID: id, WalletAccountID: merchant.Available, Amount: amount, ActorID: 1})
			}) {
				if rng.Intn(2) == 0 {
					call(func() (ledger.PostResult, error) {
						return e.appService.DeliveryRelease(e.ctx, ledger.DeliveryReleaseInput{OrderID: id, Amount: amount, ActorID: 2, DriverWallet: driver.Available, DriverBP: 7000, PlatformBP: 1000, CarrierBP: 2000, Remainder: 0, ProofID: fmt.Sprintf("proof_%d", id)})
					})
				} else {
					call(func() (ledger.PostResult, error) {
						return e.appService.DeliveryRefund(e.ctx, ledger.DeliveryRefundInput{OrderID: id, MerchantWallet: merchant.Available, Amount: amount, ActorID: 2})
					})
				}
			}
		case 5:
			if call(func() (ledger.PostResult, error) {
				return e.appService.AdCharge(e.ctx, ledger.AdTransferInput{AdID: id, WalletAvailable: merchant.Available, AdBudget: merchant.AdBudget, Amount: amount, ActorID: 1})
			}) {
				if rng.Intn(2) == 0 {
					call(func() (ledger.PostResult, error) {
						return e.appService.AdSpend(e.ctx, ledger.AdSpendInput{AdID: id, AdBudget: merchant.AdBudget, Amount: amount, ActorID: 2, Bucket: "daily"})
					})
				} else {
					call(func() (ledger.PostResult, error) {
						return e.appService.AdRefund(e.ctx, ledger.AdTransferInput{AdID: id, WalletAvailable: merchant.Available, AdBudget: merchant.AdBudget, Amount: amount, ActorID: 2})
					})
				}
			}
		case 6:
			call(func() (ledger.PostResult, error) {
				return e.appService.MemberFee(e.ctx, ledger.ChargeInput{ID: id, WalletAccountID: buyer.Available, Amount: amount, ActorID: 1}, "monthly")
			})
		case 7:
			call(func() (ledger.PostResult, error) {
				return e.appService.Sweep(e.ctx, ledger.ReviewedPaymentInput{ID: id, Amount: amount, ActorID: 1, ReviewerActorID: 2})
			})
		case 8:
			call(func() (ledger.PostResult, error) {
				return e.appService.CarrierPay(e.ctx, ledger.ReviewedPaymentInput{ID: id, Amount: amount, ActorID: 1, ReviewerActorID: 2})
			})
		case 9:
			direction := "IN"
			if rng.Intn(2) == 0 {
				direction = "OUT"
			}
			call(func() (ledger.PostResult, error) {
				return e.appService.BankDiff(e.ctx, ledger.BankDiffInput{BankLineID: id, Amount: amount, ActorID: 1, Direction: direction, Reason: "fixed-seed reconciliation"})
			})
		case 10:
			call(func() (ledger.PostResult, error) {
				return e.appService.Settle(e.ctx, ledger.SettleInput{HoldID: id, BuyerFrozen: buyer.Frozen, PayeeAvailable: payee.Available, Amount: amount, Fee: 0, ActorID: 2})
			})
		}
	}
}

func mustWallet(t *testing.T, e businessEnv, owner int64, merchant *int64) ledger.WalletAccounts {
	t.Helper()
	got, err := e.appService.EnsureWallet(e.ctx, owner, merchant)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func setBalance(t *testing.T, e businessEnv, account, balance int64) {
	t.Helper()
	if _, err := e.admin.Exec(e.ctx, `UPDATE ledger.balance SET balance=$2 WHERE account_id=$1`, account, balance); err != nil {
		t.Fatal(err)
	}
}

func assertAccountingInvariant(t *testing.T, e businessEnv) {
	t.Helper()
	var bank, liabilities int64
	if err := e.admin.QueryRow(e.ctx, `SELECT b.balance FROM ledger.balance b JOIN ledger.account a ON a.id=b.account_id WHERE a.kind='BANK_DEDICATED'`).Scan(&bank); err != nil {
		t.Fatal(err)
	}
	if err := e.admin.QueryRow(e.ctx, `SELECT COALESCE(sum(b.balance),0) FROM ledger.balance b JOIN ledger.account a ON a.id=b.account_id WHERE a.normal_side='CREDIT'`).Scan(&liabilities); err != nil {
		t.Fatal(err)
	}
	if bank != liabilities {
		t.Fatalf("bank=%d liabilities=%d", bank, liabilities)
	}
	var forbiddenNegative int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM ledger.balance WHERE balance<0 AND NOT allow_negative`).Scan(&forbiddenNegative); err != nil {
		t.Fatal(err)
	}
	if forbiddenNegative != 0 {
		t.Fatalf("non-negative accounts below zero=%d", forbiddenNegative)
	}
}

func assertBusinessTxn(t *testing.T, e businessEnv, txnID int64, typ, key, refType string, refID int64, want []expectedPosting) {
	t.Helper()
	var gotType, gotKey, gotRefType string
	var gotRefID int64
	if err := e.admin.QueryRow(e.ctx, `SELECT type,idem_key,ref_type,ref_id FROM ledger.txn WHERE id=$1`, txnID).Scan(&gotType, &gotKey, &gotRefType, &gotRefID); err != nil {
		t.Fatal(err)
	}
	if gotType != typ || gotKey != key || gotRefType != refType || gotRefID != refID {
		t.Fatalf("metadata=%s/%s/%s/%d", gotType, gotKey, gotRefType, gotRefID)
	}
	rows, err := e.admin.Query(e.ctx, `SELECT a.kind,p.side,p.amount,COALESCE(p.category,'') FROM ledger.posting p JOIN ledger.account a ON a.id=p.account_id WHERE p.txn_id=$1`, txnID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := make([]expectedPosting, 0, len(want))
	for rows.Next() {
		var posting expectedPosting
		if err := rows.Scan(&posting.kind, &posting.side, &posting.amount, &posting.category); err != nil {
			t.Fatal(err)
		}
		got = append(got, posting)
	}
	sortPostings := func(items []expectedPosting) {
		sort.Slice(items, func(i, j int) bool { return fmt.Sprint(items[i]) < fmt.Sprint(items[j]) })
	}
	sortPostings(got)
	sortPostings(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("postings=%v want=%v", got, want)
	}
}
