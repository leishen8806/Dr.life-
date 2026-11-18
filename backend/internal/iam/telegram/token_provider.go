package telegram

import (
	"context"
	"errors"
	"strconv"
)

// PoolTokenProvider adapts the encrypted Bot pool to outbound Telegram
// clients. Tokens are revealed only for the duration of one lookup and are
// never retained by this adapter.
type PoolTokenProvider struct {
	Source PoolSource
	End    End
}

// AnyPoolTokenProvider resolves a bot ID against the configured end pools.
// The worker only receives the selected bot ID from the notification plan, so
// trying each pool does not weaken recipient-end selection and keeps encrypted
// token handling in the IAM adapter.
type AnyPoolTokenProvider struct {
	Source PoolSource
}

func (p AnyPoolTokenProvider) Token(ctx context.Context, botID string) (string, error) {
	var lastErr error
	for _, end := range []End{Buyer, Merchant} {
		token, err := (PoolTokenProvider{Source: p.Source, End: end}).Token(ctx, botID)
		if err == nil {
			return token, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("telegram: bot not found")
	}
	return "", lastErr
}

func (p PoolTokenProvider) Token(ctx context.Context, botID string) (string, error) {
	if p.Source == nil || !p.End.Valid() {
		return "", errors.New("telegram: bot pool is unavailable")
	}
	id, err := strconv.ParseInt(botID, 10, 64)
	if err != nil || id <= 0 {
		return "", errors.New("telegram: invalid bot id")
	}
	bots, err := p.Source.Bots(ctx, p.End)
	if err != nil {
		return "", err
	}
	defer destroyBots(bots)
	for _, bot := range bots {
		if bot.ID != id {
			continue
		}
		plain := bot.Token.Reveal()
		defer wipe(plain)
		if len(plain) == 0 {
			return "", errors.New("telegram: bot token unavailable")
		}
		return string(plain), nil
	}
	return "", errors.New("telegram: bot not found")
}
