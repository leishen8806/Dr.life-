package iam

import (
	"errors"
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

func TestSetAccountStatusRejectsInvalidInput(t *testing.T) {
	service := &AccountStatusService{}
	for _, input := range []struct {
		name, to, reason string
		actor            StatusActor
	}{
		{"blank reason", "frozen", " \t", StatusActor{Type: "admin", ID: "admin-1"}},
		{"invalid status", "pending", "reason", StatusActor{Type: "admin", ID: "admin-1"}},
		{"invalid actor", "frozen", "reason", StatusActor{Type: "buyer", ID: "buyer-1"}},
	} {
		t.Run(input.name, func(t *testing.T) {
			_, err := service.SetAccountStatus(t.Context(), "account-1", input.to, input.reason, input.actor)
			var appErr *httpx.AppError
			if !errors.As(err, &appErr) || appErr.Code != httpx.CodeInvalidArgument {
				t.Fatalf("error=%v, want validation AppError", err)
			}
		})
	}
}
