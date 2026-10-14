package observability

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

type Business struct {
	orders         *prometheus.CounterVec
	withdrawals    *prometheus.CounterVec
	ai             *prometheus.CounterVec
	bots           *prometheus.CounterVec
	loginFailures  *prometheus.CounterVec
	ledger         *prometheus.CounterVec
	reconciliation prometheus.Counter
}

func newBusiness(registry *prometheus.Registry) *Business {
	b := &Business{
		orders:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "drlife_orders_total", Help: "Order lifecycle events."}, []string{"event"}),
		withdrawals:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "drlife_withdrawals_total", Help: "Withdrawal lifecycle events."}, []string{"event"}),
		ai:             prometheus.NewCounterVec(prometheus.CounterOpts{Name: "drlife_ai_requests_total", Help: "AI requests and fallback events."}, []string{"event", "level"}),
		bots:           prometheus.NewCounterVec(prometheus.CounterOpts{Name: "drlife_bot_sends_total", Help: "Bot send results."}, []string{"endpoint", "bot", "status", "reason"}),
		loginFailures:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "drlife_login_validation_failures_total", Help: "Login validation failures."}, []string{"endpoint", "reason"}),
		ledger:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "drlife_ledger_posts_total", Help: "Ledger posting results."}, []string{"status", "reason"}),
		reconciliation: prometheus.NewCounter(prometheus.CounterOpts{Name: "drlife_ledger_reconciliation_mismatches_total", Help: "Ledger reconciliation mismatches."}),
	}
	registry.MustRegister(b.orders, b.withdrawals, b.ai, b.bots, b.loginFailures, b.ledger, b.reconciliation)
	return b
}

func (b *Business) OrderCreated()        { b.orders.WithLabelValues("created").Inc() }
func (b *Business) OrderSigned()         { b.orders.WithLabelValues("signed").Inc() }
func (b *Business) WithdrawalRequested() { b.withdrawals.WithLabelValues("requested").Inc() }
func (b *Business) WithdrawalReviewed()  { b.withdrawals.WithLabelValues("reviewed").Inc() }
func (b *Business) WithdrawalPaid()      { b.withdrawals.WithLabelValues("paid").Inc() }
func (b *Business) WithdrawalFailed()    { b.withdrawals.WithLabelValues("failed").Inc() }
func (b *Business) WithdrawalCancelled() { b.withdrawals.WithLabelValues("cancelled").Inc() }
func (b *Business) WithdrawalOverdue(count int) {
	if count > 0 {
		b.withdrawals.WithLabelValues("overdue").Add(float64(count))
	}
}
func (b *Business) AIRequested() { b.ai.WithLabelValues("request", "none").Inc() }
func (b *Business) AIFallback(level string) {
	b.ai.WithLabelValues("fallback", bounded(level, "primary", "secondary", "local")).Inc()
}
func (b *Business) LoginValidationFailed(endpoint, reason string) {
	b.loginFailures.WithLabelValues(bounded(endpoint, "buyer", "merchant", "driver"), bounded(reason, "expired", "invalid_signature", "malformed", "account_blocked", "rate_limited", "unknown_bot")).Inc()
}
func (b *Business) BotSent(endpoint string, bot int, success bool, reason string) {
	status := "success"
	if success {
		reason = "none"
	} else {
		status = "failure"
		reason = bounded(reason, "rate_limited", "network", "telegram", "invalid_request")
	}
	botLabel := "other"
	if bot >= 0 && bot <= 2 {
		botLabel = strconv.Itoa(bot)
	}
	b.bots.WithLabelValues(bounded(endpoint, "buyer", "merchant", "driver"), botLabel, status, reason).Inc()
}
func (b *Business) LedgerPosted() { b.ledger.WithLabelValues("success", "none").Inc() }
func (b *Business) LedgerFailed(reason string) {
	b.ledger.WithLabelValues("failure", bounded(reason, "insufficient_funds", "idempotency_conflict", "unbalanced", "database")).Inc()
}
func (b *Business) ReconciliationMismatch() { b.reconciliation.Inc() }

func bounded(value string, allowed ...string) string {
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return "other"
}
