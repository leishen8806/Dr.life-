package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	commsNotifications "github.com/leishen8806/Dr.life-/backend/internal/comms/notifications"
	commsTelegram "github.com/leishen8806/Dr.life-/backend/internal/comms/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/files"
	"github.com/leishen8806/Dr.life-/backend/internal/iam"
	iamTelegram "github.com/leishen8806/Dr.life-/backend/internal/iam/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/leishen8806/Dr.life-/backend/internal/logistics"
	"github.com/leishen8806/Dr.life-/backend/internal/logistics/routefit"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
	platformdb "github.com/leishen8806/Dr.life-/backend/internal/platform/db"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/logx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/observability"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/opsalert"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue/outbox"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/storage"
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
	notificationIAMDatabase := database
	if cfg.IAMDatabaseURL != "" && cfg.IAMDatabaseURL != cfg.DatabaseURL {
		iamCfg := cfg
		iamCfg.DatabaseURL = cfg.IAMDatabaseURL
		notificationIAMDatabase, err = observability.NewDatabasePool(ctx, iamCfg, nil, logger)
		if err != nil {
			fmt.Fprintln(os.Stderr, "IAM database configuration error:", err)
			os.Exit(1)
		}
		defer notificationIAMDatabase.Close()
	}
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
	var fileStore files.PostgresStore
	var fileObjects storage.ObjectVerifier
	if cfg.StorageDriver != "" {
		if cfg.FileWorkerDatabaseURL == "" {
			fmt.Fprintln(os.Stderr, "file storage configuration error: FILE_WORKER_DATABASE_URL is required")
			os.Exit(1)
		}
		objects, objectErr := files.NewObjectStore(cfg)
		if objectErr != nil {
			fmt.Fprintln(os.Stderr, "file storage configuration error:", objectErr)
			os.Exit(1)
		}
		fileObjects, _ = objects.(storage.ObjectVerifier)
		if fileObjects == nil {
			fmt.Fprintln(os.Stderr, "file storage configuration error: verification access unavailable")
			os.Exit(1)
		}
		fileDatabase, fileDatabaseErr := platformdb.NewFileWorkerPool(ctx, cfg)
		if fileDatabaseErr != nil {
			fmt.Fprintln(os.Stderr, "file worker database configuration error:", fileDatabaseErr)
			os.Exit(1)
		}
		defer fileDatabase.Close()
		fileStore = files.PostgresStore{DB: fileDatabase}
		if err := files.RegisterTasks(registry); err != nil {
			fmt.Fprintln(os.Stderr, "file queue registry error:", err)
			os.Exit(1)
		}
	}
	if err := logistics.RegisterRouteFitReconciliationTask(registry); err != nil {
		fmt.Fprintln(os.Stderr, "RouteFit reconciliation queue registration error:", err)
		os.Exit(1)
	}
	if routefitBaseURL, routefitToken := os.Getenv("ROUTEFIT_BASE_URL"), os.Getenv("ROUTEFIT_SERVICE_TOKEN"); routefitBaseURL != "" || routefitToken != "" {
		routefitClient, routefitErr := routefit.New(routefit.Config{BaseURL: routefitBaseURL, ServiceToken: routefitToken, Timeout: 10 * time.Second})
		if routefitErr != nil {
			fmt.Fprintln(os.Stderr, "RouteFit fulfillment configuration error:", routefitErr)
			os.Exit(1)
		}
		fulfillmentWorker := logistics.FulfillmentOutboxWorker{DB: database, Client: routefitClient, BatchSize: 20, Lease: 15 * time.Minute, MaxAttempts: 5, RetryDelay: 30 * time.Second}
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				if err := fulfillmentWorker.RunOnce(ctx); err != nil {
					logger.Error("RouteFit fulfillment worker iteration failed", "error", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	notificationWorker, err := buildNotificationWorker(cfg, database, notificationIAMDatabase, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "notification worker configuration error:", err)
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
	asynqOptions, err := asynq.ParseRedisURI(cfg.QueueRedisURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "queue Redis configuration error:", err)
		os.Exit(1)
	}
	client := asynq.NewClient(asynqOptions)
	defer client.Close()
	alerts := reconciliationAlerts(cfg, logger, metrics.Business(), opsalert.NewRedisDeduper(alertRedis), opsalert.NewRedisMerger(alertRedis), client)
	if err := outbox.RegisterLedgerHandlersWithAlerts(worker, logger, alerts.Alert); err != nil {
		fmt.Fprintln(os.Stderr, "queue handler error:", err)
		os.Exit(1)
	}
	if err := worker.Register(opsalert.TaskFlush, alerts.Flush); err != nil {
		fmt.Fprintln(os.Stderr, "queue handler error:", err)
		os.Exit(1)
	}
	if fileObjects != nil {
		if err := files.RegisterVerificationHandler(worker, fileStore, fileObjects); err != nil {
			fmt.Fprintln(os.Stderr, "file queue handler error:", err)
			os.Exit(1)
		}
		if err := files.RegisterCleanupHandler(worker, fileStore, fileObjects); err != nil {
			fmt.Fprintln(os.Stderr, "file cleanup handler error:", err)
			os.Exit(1)
		}
	}
	// R-02 installs the RouteFit client runner. Until then the scheduled task is
	// deliberately a no-op after the queue payload is validated by its contract.
	if err := worker.Register(logistics.RouteFitReconciliationTaskType, logistics.NewRouteFitReconciliationHandler(nil)); err != nil {
		fmt.Fprintln(os.Stderr, "RouteFit reconciliation handler error:", err)
		os.Exit(1)
	}
	relay, err := outbox.New(ledger.New(database, ledger.Options{}), client, outbox.Options{
		Routes: outbox.LedgerRoutes(),
		OnRetry: func(err error) {
			metrics.Business().OutboxRelayRetry()
			logger.Error("outbox relay retrying after transient database failure", "error", err)
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "outbox relay configuration error:", err)
		os.Exit(1)
	}
	if err := worker.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "worker start error:", err)
		os.Exit(1)
	}
	if notificationWorker != nil {
		go runNotificationWorker(ctx, notificationWorker, logger)
	}
	relayDone := make(chan error, 1)
	go func() { relayDone <- relay.Run(ctx) }()
	select {
	case <-ctx.Done():
		return
	case relayErr := <-relayDone:
		if errors.Is(relayErr, context.Canceled) {
			return
		}
		logger.Error("outbox relay stopped", "error", relayErr)
		os.Exit(relayExitCode(relayErr))
	}
}

func buildNotificationWorker(cfg config.Config, database, iamDatabase *pgxpool.Pool, logger *slog.Logger) (*commsNotifications.Worker, error) {
	if !cfg.JWTSigningConfigured {
		return nil, nil
	}
	masterKeys, err := secrets.LoadFromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("bot token configuration: %w", err)
	}
	envelope, err := secrets.NewEnvelope(masterKeys)
	if err != nil {
		return nil, fmt.Errorf("bot token encryption configuration: %w", err)
	}
	source := &iamTelegram.DBPoolSource{DB: iamDatabase, Envelope: envelope, TTL: cfg.BotTokenCacheTTL}
	accountDirectory := iam.AccountDirectory{DB: iamDatabase}
	repo := commsNotifications.NewPostgresRepository(database, func(ctx context.Context, _ int64, end commsNotifications.End) ([]commsNotifications.Bot, error) {
		bots, err := source.Bots(ctx, iamTelegram.End(end))
		if err != nil {
			return nil, err
		}
		result := make([]commsNotifications.Bot, 0, len(bots))
		for _, bot := range bots {
			bot.Token.Destroy()
			result = append(result, commsNotifications.Bot{ID: bot.ID, End: end, Active: true})
		}
		return result, nil
	})
	sender := commsTelegram.NewHTTPSender("https://api.telegram.org", iamTelegram.AnyPoolTokenProvider{Source: source}, commsTelegram.SenderOptions{})
	return &commsNotifications.Worker{
		Repo: repo, Inbox: repo, Telegram: commsNotifications.TelegramDeliveryWriter{
			Sender: sender,
			ChatID: func(ctx context.Context, accountID int64) (int64, error) {
				return accountDirectory.TelegramChatID(ctx, accountID)
			},
			DeepLink: func(event commsNotifications.Event) string {
				username := os.Getenv("TELEGRAM_BUYER_BOT_USERNAME")
				if event.End == commsNotifications.Merchant {
					username = os.Getenv("TELEGRAM_MERCHANT_BOT_USERNAME")
				}
				link, err := commsNotifications.StartAppLink(username, event.ID)
				if err != nil {
					return ""
				}
				return link
			},
		}, MaxAttempts: 8,
	}, nil
}

func runNotificationWorker(ctx context.Context, worker *commsNotifications.Worker, logger *slog.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := worker.RunOnce(ctx); err != nil {
				logger.Error("notification worker iteration failed", "error", err)
			}
		}
	}
}

func relayExitCode(err error) int {
	if err == nil || errors.Is(err, context.Canceled) {
		return 0
	}
	return 1
}

func reconciliationAlerts(cfg config.Config, logger *slog.Logger, metrics opsalert.Metrics, deduper opsalert.Deduper, merger opsalert.Merger, scheduler opsalert.Scheduler) opsalert.Handlers {
	options := opsalert.Options{Environment: cfg.AppEnv, Language: cfg.OpsAlertLang, MergeWindow: cfg.OpsAlertMergeWindow, Metrics: metrics, Logger: logger, Deduper: deduper, Merger: merger, Scheduler: scheduler}
	token := os.Getenv("OPS_ALERT_BOT_TOKEN")
	if cfg.OpsAlertBotID == 0 || cfg.OpsAlertChatID == 0 || token == "" {
		logger.Warn("operations reconciliation alerts are not configured; undeliverable alerts will be logged and counted")
		return opsalert.NewReconciliationHandlers(options)
	}
	options.Sender = commsTelegram.NewHTTPSender("https://api.telegram.org", commsTelegram.StaticTokenProvider{strconv.FormatInt(cfg.OpsAlertBotID, 10): token}, commsTelegram.SenderOptions{})
	options.BotID = strconv.FormatInt(cfg.OpsAlertBotID, 10)
	options.ChatID = cfg.OpsAlertChatID
	return opsalert.NewReconciliationHandlers(options)
}
