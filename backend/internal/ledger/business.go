package ledger

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

var (
	ErrEscrowExceeded       = errors.New("order delivery escrow exceeded")
	ErrSeparationDuty       = errors.New("ledger separation of duties violated")
	ErrAccountOwnerMismatch = errors.New("ledger account owners do not match")
)

var keySegmentPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type TopupInput struct {
	BankStatementLineID int64
	ManualConfirmation  string
	BankConfirmed       bool
	WalletAccountID     int64
	Amount              int64
	ActorID             int64
}

func (s *Service) Topup(ctx context.Context, in TopupInput) (PostResult, error) {
	if !in.BankConfirmed || in.BankStatementLineID <= 0 && !keySegmentPattern.MatchString(in.ManualConfirmation) {
		return PostResult{}, ErrInvalidRequest
	}
	ref := in.ManualConfirmation
	var refID *int64
	if in.BankStatementLineID > 0 {
		ref = fmt.Sprint(in.BankStatementLineID)
		refID = &in.BankStatementLineID
	}
	ids, err := s.singletonAccounts(ctx)
	if err != nil {
		return PostResult{}, err
	}
	req := businessRequest("topup", in.Amount, "topup:bankline:"+ref, "bankline", refID, in.ActorID,
		Posting{AccountID: ids["BANK_DEDICATED"], Side: "DEBIT", Amount: in.Amount},
		Posting{AccountID: in.WalletAccountID, Side: "CREDIT", Amount: in.Amount})
	return s.postBusiness(ctx, req, map[int64]string{ids["BANK_DEDICATED"]: "BANK_DEDICATED", in.WalletAccountID: "WALLET_AVAIL"}, nil)
}

type FreezeInput struct {
	RefType                       string
	RefID                         int64
	WalletAvailable, WalletFrozen int64
	Amount, ActorID               int64
}

func (s *Service) freeze(ctx context.Context, in FreezeInput) (PostResult, error) {
	if !keySegmentPattern.MatchString(in.RefType) || in.RefID <= 0 {
		return PostResult{}, ErrInvalidRequest
	}
	req := businessRequest("freeze", in.Amount, fmt.Sprintf("freeze:hold:%s-%d", in.RefType, in.RefID), in.RefType, &in.RefID, in.ActorID,
		Posting{AccountID: in.WalletAvailable, Side: "DEBIT", Amount: in.Amount},
		Posting{AccountID: in.WalletFrozen, Side: "CREDIT", Amount: in.Amount})
	return s.postBusiness(ctx, req, map[int64]string{in.WalletAvailable: "WALLET_AVAIL", in.WalletFrozen: "WALLET_FROZEN"}, nil, []int64{in.WalletAvailable, in.WalletFrozen})
}

type SettleInput struct {
	HoldID                      int64
	BuyerFrozen, PayeeAvailable int64
	Amount, Fee, ActorID        int64
}

func (s *Service) settle(ctx context.Context, in SettleInput) (PostResult, error) {
	if in.HoldID <= 0 || in.Fee < 0 || in.Fee > in.Amount {
		return PostResult{}, ErrInvalidRequest
	}
	ids, err := s.singletonAccounts(ctx)
	if err != nil {
		return PostResult{}, err
	}
	postings := []Posting{{AccountID: in.BuyerFrozen, Side: "DEBIT", Amount: in.Amount}}
	if payee := in.Amount - in.Fee; payee > 0 {
		postings = append(postings, Posting{AccountID: in.PayeeAvailable, Side: "CREDIT", Amount: payee})
	}
	expected := map[int64]string{in.BuyerFrozen: "WALLET_FROZEN"}
	if in.Amount > in.Fee {
		expected[in.PayeeAvailable] = "WALLET_AVAIL"
	}
	if in.Fee > 0 {
		postings = append(postings, Posting{AccountID: ids["PLATFORM_HELD"], Side: "CREDIT", Amount: in.Fee, Category: "settle_fee"})
		expected[ids["PLATFORM_HELD"]] = "PLATFORM_HELD"
	}
	req := businessRequest("settle", in.Amount, fmt.Sprintf("settle:hold:%d", in.HoldID), "hold", &in.HoldID, in.ActorID, postings...)
	return s.postBusiness(ctx, req, expected, nil)
}

type RefundFrozenInput struct {
	HoldID, WalletFrozen, WalletAvailable int64
	Amount, ActorID, PayerActorID         int64
}

func (s *Service) refundFrozen(ctx context.Context, in RefundFrozenInput) (PostResult, error) {
	if in.HoldID <= 0 || in.ActorID <= 0 || in.PayerActorID <= 0 {
		return PostResult{}, ErrInvalidRequest
	}
	if in.ActorID == in.PayerActorID {
		return PostResult{}, ErrSeparationDuty
	}
	req := businessRequest("refund", in.Amount, fmt.Sprintf("refund:hold:%d", in.HoldID), "hold", &in.HoldID, in.ActorID,
		Posting{AccountID: in.WalletFrozen, Side: "DEBIT", Amount: in.Amount},
		Posting{AccountID: in.WalletAvailable, Side: "CREDIT", Amount: in.Amount})
	req.Note = fmt.Sprintf("payer_actor:%d", in.PayerActorID)
	return s.postBusiness(ctx, req, map[int64]string{in.WalletFrozen: "WALLET_FROZEN", in.WalletAvailable: "WALLET_AVAIL"}, nil, []int64{in.WalletFrozen, in.WalletAvailable})
}

type WithdrawReqInput struct{ WithdrawalID, WalletAvailable, Amount, ActorID int64 }

func (s *Service) withdrawReq(ctx context.Context, in WithdrawReqInput) (PostResult, error) {
	ids, err := s.singletonAccounts(ctx)
	if err != nil {
		return PostResult{}, err
	}
	req := businessRequest("withdraw_req", in.Amount, fmt.Sprintf("withdraw_req:withdrawal:%d", in.WithdrawalID), "withdrawal", &in.WithdrawalID, in.ActorID,
		Posting{AccountID: in.WalletAvailable, Side: "DEBIT", Amount: in.Amount}, Posting{AccountID: ids["PENDING_PAYOUT"], Side: "CREDIT", Amount: in.Amount})
	return s.postBusiness(ctx, req, map[int64]string{in.WalletAvailable: "WALLET_AVAIL", ids["PENDING_PAYOUT"]: "PENDING_PAYOUT"}, nil)
}

type PayoutInput struct{ WithdrawalID, Amount, RequestedAmount, ActorID int64 }

func (s *Service) payoutLegacy(ctx context.Context, in PayoutInput) (PostResult, error) {
	if in.Amount != in.RequestedAmount {
		return PostResult{}, ErrInvalidAmount
	}
	return s.payout(ctx, "payout", in.WithdrawalID, in.Amount, in.ActorID, 0)
}

type PayoutFailInput struct{ WithdrawalID, WalletAvailable, Amount, ActorID int64 }

func (s *Service) payoutFailLegacy(ctx context.Context, in PayoutFailInput) (PostResult, error) {
	return s.payout(ctx, "payout_fail", in.WithdrawalID, in.Amount, in.ActorID, in.WalletAvailable)
}

func (s *Service) payout(ctx context.Context, typ string, withdrawalID, amount, actorID, wallet int64) (PostResult, error) {
	ids, err := s.singletonAccounts(ctx)
	if err != nil {
		return PostResult{}, err
	}
	destination, kind := ids["BANK_DEDICATED"], "BANK_DEDICATED"
	if typ == "payout_fail" {
		destination, kind = wallet, "WALLET_AVAIL"
	}
	req := businessRequest(typ, amount, fmt.Sprintf("%s:withdrawal:%d", typ, withdrawalID), "withdrawal", &withdrawalID, actorID,
		Posting{AccountID: ids["PENDING_PAYOUT"], Side: "DEBIT", Amount: amount}, Posting{AccountID: destination, Side: "CREDIT", Amount: amount})
	return s.postBusiness(ctx, req, map[int64]string{ids["PENDING_PAYOUT"]: "PENDING_PAYOUT", destination: kind}, nil)
}

type ChargeInput struct{ ID, WalletAccountID, Amount, ActorID int64 }

func (s *Service) StorageFee(ctx context.Context, in ChargeInput) (PostResult, error) {
	return s.walletToPlatform(ctx, "storage_fee", "bill", in, "storage")
}

func (s *Service) MemberFee(ctx context.Context, in ChargeInput, period string) (PostResult, error) {
	// Digital-goods payment rules are pending confirmation; do not expose this method to product flows yet.
	if !keySegmentPattern.MatchString(period) {
		return PostResult{}, ErrInvalidRequest
	}
	return s.walletToPlatformWithKey(ctx, "member_fee", "member", fmt.Sprintf("member_fee:member:%d:%s", in.ID, period), in, "member")
}

func (s *Service) walletToPlatform(ctx context.Context, typ, refType string, in ChargeInput, category string) (PostResult, error) {
	return s.walletToPlatformWithKey(ctx, typ, refType, fmt.Sprintf("%s:%s:%d", typ, refType, in.ID), in, category)
}

func (s *Service) walletToPlatformWithKey(ctx context.Context, typ, refType, key string, in ChargeInput, category string) (PostResult, error) {
	ids, err := s.singletonAccounts(ctx)
	if err != nil {
		return PostResult{}, err
	}
	req := businessRequest(typ, in.Amount, key, refType, &in.ID, in.ActorID,
		Posting{AccountID: in.WalletAccountID, Side: "DEBIT", Amount: in.Amount}, Posting{AccountID: ids["PLATFORM_HELD"], Side: "CREDIT", Amount: in.Amount, Category: category})
	return s.postBusiness(ctx, req, map[int64]string{in.WalletAccountID: "WALLET_AVAIL", ids["PLATFORM_HELD"]: "PLATFORM_HELD"}, nil)
}

func (s *Service) DeliveryHold(ctx context.Context, in ChargeInput) (PostResult, error) {
	ids, err := s.singletonAccounts(ctx)
	if err != nil {
		return PostResult{}, err
	}
	req := businessRequest("delivery_hold", in.Amount, fmt.Sprintf("delivery_hold:order:%d", in.ID), "order", &in.ID, in.ActorID,
		Posting{AccountID: in.WalletAccountID, Side: "DEBIT", Amount: in.Amount}, Posting{AccountID: ids["DELIVERY_ESCROW"], Side: "CREDIT", Amount: in.Amount})
	return s.postBusiness(ctx, req, map[int64]string{in.WalletAccountID: "WALLET_AVAIL", ids["DELIVERY_ESCROW"]: "DELIVERY_ESCROW"}, nil)
}

type AdTransferInput struct{ AdID, WalletAvailable, AdBudget, Amount, ActorID int64 }

func (s *Service) AdCharge(ctx context.Context, in AdTransferInput) (PostResult, error) {
	return s.adTransfer(ctx, "ad_charge", fmt.Sprintf("ad_charge:ad:%d", in.AdID), in.WalletAvailable, in.AdBudget, in)
}

func (s *Service) AdRefund(ctx context.Context, in AdTransferInput) (PostResult, error) {
	return s.adTransfer(ctx, "ad_refund", fmt.Sprintf("ad_refund:ad:%d", in.AdID), in.AdBudget, in.WalletAvailable, in)
}

func (s *Service) adTransfer(ctx context.Context, typ, key string, debit, credit int64, in AdTransferInput) (PostResult, error) {
	req := businessRequest(typ, in.Amount, key, "ad", &in.AdID, in.ActorID,
		Posting{AccountID: debit, Side: "DEBIT", Amount: in.Amount}, Posting{AccountID: credit, Side: "CREDIT", Amount: in.Amount})
	return s.postBusiness(ctx, req, map[int64]string{in.WalletAvailable: "WALLET_AVAIL", in.AdBudget: "AD_BUDGET"}, nil, []int64{in.WalletAvailable, in.AdBudget})
}

type AdSpendInput struct {
	AdID, AdBudget, Amount, ActorID int64
	Bucket                          string
}

func (s *Service) AdSpend(ctx context.Context, in AdSpendInput) (PostResult, error) {
	if !keySegmentPattern.MatchString(in.Bucket) {
		return PostResult{}, ErrInvalidRequest
	}
	ids, err := s.singletonAccounts(ctx)
	if err != nil {
		return PostResult{}, err
	}
	req := businessRequest("ad_spend", in.Amount, fmt.Sprintf("ad_spend:ad:%d:%s", in.AdID, in.Bucket), "ad", &in.AdID, in.ActorID,
		Posting{AccountID: in.AdBudget, Side: "DEBIT", Amount: in.Amount}, Posting{AccountID: ids["PLATFORM_HELD"], Side: "CREDIT", Amount: in.Amount, Category: "ads"})
	return s.postBusiness(ctx, req, map[int64]string{in.AdBudget: "AD_BUDGET", ids["PLATFORM_HELD"]: "PLATFORM_HELD"}, nil)
}

type DeliveryReleaseInput struct {
	OrderID, Amount, ActorID, DriverWallet int64
	DriverBP, PlatformBP, CarrierBP        int64
	Remainder                              int
	ProofID                                string
}

func (s *Service) DeliveryRelease(ctx context.Context, in DeliveryReleaseInput) (PostResult, error) {
	if in.OrderID <= 0 || strings.TrimSpace(in.ProofID) == "" || in.Remainder < 0 || in.Remainder > 2 {
		return PostResult{}, ErrInvalidRequest
	}
	amounts, err := Allocate(in.Amount, []Share{{BasisPoints: in.DriverBP, Remainder: in.Remainder == 0}, {BasisPoints: in.PlatformBP, Remainder: in.Remainder == 1}, {BasisPoints: in.CarrierBP, Remainder: in.Remainder == 2}})
	if err != nil {
		return PostResult{}, err
	}
	ids, err := s.singletonAccounts(ctx)
	if err != nil {
		return PostResult{}, err
	}
	postings := []Posting{{AccountID: ids["DELIVERY_ESCROW"], Side: "DEBIT", Amount: in.Amount}}
	expected := map[int64]string{ids["DELIVERY_ESCROW"]: "DELIVERY_ESCROW"}
	if amounts[0] > 0 {
		postings = append(postings, Posting{AccountID: in.DriverWallet, Side: "CREDIT", Amount: amounts[0], Role: "driver"})
		expected[in.DriverWallet] = "WALLET_AVAIL"
	}
	if amounts[1] > 0 {
		postings = append(postings, Posting{AccountID: ids["PLATFORM_HELD"], Side: "CREDIT", Amount: amounts[1], Category: "delivery_margin"})
		expected[ids["PLATFORM_HELD"]] = "PLATFORM_HELD"
	}
	if amounts[2] > 0 {
		postings = append(postings, Posting{AccountID: ids["PAYABLE_CARRIER"], Side: "CREDIT", Amount: amounts[2], Role: "carrier"})
		expected[ids["PAYABLE_CARRIER"]] = "PAYABLE_CARRIER"
	}
	req := businessRequest("delivery_release", in.Amount, fmt.Sprintf("delivery_release:order:%d", in.OrderID), "order", &in.OrderID, in.ActorID, postings...)
	req.Note = "delivery_proof:" + in.ProofID
	return s.postBusiness(ctx, req, expected, escrowCheck(ids["DELIVERY_ESCROW"], in.OrderID, in.Amount, false, nil))
}

type DeliveryRefundInput struct{ OrderID, MerchantWallet, Amount, ActorID int64 }

func (s *Service) DeliveryRefund(ctx context.Context, in DeliveryRefundInput) (PostResult, error) {
	ids, err := s.singletonAccounts(ctx)
	if err != nil {
		return PostResult{}, err
	}
	req := businessRequest("delivery_refund", in.Amount, fmt.Sprintf("delivery_refund:order:%d", in.OrderID), "order", &in.OrderID, in.ActorID,
		Posting{AccountID: ids["DELIVERY_ESCROW"], Side: "DEBIT", Amount: in.Amount}, Posting{AccountID: in.MerchantWallet, Side: "CREDIT", Amount: in.Amount})
	return s.postBusiness(ctx, req, map[int64]string{ids["DELIVERY_ESCROW"]: "DELIVERY_ESCROW", in.MerchantWallet: "WALLET_AVAIL"}, escrowCheck(ids["DELIVERY_ESCROW"], in.OrderID, in.Amount, true, &in.MerchantWallet))
}

func escrowCheck(accountID, orderID, amount int64, rejectReleased bool, refundWallet *int64) transactionCheck {
	return func(ctx context.Context, tx pgx.Tx) error {
		var holdWallet int64
		if err := tx.QueryRow(ctx, `
			SELECT p.account_id
			FROM ledger.txn t JOIN ledger.posting p ON p.txn_id=t.id
			JOIN ledger.account a ON a.id=p.account_id
			WHERE t.type='delivery_hold' AND t.ref_type='order' AND t.ref_id=$1
			  AND p.side='DEBIT' AND a.kind='WALLET_AVAIL'
			ORDER BY t.id LIMIT 1`, orderID).Scan(&holdWallet); errors.Is(err, pgx.ErrNoRows) {
			return ErrAccountOwnerMismatch
		} else if err != nil {
			return err
		}
		if refundWallet != nil && holdWallet != *refundWallet {
			return ErrAccountOwnerMismatch
		}
		if rejectReleased {
			var released bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ledger.txn WHERE type='delivery_release' AND ref_type='order' AND ref_id=$1)`, orderID).Scan(&released); err != nil {
				return err
			}
			if released {
				return ErrEscrowExceeded
			}
		}
		var available int64
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(sum(CASE p.side WHEN 'CREDIT' THEN p.amount ELSE -p.amount END),0)
			FROM ledger.posting p JOIN ledger.txn t ON t.id=p.txn_id
			WHERE p.account_id=$1 AND t.ref_type='order' AND t.ref_id=$2`, accountID, orderID).Scan(&available); err != nil {
			return err
		}
		if amount > available {
			return ErrEscrowExceeded
		}
		return nil
	}
}

type ReviewedPaymentInput struct{ ID, Amount, ActorID, ReviewerActorID int64 }

func (s *Service) Sweep(ctx context.Context, in ReviewedPaymentInput) (PostResult, error) {
	return s.reviewedPayment(ctx, "sweep", "sweep", in)
}

func (s *Service) CarrierPay(ctx context.Context, in ReviewedPaymentInput) (PostResult, error) {
	return s.reviewedPayment(ctx, "carrier_pay", "statement", in)
}

func (s *Service) reviewedPayment(ctx context.Context, typ, refType string, in ReviewedPaymentInput) (PostResult, error) {
	if in.ActorID <= 0 || in.ReviewerActorID <= 0 || in.ActorID == in.ReviewerActorID {
		return PostResult{}, ErrSeparationDuty
	}
	ids, err := s.singletonAccounts(ctx)
	if err != nil {
		return PostResult{}, err
	}
	source, kind := ids["PLATFORM_HELD"], "PLATFORM_HELD"
	if typ == "carrier_pay" {
		source, kind = ids["PAYABLE_CARRIER"], "PAYABLE_CARRIER"
	}
	req := businessRequest(typ, in.Amount, fmt.Sprintf("%s:%s:%d", typ, refType, in.ID), refType, &in.ID, in.ActorID,
		Posting{AccountID: source, Side: "DEBIT", Amount: in.Amount}, Posting{AccountID: ids["BANK_DEDICATED"], Side: "CREDIT", Amount: in.Amount})
	req.Note = fmt.Sprintf("reviewer_actor:%d", in.ReviewerActorID)
	return s.postBusiness(ctx, req, map[int64]string{source: kind, ids["BANK_DEDICATED"]: "BANK_DEDICATED"}, nil)
}

type BankDiffInput struct {
	BankLineID, Amount, ActorID int64
	Direction, Reason           string
}

func (s *Service) BankDiff(ctx context.Context, in BankDiffInput) (PostResult, error) {
	if in.BankLineID <= 0 || in.Direction != "IN" && in.Direction != "OUT" || strings.TrimSpace(in.Reason) == "" {
		return PostResult{}, ErrInvalidRequest
	}
	ids, err := s.singletonAccounts(ctx)
	if err != nil {
		return PostResult{}, err
	}
	debit, credit := ids["BANK_DEDICATED"], ids["SUSPENSE"]
	if in.Direction == "OUT" {
		debit, credit = credit, debit
	}
	req := businessRequest("bank_diff", in.Amount, fmt.Sprintf("bank_diff:bankline:%d", in.BankLineID), "bankline", &in.BankLineID, in.ActorID,
		Posting{AccountID: debit, Side: "DEBIT", Amount: in.Amount}, Posting{AccountID: credit, Side: "CREDIT", Amount: in.Amount})
	req.Note = in.Reason
	return s.postBusiness(ctx, req, map[int64]string{ids["BANK_DEDICATED"]: "BANK_DEDICATED", ids["SUSPENSE"]: "SUSPENSE"}, nil)
}

func businessRequest(typ string, amount int64, key, refType string, refID *int64, actorID int64, postings ...Posting) PostRequest {
	return PostRequest{Type: typ, Amount: amount, IdemKey: key, RefType: refType, RefID: refID, ActorID: actorID, Postings: postings}
}

func (s *Service) postBusiness(ctx context.Context, request PostRequest, kinds map[int64]string, extra transactionCheck, sameOwner ...[]int64) (PostResult, error) {
	if request.RefID != nil && *request.RefID <= 0 {
		return PostResult{}, ErrInvalidRequest
	}
	return s.post(ctx, request, requireKinds(kinds, extra, sameOwner...))
}
