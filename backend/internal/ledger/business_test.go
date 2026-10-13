package ledger

import (
	"context"
	"errors"
	"testing"
)

func TestRefundFrozenValidatesSeparationActors(t *testing.T) {
	service := &Service{}
	valid := RefundFrozenInput{HoldID: 1, ActorID: 2, PayerActorID: 3}
	for name, mutate := range map[string]func(*RefundFrozenInput){
		"zero hold":  func(in *RefundFrozenInput) { in.HoldID = 0 },
		"zero actor": func(in *RefundFrozenInput) { in.ActorID = 0 },
		"zero payer": func(in *RefundFrozenInput) { in.PayerActorID = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if _, err := service.RefundFrozen(context.Background(), input); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("error=%v, want ErrInvalidRequest", err)
			}
		})
	}

	input := valid
	input.PayerActorID = input.ActorID
	if _, err := service.RefundFrozen(context.Background(), input); !errors.Is(err, ErrSeparationDuty) {
		t.Fatalf("same actor error=%v, want ErrSeparationDuty", err)
	}
}
