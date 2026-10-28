package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
)

func TestBotTokenProviderReadsOnlyRequestedBotAndDoesNotExposeTokenInErrors(t *testing.T) {
	envelope := testProviderEnvelope(t)
	const token = "token-must-not-appear-in-errors"
	ciphertext, err := envelope.Encrypt(context.Background(), []byte(token), "bot_token:55")
	if err != nil {
		t.Fatal(err)
	}
	provider := DBBotTokenProvider{DB: tokenQueryer{ciphertext: ciphertext}, Envelope: envelope}
	got, err := provider.Token(context.Background(), "55")
	if err != nil || got != token {
		t.Fatalf("Token() = %q, %v", got, err)
	}
	_, err = provider.Token(context.Background(), "not-an-id")
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("invalid ID error leaks token: %v", err)
	}
	_, err = (DBBotTokenProvider{DB: tokenQueryer{err: errors.New("db unavailable")}, Envelope: envelope}).Token(context.Background(), "55")
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("database error leaks token: %v", err)
	}
}

type tokenQueryer struct {
	ciphertext []byte
	err        error
}

func (q tokenQueryer) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return tokenRow{ciphertext: q.ciphertext, err: q.err}
}

type tokenRow struct {
	ciphertext []byte
	err        error
}

func (r tokenRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*[]byte) = append([]byte(nil), r.ciphertext...)
	return nil
}

func testProviderEnvelope(t *testing.T) *secrets.Envelope {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	ring, err := secrets.NewKeyring(map[string][]byte{"current": key}, "current")
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := secrets.NewEnvelope(ring)
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}
