package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	token, domain, secret := required("BOT_TOKEN"), required("WEBHOOK_DOMAIN"), required("WEBHOOK_SECRET")
	b, err := bot.New(token, bot.WithWebhookSecretToken(secret))
	if err != nil {
		log.Fatal("failed to initialize bot")
	}
	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, start)
	if _, err := b.SetWebhook(ctx, &bot.SetWebhookParams{URL: domain + "/webhook", SecretToken: secret}); err != nil {
		log.Fatal("failed to register webhook")
	}
	go func() { log.Fatal(http.ListenAndServe(":8083", b.WebhookHandler())) }()
	b.StartWebhook(ctx)
}

func start(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "ready"})
}

func required(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}
