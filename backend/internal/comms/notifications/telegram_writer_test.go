package notifications

import (
	"context"
	"errors"
	"testing"

	telegramclient "github.com/leishen8806/Dr.life-/backend/internal/comms/telegram"
)

type writerSender struct {
	botID string
	chat  int64
	text  string
	kind  telegramclient.ResultKind
}

func (s *writerSender) SendMessage(_ context.Context, botID string, chatID int64, text string, _ telegramclient.SendOptions) telegramclient.Result {
	s.botID, s.chat, s.text = botID, chatID, text
	return telegramclient.Result{Kind: s.kind}
}

func TestTelegramDeliveryWriterUsesSelectedBotAndResolvedChat(t *testing.T) {
	sender := &writerSender{kind: telegramclient.ResultSuccess}
	writer := TelegramDeliveryWriter{
		Sender:   sender,
		ChatID:   func(_ context.Context, accountID int64) (int64, error) { return accountID + 9000, nil },
		DeepLink: func(event Event) string { return "https://t.me/buyer_bot?startapp=" + event.ID },
	}
	event := Event{ID: "order-1", AccountID: 42, End: Buyer, Title: map[string]string{"en": "Order"}, Body: map[string]string{"en": "Created"}}
	if err := writer.SendOnce(context.Background(), 7, event); err != nil {
		t.Fatal(err)
	}
	if sender.botID != "7" || sender.chat != 9042 || sender.text != "Order\nCreated\nhttps://t.me/buyer_bot?startapp=order-1" {
		t.Fatalf("sender call bot=%q chat=%d text=%q", sender.botID, sender.chat, sender.text)
	}
}

func TestTelegramDeliveryWriterSanitizesTransportFailureBoundary(t *testing.T) {
	writer := TelegramDeliveryWriter{Sender: &writerSender{kind: telegramclient.ResultTemporaryError}, ChatID: func(context.Context, int64) (int64, error) { return 9, nil }}
	if err := writer.SendOnce(context.Background(), 7, Event{AccountID: 1, Body: map[string]string{"en": "hello"}}); err == nil || err.Error() != "notifications: telegram delivery failed: temporary_error" {
		t.Fatalf("err=%v", err)
	}
	writer.ChatID = func(context.Context, int64) (int64, error) { return 0, errors.New("private account mapping") }
	if err := writer.SendOnce(context.Background(), 7, Event{AccountID: 1}); err == nil || err.Error() != "private account mapping" {
		t.Fatalf("resolver err=%v", err)
	}
}
