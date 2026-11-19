package iam

import (
	"context"
	"strings"
	"testing"
)

func TestLast4DoesNotExposeMoreThanSuffix(t *testing.T) {
	for _, test := range []struct {
		name, input, want string
	}{
		{"short", "abc", "abc"},
		{"exact", "abcd", "abcd"},
		{"long", "bot-token-1234", "1234"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := last4(test.input); got != test.want {
				t.Fatalf("last4(%q)=%q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestBootstrapRejectsInvalidInputsBeforeDatabaseUse(t *testing.T) {
	var service *BootstrapService
	if _, err := service.CreateFirstSuper(context.Background(), "name", "owner@example.test"); err == nil || err.Error() != "iam: invalid bootstrap configuration" {
		t.Fatalf("CreateFirstSuper error=%v", err)
	}
	if err := service.RegisterBot(context.Background(), BootstrapBotInput{End: "buyer", Username: "bot", Actor: "actor", Token: "test-token"}); err == nil || err.Error() != "iam: invalid bot bootstrap input" {
		t.Fatalf("RegisterBot error=%v", err)
	}
}

func TestNormalizeAdminEmail(t *testing.T) {
	for _, test := range []struct {
		name, input, want string
	}{
		{"trim and lowercase", "  Owner@Example.TEST ", "owner@example.test"},
		{"single at", "owner@example.test", "owner@example.test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeAdminEmail(test.input)
			if err != nil || got != test.want {
				t.Fatalf("normalizeAdminEmail(%q)=%q, err=%v", test.input, got, err)
			}
		})
	}
	for _, input := range []string{"", "owner@@example.test", "owner@example\n.test", strings.Repeat("a", 321) + "@x"} {
		if _, err := normalizeAdminEmail(input); err == nil {
			t.Fatalf("normalizeAdminEmail(%q) accepted invalid email", input)
		}
	}
}
