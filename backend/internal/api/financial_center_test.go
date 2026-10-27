package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/api/gen"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
)

type financialCenterStub struct {
	filter ledger.FinancialCenterFilter
}

func (*financialCenterStub) ListAdminTransactions(context.Context, int) ([]ledger.AdminTransaction, error) {
	return nil, nil
}
func (*financialCenterStub) GetAdminTransaction(context.Context, string) (ledger.AdminTransaction, bool, error) {
	return ledger.AdminTransaction{}, false, nil
}
func (*financialCenterStub) ListAccountReconciliations(context.Context) ([]ledger.AdminAccountReconciliation, error) {
	return nil, nil
}
func (*financialCenterStub) ListAccountPostings(context.Context, int64, int) ([]ledger.AdminPosting, error) {
	return nil, nil
}
func (*financialCenterStub) ListWalletTransactions(context.Context, ledger.WalletTransactionQuery) (ledger.WalletTransactionPage, error) {
	return ledger.WalletTransactionPage{}, nil
}
func (*financialCenterStub) GetWalletTransaction(context.Context, int64, string) (ledger.WalletTransaction, bool, error) {
	return ledger.WalletTransaction{}, false, nil
}

func (s *financialCenterStub) FinancialCenter(_ context.Context, filter ledger.FinancialCenterFilter) (ledger.FinancialCenterReport, error) {
	s.filter = filter
	return ledger.FinancialCenterReport{Revenue: []ledger.RevenueByCategory{}}, nil
}

func TestGetAdminFinancialCenterRejectsUnauthenticatedRequest(t *testing.T) {
	stub := &financialCenterStub{}
	handler := Handler{Ledger: stub}
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	limit := 25
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/financial-center", nil)
	response := httptest.NewRecorder()
	handler.GetAdminFinancialCenter(response, req, gen.GetAdminFinancialCenterParams{From: &from, To: &to, MerchantLimit: &limit})
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !stub.filter.From.IsZero() || !stub.filter.To.IsZero() || stub.filter.MerchantLimit != 0 {
		t.Fatalf("unauthenticated request called reader: filter=%+v", stub.filter)
	}
}
