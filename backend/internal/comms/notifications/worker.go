package notifications

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type OutboxItem struct {
	ID              int64
	Event           Event
	Bots            []Bot
	TelegramEnabled bool
	Attempts        int
}

type Repository interface {
	ClaimPending(context.Context) (OutboxItem, bool, error)
	MarkDelivered(context.Context, OutboxItem) error
	MarkRetry(context.Context, OutboxItem, time.Time, string, bool) error
}

type InboxWriter interface {
	PutOnce(context.Context, Event) error
}

type TelegramWriter interface {
	SendOnce(context.Context, int64, Event) error
}

type Worker struct {
	Repo        Repository
	Inbox       InboxWriter
	Telegram    TelegramWriter
	MaxAttempts int
	Now         func() time.Time
	Backoff     func(int) time.Duration
}

func (w Worker) RunOnce(ctx context.Context) (bool, error) {
	if w.Repo == nil {
		return false, errors.New("notifications: repository is required")
	}
	item, ok, err := w.Repo.ClaimPending(ctx)
	if err != nil || !ok {
		return false, err
	}
	if err := item.Event.Validate(); err != nil {
		return true, w.fail(ctx, item, err)
	}
	plan, err := Plan(item.Event, item.Bots, item.TelegramEnabled)
	if err != nil {
		return true, w.fail(ctx, item, err)
	}
	if plan.InApp && w.Inbox != nil {
		if err := w.Inbox.PutOnce(ctx, item.Event); err != nil {
			return true, w.fail(ctx, item, err)
		}
	}
	if plan.TelegramBotID != 0 && w.Telegram != nil {
		if err := w.Telegram.SendOnce(ctx, plan.TelegramBotID, item.Event); err != nil {
			return true, w.fail(ctx, item, err)
		}
	}
	return true, w.Repo.MarkDelivered(ctx, item)
}

func (w Worker) fail(ctx context.Context, item OutboxItem, cause error) error {
	attempts := item.Attempts + 1
	max := w.MaxAttempts
	if max <= 0 {
		max = 8
	}
	now := time.Now()
	if w.Now != nil {
		now = w.Now()
	}
	dead := attempts >= max
	backoff := time.Minute
	if w.Backoff != nil {
		backoff = w.Backoff(attempts)
	}
	if dead {
		backoff = 0
	}
	return w.Repo.MarkRetry(ctx, item, now.Add(backoff), sanitizeError(cause), dead)
}

func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("notification delivery failed: %T", err)
}
