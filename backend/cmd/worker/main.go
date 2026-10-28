package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/hibiken/asynq"
	commsTelegram "github.com/leishen8806/Dr.life-/backend/internal/comms/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/logx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/observability"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/opsalert"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue/outbox"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(1)
	}
	if cfg.QueueRedisURL == "" {
		fmt.Fprintln(os.Stderr, "configuration error: QUEUE_REDIS_URL is required for worker")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := logx.NewLogger(cfg.LogLevel, os.Stdout)
	metrics := observability.NewMetrics()
	database, err := observability.NewDatabasePool(ctx, cfg, nil, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "database configuration error:", err)
		os.Exit(1)
	}
	defer database.Close()
	if err := queue.QueueRedisPolicy(ctx, cfg.QueueRedisURL, cfg.QueueAllowEviction); err != nil {
		fmt.Fprintln(os.Stderr, "queue Redis configuration error:", err)
		os.Exit(1)
	}
	queueMetrics, err := queue.NewMetricsCollector(cfg.QueueRedisURL, queue.AllQueues, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "queue metrics configuration error:", err)
		os.Exit(1)
	}
	defer queueMetrics.Close()
	registry := queue.NewRegistry()
	if err := outbox.RegisterLedgerTasks(registry); err != nil {
		fmt.Fprintln(os.Stderr, "queue registry error:", err)
		os.Exit(1)
	}
	worker, err := queue.NewWorker(cfg.QueueRedisURL, registry, queue.WorkerOptions{ShutdownTimeout: cfg.ShutdownTimeout, Logger: logger, Observer: queueMetrics})
	if err != nil {
		fmt.Fprintln(os.Stderr, "worker configuration error:", err)
		os.Exit(1)
	}
	alertRedisOptions, err := redis.ParseURL(cfg.QueueRedisURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "queue Redis configuration error:", err)
		os.Exit(1)
	}
	alertRedis := redis.NewClient(alertRedisOptions)
	defer alertRedis.Close()
	alerts := reconciliationAlerts(cfg, logger, metrics.Business(), opsalert.NewRedisDeduper(alertRedis))
	if err := outbox.RegisterLedgerHandlersWithAlerts(worker, logger, alerts); err != nil {
		fmt.Fprintln(os.Stderr, "queue handler error:", err)
		os.Exit(1)
	}
	asynqOptions, err := asynq.ParseRedisURI(cfg.QueueRedisURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "queue Redis configuration error:", err)
		os.Exit(1)
	}
	client := asynq.NewClient(asynqOptions)
	defer client.Close()
	relay, err := outbox.New(ledger.New(database, ledger.Options{}), client, outbox.Options{Routes: outbox.LedgerRoutes()})
	if err != nil {
		fmt.Fprintln(os.Stderr, "outbox relay configuration error:", err)
		os.Exit(1)
	}
	if err := worker.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "worker start error:", err)
		os.Exit(1)
	}
	go func() {
		if err := relay.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("outbox relay stopped", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
}

func reconciliationAlerts(cfg config.Config, logger *slog.Logger, metrics opsalert.Metrics, deduper opsalert.Deduper) queue.Handler {
	options := opsalert.Options{Environment: cfg.AppEnv, Language: cfg.OpsAlertLang, MergeWindow: cfg.OpsAlertMergeWindow, Metrics: metrics, Logger: logger, Deduper: deduper}
	token := os.Getenv("OPS_ALERT_BOT_TOKEN")
	if cfg.OpsAlertBotID == 0 || cfg.OpsAlertChatID == 0 || token == "" {
		logger.Warn("operations reconciliation alerts are not configured; undeliverable alerts will be logged and counted")
		return opsalert.NewReconciliationHandler(options)
	}
	options.Sender = commsTelegram.NewHTTPSender("https://api.telegram.org", commsTelegram.StaticTokenProvider{strconv.FormatInt(cfg.OpsAlertBotID, 10): token}, commsTelegram.SenderOptions{})
	options.BotID = strconv.FormatInt(cfg.OpsAlertBotID, 10)
	options.ChatID = cfg.OpsAlertChatID
	return opsalert.NewReconciliationHandler(options)
}
