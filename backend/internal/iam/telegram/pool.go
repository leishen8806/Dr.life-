package telegram

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
)

type End string

const (
	Buyer    End = "buyer"
	Merchant End = "merchant"
	Driver   End = "driver"
)

func (e End) Valid() bool { return e == Buyer || e == Merchant || e == Driver }

type Bot struct {
	ID         int64
	RoleInPool string
	Priority   int
	Token      secrets.Secret
}

type PoolSource interface {
	Bots(context.Context, End) ([]Bot, error)
	Invalidate(End)
}

type PoolResult struct {
	Data         ValidatedData
	PrimaryBotID *int64
	ValidByBotID *int64
	TriedCount   int
}

type PoolVerifier struct {
	Validator Validator
	Source    PoolSource
}

func (p PoolVerifier) Verify(ctx context.Context, end End, raw string) (PoolResult, error) {
	if !end.Valid() || p.Source == nil {
		return PoolResult{}, ErrInvalidInitData
	}
	bots, err := p.Source.Bots(ctx, end)
	if err != nil || len(bots) == 0 {
		return PoolResult{}, ErrInvalidInitData
	}
	defer destroyBots(bots)
	result := PoolResult{}
	var validationFailure error
	for i := range bots {
		if bots[i].RoleInPool == "primary" && result.PrimaryBotID == nil {
			id := bots[i].ID
			result.PrimaryBotID = &id
		}
		token := bots[i].Token.Reveal()
		data, validationErr := p.Validator.Validate(raw, token)
		wipe(token)
		result.TriedCount = i + 1
		if validationErr == nil {
			id := bots[i].ID
			result.ValidByBotID = &id
			result.Data = data
			return result, nil
		}
		if validationFailure == nil || FailureOf(validationErr) == FailureExpired || (FailureOf(validationErr) == FailureMalformed && FailureOf(validationFailure) == FailureInvalidSignature) {
			validationFailure = validationErr
		}
	}
	if validationFailure == nil {
		validationFailure = &ValidationError{Failure: FailureInvalidSignature}
	}
	return result, validationFailure
}

func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

type DBPoolSource struct {
	DB       *pgxpool.Pool
	Envelope *secrets.Envelope
	TTL      time.Duration
	Now      func() time.Time
	mu       sync.Mutex
	cache    map[End]cacheEntry
}

type cacheEntry struct {
	bots    []Bot
	expires time.Time
}

func (s *DBPoolSource) Bots(ctx context.Context, end End) ([]Bot, error) {
	if s.DB == nil || s.Envelope == nil || !end.Valid() {
		return nil, ErrInvalidInitData
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	s.mu.Lock()
	if entry, ok := s.cache[end]; ok && now.Before(entry.expires) {
		bots := cloneBots(entry.bots)
		s.mu.Unlock()
		return bots, nil
	}
	s.mu.Unlock()
	rows, err := s.DB.Query(ctx, `SELECT id, role_in_pool, priority, token_enc FROM iam.bot WHERE "end"=$1 ORDER BY CASE role_in_pool WHEN 'primary' THEN 0 ELSE 1 END, priority, id`, string(end))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var bots []Bot
	for rows.Next() {
		var bot Bot
		var encrypted []byte
		if err := rows.Scan(&bot.ID, &bot.RoleInPool, &bot.Priority, &encrypted); err != nil {
			destroyBots(bots)
			return nil, err
		}
		plain, err := s.Envelope.Decrypt(ctx, encrypted, fmt.Sprintf("bot_token:%d", bot.ID))
		if err != nil {
			destroyBots(bots)
			return nil, err
		}
		bot.Token = secrets.NewSecret(plain)
		wipe(plain)
		bots = append(bots, bot)
	}
	if err := rows.Err(); err != nil {
		destroyBots(bots)
		return nil, err
	}
	ttl := s.TTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	s.mu.Lock()
	if s.cache == nil {
		s.cache = make(map[End]cacheEntry)
	}
	if old, ok := s.cache[end]; ok {
		destroyBots(old.bots)
	}
	s.cache[end] = cacheEntry{bots: cloneBots(bots), expires: now.Add(ttl)}
	s.mu.Unlock()
	return bots, nil
}

// Invalidate must be called after a bot token or pool state changes.
func (s *DBPoolSource) Invalidate(end End) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.cache[end]; ok {
		destroyBots(entry.bots)
		delete(s.cache, end)
	}
}

func cloneBots(input []Bot) []Bot {
	output := make([]Bot, len(input))
	for i := range input {
		output[i] = input[i]
		revealed := input[i].Token.Reveal()
		output[i].Token = secrets.NewSecret(revealed)
		wipe(revealed)
	}
	return output
}

func destroyBots(bots []Bot) {
	for i := range bots {
		bots[i].Token.Destroy()
	}
}
