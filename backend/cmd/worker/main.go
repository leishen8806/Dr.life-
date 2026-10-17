package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/logx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
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
	worker, err := queue.NewWorker(cfg.QueueRedisURL, queue.NewRegistry(), queue.WorkerOptions{ShutdownTimeout: cfg.ShutdownTimeout, Logger: logx.NewLogger(cfg.LogLevel, os.Stdout), Observer: queueMetrics})
	if err != nil {
		fmt.Fprintln(os.Stderr, "worker configuration error:", err)
		os.Exit(1)
	}
	if err := worker.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "worker start error:", err)
		os.Exit(1)
	}
	<-ctx.Done()
}
