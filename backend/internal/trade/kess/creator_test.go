package kess

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/leishen8806/Dr.life-/backend/internal/trade"
)

func TestPaymentCreateErrorMapsDatabaseGuards(t *testing.T) {
	for _, test := range []struct {
		name       string
		pgErr      *pgconn.PgError
		want       error
		wantMapped bool
	}{
		{"released", &pgconn.PgError{Code: "TR001"}, trade.ErrReservationReleased, true},
		{"amount", &pgconn.PgError{Code: "TR002"}, trade.ErrPaymentAmount, true},
		{"active intent", &pgconn.PgError{Code: "23505", ConstraintName: "ux_kess_payment_intent_one_active_order"}, trade.ErrPaymentConfirming, true},
		{"other unique", &pgconn.PgError{Code: "23505", ConstraintName: "other"}, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := paymentCreateError(test.pgErr)
			if test.wantMapped && !errors.Is(got, test.want) {
				t.Fatalf("error=%v, want %v", got, test.want)
			}
			if !test.wantMapped && got != test.pgErr {
				t.Fatalf("error=%v, want original PostgreSQL error", got)
			}
		})
	}
}
