package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/logx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/observability"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue/outbox"
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
	if err := outbox.RegisterLedgerHandlers(worker, logger); err != nil {
		fmt.Fprintln(os.Stderr, "queue handler error:", err)
		os.Exit(1)
	}
	redisOptions, err := asynq.ParseRedisURI(cfg.QueueRedisURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "queue Redis configuration error:", err)
		os.Exit(1)
	}
	client := asynq.NewClient(redisOptions)
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
