package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
)

var ErrBotTokenUnavailable = errors.New("telegram bot token unavailable")

type botTokenQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// DBBotTokenProvider is the IAM-owned bridge from an approved bot ID to its
// envelope-encrypted token. It deliberately does not select a user-facing pool.
type DBBotTokenProvider struct {
	DB       botTokenQueryer
	Envelope *secrets.Envelope
}

func (p DBBotTokenProvider) Token(ctx context.Context, rawID string) (string, error) {
	if p.DB == nil || p.Envelope == nil {
		return "", ErrBotTokenUnavailable
	}
	id, err := strconv.ParseInt(strings.TrimSpace(rawID), 10, 64)
	if err != nil || id <= 0 {
		return "", ErrBotTokenUnavailable
	}
	var encrypted []byte
	if err := p.DB.QueryRow(ctx, `SELECT token_enc FROM iam.bot WHERE id=$1 AND status='active'`, id).Scan(&encrypted); err != nil {
		return "", ErrBotTokenUnavailable
	}
	plain, err := p.Envelope.Decrypt(ctx, encrypted, fmt.Sprintf("bot_token:%d", id))
	if err != nil {
		return "", ErrBotTokenUnavailable
	}
	defer wipe(plain)
	return string(plain), nil
}
