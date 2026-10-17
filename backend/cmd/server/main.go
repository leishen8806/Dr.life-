package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/logx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/observability"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/overload"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(1)
	}

	logger := logx.NewLogger(cfg.LogLevel, os.Stdout)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	metrics := observability.NewMetrics()
	bulkhead := overload.New(overload.Config{Total: cfg.BulkheadTotal, Wait: cfg.BulkheadWait, Limits: overload.Limits{Critical: cfg.BulkheadLimits["critical"], Auth: cfg.BulkheadLimits["auth"], Write: cfg.BulkheadLimits["write"], Read: cfg.BulkheadLimits["read"], Heavy: cfg.BulkheadLimits["heavy"]}, Timeouts: overload.DefaultConfig().Timeouts, Rules: routeClasses(cfg.BulkheadRules), Metrics: metrics})
	reporter, err := observability.NewErrorReporter(cfg.SentryDSN, cfg.AppEnv, cfg.AppRelease, nil)
	if err != nil {
		logger.Error("Sentry configuration failed", "error", err)
		os.Exit(1)
	}
	defer reporter.Flush(2 * time.Second)

	database, err := observability.NewDatabasePool(ctx, cfg, metrics, logger)
	if err != nil {
		logger.Error("database pool failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	var waitMu sync.Mutex
	var lastAcquireCount int64
	var lastAcquireDuration time.Duration
	dbWait := func() time.Duration {
		stat := database.Stat()
		waitMu.Lock()
		defer waitMu.Unlock()
		countDelta := stat.AcquireCount() - lastAcquireCount
		durationDelta := stat.AcquireDuration() - lastAcquireDuration
		lastAcquireCount, lastAcquireDuration = stat.AcquireCount(), stat.AcquireDuration()
		if countDelta <= 0 {
			return 0
		}
		return durationDelta / time.Duration(countDelta)
	}
	brownout := overload.NewBrownout(overload.BrownoutConfig{
		BaseHeavy:              cfg.BulkheadLimits["heavy"],
		EscalateAfter:          cfg.BrownoutEscalateAfter,
		RecoverAfter:           cfg.BrownoutRecoverAfter,
		P95Threshold:           cfg.BrownoutP95Threshold,
		P95Recovery:            cfg.BrownoutP95Recovery,
		DBWaitThreshold:        cfg.BrownoutDBWaitThreshold,
		DBWaitRecovery:         cfg.BrownoutDBWaitRecovery,
		InFlightRatioThreshold: cfg.BrownoutInFlightRatioThreshold,
		InFlightRatioRecovery:  cfg.BrownoutInFlightRatioRecovery,
	}, func() overload.Signals {
		return overload.Signals{DBWait: dbWait(), InFlightRatio: bulkhead.InFlightRatio()}
	}, overload.BrownoutOptions{
		Logger:  logger,
		Metrics: metrics,
		OnChange: func(_ overload.BrownoutLevel, limit int) {
			bulkhead.SetHeavyLimit(limit)
		},
	})
	bulkhead.SetBrownout(brownout)
	go brownout.Run(ctx, cfg.BrownoutSampleInterval)

	clientIP, err := gateway.NewClientIPResolver(cfg.TrustedProxyHeader, cfg.TrustedProxyCIDRs)
	if err != nil {
		logger.Error("gateway configuration failed", "error", err)
		os.Exit(1)
	}
	adminAccess, err := gateway.NewAdminAccess(cfg.AppEnv, clientIP, cfg.AdminAllowedCIDRs)
	if err != nil {
		logger.Error("admin network configuration failed", "error", err)
		os.Exit(1)
	}
	memoryLimiter := gateway.NewMemoryLimiter()
	var limiter gateway.Limiter = memoryLimiter
	var idempotencyStore gateway.IdempotencyStore
	if cfg.RedisURL != "" {
		redisLimiter, redisErr := gateway.NewRedisLimiter(cfg.RedisURL)
		if redisErr != nil {
			logger.Error("Redis limiter configuration failed", "error", redisErr)
			os.Exit(1)
		}
		defer redisLimiter.Close()
		limiter = &gateway.FallbackLimiter{Primary: redisLimiter, Fallback: memoryLimiter, Logger: logger}
		redisIdempotency, redisErr := gateway.NewRedisIdempotencyStore(cfg.RedisURL)
		if redisErr != nil {
			logger.Error("Redis idempotency configuration failed", "error", redisErr)
			os.Exit(1)
		}
		defer redisIdempotency.Close()
		idempotencyStore = redisIdempotency
	}
	strictRules := func(defaultLimit int) gateway.RuleSet {
		return gateway.RuleSet{
			Default:    gateway.Rule{Limit: defaultLimit, Window: time.Minute},
			Login:      gateway.Rule{Limit: cfg.RateLimitLogin, Window: time.Minute},
			Withdrawal: gateway.Rule{Limit: cfg.RateLimitWithdraw, Window: time.Minute},
			AI:         gateway.Rule{Limit: cfg.RateLimitAI, Window: time.Minute},
			Admin:      gateway.Rule{Limit: cfg.RateLimitAdmin, Window: time.Minute},
		}
	}

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		logger.Error("http listener failed", "error", err)
		os.Exit(1)
	}

	handler := httpx.ETag(server.NewHandlerWithGateway(logger, server.GatewayOptions{
		AppEnv: cfg.AppEnv,
		OriginGroups: gateway.OriginGroups{
			Buyer: cfg.CORSBuyerOrigins, Merchant: cfg.CORSMerchantOrigins, Driver: cfg.CORSDriverOrigins,
			WMS: cfg.CORSWMSOrigins, Admin: cfg.CORSAdminOrigins, Public: cfg.CORSPublicOrigins,
			CredentialPaths: cfg.CORSCredentialPaths,
		},
		WebhookPrefix:  cfg.WebhookPrefix,
		WebhookMaxBody: cfg.WebhookMaxBodyBytes,
		ClientIP:       clientIP,
		AdminAccess:    adminAccess,
		Limiter:        limiter,
		IPRules:        strictRules(cfg.RateLimitIP),
		AccountRules:   strictRules(cfg.RateLimitAccount),
		BodyPolicy:     gateway.BodyPolicy{DefaultBytes: cfg.RequestBodyMaxBytes, Overrides: cfg.RequestBodyOverrides, ExemptPaths: cfg.ContentTypeExemptPaths},
		Idempotency: gateway.IdempotencyOptions{
			Store: idempotencyStore, FallbackStore: gateway.NewMemoryIdempotencyStore(), TTL: cfg.IdempotencyTTL, MaxResponseBytes: cfg.IdempotencyMaxBytes,
			FinancialPrefixes: cfg.FinancialPrefixes,
		},
		Metrics:       metrics,
		ErrorReporter: reporter,
		Bulkhead:      bulkhead,
	}, database.Ping))
	httpServer := server.NewHTTPServer(cfg.HTTPAddr, handler, cfg.ReadHeaderTimeout, cfg.ReadTimeout, cfg.IdleTimeout)
	// WriteTimeout stays unset so future real-time streams can remain open.
	if cfg.MetricsAddr != "" {
		metricsListener, listenErr := net.Listen("tcp", cfg.MetricsAddr)
		if listenErr != nil {
			logger.Error("metrics listener failed", "error", listenErr)
			os.Exit(1)
		}
		metricsServer := &http.Server{
			Addr: cfg.MetricsAddr, Handler: observability.MetricsHandler(metrics),
			ReadHeaderTimeout: cfg.ReadHeaderTimeout, ReadTimeout: cfg.ReadTimeout,
			WriteTimeout: 15 * time.Second, IdleTimeout: cfg.IdleTimeout,
		}
		go func() {
			logger.Info("internal metrics server starting", "addr", cfg.MetricsAddr)
			if serveErr := server.Serve(ctx, metricsServer, metricsListener, cfg.ShutdownTimeout); serveErr != nil {
				logger.Error("metrics server stopped with error", "error", serveErr)
				stop()
			}
		}()
	}
	logger.Info("http server starting", "addr", cfg.HTTPAddr, "environment", cfg.AppEnv)
	if err := server.Serve(ctx, httpServer, listener, cfg.ShutdownTimeout, struct {
		server.Drainer
		Delay time.Duration
	}{Drainer: bulkhead, Delay: cfg.BulkheadDrain}); err != nil {
		logger.Error("http server stopped with error", "error", err)
		os.Exit(1)
	}
}

func routeClasses(values map[string]string) map[string]overload.Class {
	result := make(map[string]overload.Class, len(values))
	for path, class := range values {
		result[path] = overload.Class(class)
	}
	return result
}
