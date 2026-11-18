package notifications

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	telegramclient "github.com/leishen8806/Dr.life-/backend/internal/comms/telegram"
)

// ChatIDResolver maps an application account to its Telegram chat. Keeping
// this lookup behind an adapter prevents the notification schema from copying
// Telegram identifiers and lets the IAM/account boundary own the mapping.
type ChatIDResolver func(context.Context, int64) (int64, error)

// TelegramDeliveryWriter connects the notification worker to the outbound
// Telegram adapter. The worker has already selected a Bot ID using the
// recipient end, so this writer never falls back to another end's pool.
type TelegramDeliveryWriter struct {
	Sender   telegramclient.Sender
	ChatID   ChatIDResolver
	DeepLink func(Event) string
}

func (w TelegramDeliveryWriter) SendOnce(ctx context.Context, botID int64, event Event) error {
	if w.Sender == nil || w.ChatID == nil {
		return errors.New("notifications: telegram delivery is unavailable")
	}
	chatID, err := w.ChatID(ctx, event.AccountID)
	if err != nil {
		return err
	}
	if chatID == 0 || botID == 0 {
		return errors.New("notifications: telegram recipient is unavailable")
	}
	text := eventText(event)
	if w.DeepLink != nil {
		if link := strings.TrimSpace(w.DeepLink(event)); link != "" {
			text += "\n" + link
		}
	}
	result := w.Sender.SendMessage(ctx, strconv.FormatInt(botID, 10), chatID, text, telegramclient.SendOptions{})
	if result.Kind != telegramclient.ResultSuccess {
		if result.Err != nil {
			return result.Err
		}
		return fmt.Errorf("notifications: telegram delivery failed: %s", resultKind(result.Kind))
	}
	return nil
}

func eventText(event Event) string {
	title := localized(event.Title)
	body := localized(event.Body)
	if title == "" {
		return body
	}
	if body == "" {
		return title
	}
	return title + "\n" + body
}

func localized(values map[string]string) string {
	for _, lang := range []string{"zh", "en", "km"} {
		if value := strings.TrimSpace(values[lang]); value != "" {
			return value
		}
	}
	return ""
}

func resultKind(kind telegramclient.ResultKind) string {
	switch kind {
	case telegramclient.ResultRateLimited:
		return "rate_limited"
	case telegramclient.ResultUserBlocked:
		return "user_blocked"
	case telegramclient.ResultChatNotFound:
		return "chat_not_found"
	case telegramclient.ResultTemporaryError:
		return "temporary_error"
	default:
		return "permanent_error"
	}
}
