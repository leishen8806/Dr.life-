package ledger

import (
	"context"
	"time"
)

// FinancialCenterFilter narrows ledger-derived finance reporting to an
// occurred-at interval. A zero From and To means all time; otherwise the
// interval is [From, To). Amounts are USD e4 integers.
type FinancialCenterFilter struct {
	From, To      time.Time
	MerchantLimit int
}

type RevenueByCategory struct {
	Category string `json:"category"`
	Amount   int64  `json:"amount_e4"`
}

type FundsPosition struct {
	UserAvailable  int64 `json:"user_available_e4"`
	Frozen         int64 `json:"frozen_e4"`
	PendingPayout  int64 `json:"pending_payout_e4"`
	DeliveryEscrow int64 `json:"delivery_escrow_e4"`
	AdBudget       int64 `json:"ad_budget_e4"`
	PlatformHeld   int64 `json:"platform_held_e4"`
	PayableCarrier int64 `json:"payable_carrier_e4"`
	Suspense       int64 `json:"suspense_e4"`
	InternalTotal  int64 `json:"internal_total_e4"`
	BankDedicated  int64 `json:"bank_dedicated_e4"`
	MatchesBank    bool  `json:"matches_bank"`
}

type WithdrawalSummary struct {
	PendingCount  int64 `json:"pending_count"`
	PendingAmount int64 `json:"pending_amount_e4"`
	PaidCount     int64 `json:"paid_count"`
	PaidAmount    int64 `json:"paid_amount_e4"`
}

type MerchantBalance struct {
	MerchantID int64 `json:"merchant_id"`
	Available  int64 `json:"available_e4"`
}

type FinancialCenterReport struct {
	Revenue          []RevenueByCategory `json:"revenue"`
	RevenueTotal     int64               `json:"revenue_total_e4"`
	Position         FundsPosition       `json:"position"`
	Withdrawals      WithdrawalSummary   `json:"withdrawals"`
	MerchantBalances []MerchantBalance   `json:"merchant_balances"`
}

var financialCategories = []string{"storage", "delivery_margin", "ads", "settle_fee", "member"}

// FinancialCenter is a read-only ledger report. It intentionally reads only
// the ledger schema so the report can run through the reporting replica.
func (s *ReadService) FinancialCenter(ctx context.Context, filter FinancialCenterFilter) (FinancialCenterReport, error) {
	if filter.From.IsZero() != filter.To.IsZero() || !filter.From.IsZero() && !filter.From.Before(filter.To) {
		return FinancialCenterReport{}, ErrInvalidRequest
	}
	limit := filter.MerchantLimit
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 100 {
		return FinancialCenterReport{}, ErrInvalidRequest
	}
	report := FinancialCenterReport{Revenue: make([]RevenueByCategory, 0, len(financialCategories))}
	if err := s.financialRevenue(ctx, filter, &report); err != nil {
		return FinancialCenterReport{}, err
	}
	if err := s.financialPosition(ctx, &report.Position); err != nil {
		return FinancialCenterReport{}, err
	}
	if err := s.financialWithdrawals(ctx, filter, &report.Withdrawals); err != nil {
		return FinancialCenterReport{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT a.merchant_id,b.balance
		FROM ledger.account a JOIN ledger.balance b ON b.account_id=a.id
		WHERE a.kind='WALLET_AVAIL' AND a.merchant_id IS NOT NULL
		ORDER BY b.balance DESC,a.merchant_id ASC LIMIT $1`, limit)
	if err != nil {
		return FinancialCenterReport{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item MerchantBalance
		if err := rows.Scan(&item.MerchantID, &item.Available); err != nil {
			return FinancialCenterReport{}, err
		}
		report.MerchantBalances = append(report.MerchantBalances, item)
	}
	return report, rows.Err()
}

func (s *ReadService) financialRevenue(ctx context.Context, filter FinancialCenterFilter, report *FinancialCenterReport) error {
	amounts := make(map[string]int64, len(financialCategories))
	query := `SELECT p.category,COALESCE(sum(CASE p.side WHEN 'CREDIT' THEN p.amount ELSE -p.amount END),0)
		FROM ledger.posting p JOIN ledger.txn t ON t.id=p.txn_id
		JOIN ledger.account a ON a.id=p.account_id
		WHERE a.kind='PLATFORM_HELD' AND p.category IS NOT NULL`
	args := []any{}
	if !filter.From.IsZero() {
		query += ` AND t.occurred_at >= $1 AND t.occurred_at < $2`
		args = append(args, filter.From, filter.To)
	}
	query += ` GROUP BY p.category`
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var category string
		var amount int64
		if err := rows.Scan(&category, &amount); err != nil {
			return err
		}
		amounts[category] = amount
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, category := range financialCategories {
		amount := amounts[category]
		report.Revenue = append(report.Revenue, RevenueByCategory{Category: category, Amount: amount})
		report.RevenueTotal += amount
	}
	return nil
}

func (s *ReadService) financialPosition(ctx context.Context, position *FundsPosition) error {
	err := s.db.QueryRow(ctx, `
		SELECT
			COALESCE(sum(b.balance) FILTER (WHERE a.kind='WALLET_AVAIL'),0),
			COALESCE(sum(b.balance) FILTER (WHERE a.kind='WALLET_FROZEN'),0),
			COALESCE(sum(b.balance) FILTER (WHERE a.kind='PENDING_PAYOUT'),0),
			COALESCE(sum(b.balance) FILTER (WHERE a.kind='DELIVERY_ESCROW'),0),
			COALESCE(sum(b.balance) FILTER (WHERE a.kind='AD_BUDGET'),0),
			COALESCE(sum(b.balance) FILTER (WHERE a.kind='PLATFORM_HELD'),0),
			COALESCE(sum(b.balance) FILTER (WHERE a.kind='PAYABLE_CARRIER'),0),
			COALESCE(sum(b.balance) FILTER (WHERE a.kind='SUSPENSE'),0),
			COALESCE(sum(b.balance) FILTER (WHERE a.kind <> 'BANK_DEDICATED'),0),
			COALESCE(sum(b.balance) FILTER (WHERE a.kind='BANK_DEDICATED'),0)
		FROM ledger.account a JOIN ledger.balance b ON b.account_id=a.id`).Scan(
		&position.UserAvailable, &position.Frozen, &position.PendingPayout, &position.DeliveryEscrow,
		&position.AdBudget, &position.PlatformHeld, &position.PayableCarrier, &position.Suspense,
		&position.InternalTotal, &position.BankDedicated,
	)
	if err == nil {
		position.MatchesBank = position.InternalTotal == position.BankDedicated
	}
	return err
}

func (s *ReadService) financialWithdrawals(ctx context.Context, filter FinancialCenterFilter, summary *WithdrawalSummary) error {
	query := `SELECT
		count(*) FILTER (WHERE status IN ('requested','review','approved')),
		COALESCE(sum(amount) FILTER (WHERE status IN ('requested','review','approved')),0),
		count(*) FILTER (WHERE status='paid'),
		COALESCE(sum(amount) FILTER (WHERE status='paid'),0)
		FROM ledger.withdrawal`
	args := []any{}
	if !filter.From.IsZero() {
		query += ` WHERE requested_at >= $1 AND requested_at < $2`
		args = append(args, filter.From, filter.To)
	}
	return s.db.QueryRow(ctx, query, args...).Scan(&summary.PendingCount, &summary.PendingAmount, &summary.PaidCount, &summary.PaidAmount)
}
