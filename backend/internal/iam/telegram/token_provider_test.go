package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
)

type tokenProviderSource struct {
	bots []Bot
	err  error
}

func (s tokenProviderSource) Bots(context.Context, End) ([]Bot, error) { return s.bots, s.err }
func (tokenProviderSource) Invalidate(End)                             {}

func TestPoolTokenProviderReturnsOnlyRequestedBotToken(t *testing.T) {
	provider := PoolTokenProvider{
		Source: tokenProviderSource{bots: []Bot{{ID: 7, Token: secrets.NewSecret([]byte("buyer-token"))}, {ID: 8, Token: secrets.NewSecret([]byte("other-token"))}}},
		End:    Buyer,
	}
	token, err := provider.Token(context.Background(), "7")
	if err != nil || token != "buyer-token" {
		t.Fatalf("token=%q err=%v, want requested bot token", token, err)
	}
}

func TestPoolTokenProviderRejectsInvalidOrMissingBot(t *testing.T) {
	provider := PoolTokenProvider{Source: tokenProviderSource{}, End: Buyer}
	for _, id := range []string{"", "not-a-number", "8"} {
		if _, err := provider.Token(context.Background(), id); err == nil {
			t.Fatalf("Token(%q) returned nil error", id)
		}
	}
	if _, err := (PoolTokenProvider{Source: tokenProviderSource{err: errors.New("db unavailable")}, End: Buyer}).Token(context.Background(), "7"); err == nil {
		t.Fatal("database error was swallowed")
	}
}
