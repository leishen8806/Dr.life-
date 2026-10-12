package main

import (
	"log"
	"log/slog"
	"os"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

func main() {
	token, domain, secret := required("BOT_TOKEN"), required("WEBHOOK_DOMAIN"), required("WEBHOOK_SECRET")
	bot, err := gotgbot.NewBot(token, nil)
	if err != nil {
		log.Fatal("failed to initialize bot")
	}
	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{Logger: slog.Default()})
	dispatcher.AddHandler(handlers.NewCommand("start", func(bot *gotgbot.Bot, ctx *ext.Context) error {
		_, err := ctx.EffectiveMessage.Reply(bot, "ready", nil)
		return err
	}))
	updater := ext.NewUpdater(dispatcher, &ext.UpdaterOpts{Logger: slog.Default()})
	opts := ext.WebhookOpts{ListenAddr: ":8082", SecretToken: secret}
	if err := updater.StartWebhook(bot, "webhook", opts); err != nil {
		log.Fatal("failed to start webhook")
	}
	if err := updater.SetAllBotWebhooks(domain, &gotgbot.SetWebhookOpts{SecretToken: secret}); err != nil {
		log.Fatal("failed to register webhook")
	}
	updater.Idle()
}

func required(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}
