package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/iam"
	"github.com/leishen8806/Dr.life-/backend/internal/iam/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/logx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/observability"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
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

	clientIP, err := gateway.NewClientIPResolver(cfg.TrustedProxyHeader, cfg.TrustedProxyCIDRs)
	if err != nil {
		logger.Error("gateway configuration failed", "error", err)
		os.Exit(1)
	}
	memoryLimiter := gateway.NewMemoryLimiter()
	var limiter gateway.Limiter = memoryLimiter
	if cfg.RedisURL != "" {
		redisLimiter, redisErr := gateway.NewRedisLimiter(cfg.RedisURL)
		if redisErr != nil {
			logger.Error("Redis limiter configuration failed", "error", redisErr)
			os.Exit(1)
		}
		defer redisLimiter.Close()
		limiter = &gateway.FallbackLimiter{Primary: redisLimiter, Fallback: memoryLimiter, Logger: logger}
	}
	strictRules := func(defaultLimit int) gateway.RuleSet {
		return gateway.RuleSet{
			Default:    gateway.Rule{Limit: defaultLimit, Window: time.Minute},
			Login:      gateway.Rule{Limit: cfg.RateLimitLogin, Window: time.Minute},
			Withdrawal: gateway.Rule{Limit: cfg.RateLimitWithdraw, Window: time.Minute},
			AI:         gateway.Rule{Limit: cfg.RateLimitAI, Window: time.Minute},
		}
	}
	var loginService *iam.Service
	var authenticator gateway.Authenticator
	checks := []server.ReadinessCheck{database.Ping}
	iamDatabase := database
	if cfg.JWTSigningConfigured {
		if cfg.IAMDatabaseURL == "" {
			logger.Error("IAM database URL is required when Telegram login is enabled")
			os.Exit(1)
		}
		iamCfg := cfg
		iamCfg.DatabaseURL = cfg.IAMDatabaseURL
		iamDatabase, err = observability.NewDatabasePool(ctx, iamCfg, nil, logger)
		if err != nil {
			logger.Error("IAM database pool failed", "error", err)
			os.Exit(1)
		}
		defer iamDatabase.Close()
		checks = append(checks, iamDatabase.Ping)
		jwtKeys, keyErr := iam.LoadJWTKeyringFromEnvironment()
		if keyErr != nil {
			logger.Error("JWT key configuration failed")
			os.Exit(1)
		}
		masterKeys, keyErr := secrets.LoadFromEnvironment()
		if keyErr != nil {
			logger.Error("Bot token key configuration failed")
			os.Exit(1)
		}
		envelope, envelopeErr := secrets.NewEnvelope(masterKeys)
		if envelopeErr != nil {
			logger.Error("Bot token encryption configuration failed")
			os.Exit(1)
		}
		tokens := iam.TokenManager{Keys: jwtKeys, AccessTTL: cfg.AccessTokenTTL, Checker: iam.PassSessionChecker{}}
		loginService = &iam.Service{
			DB:       iamDatabase,
			Verifier: telegram.PoolVerifier{Validator: telegram.NewValidator(cfg.TelegramInitDataMaxAge), Source: &telegram.DBPoolSource{DB: iamDatabase, Envelope: envelope, TTL: cfg.BotTokenCacheTTL}},
			Tokens:   tokens, RefreshTTL: cfg.RefreshTokenTTL,
			Failures: &iam.FailureGuard{Limiter: limiter, Rule: gateway.Rule{Limit: cfg.LoginFailureLimit, Window: cfg.LoginFailureWindow}},
			Metrics:  metrics.Business(),
		}
		authenticator = iam.Authenticator{Tokens: tokens}
	}

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		logger.Error("http listener failed", "error", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: server.NewHandlerWithGateway(logger, server.GatewayOptions{
			AllowedOrigins: cfg.CORSAllowedOrigins,
			WebhookPrefix:  cfg.WebhookPrefix,
			ClientIP:       clientIP,
			Limiter:        limiter,
			IPRules:        strictRules(cfg.RateLimitIP),
			AccountRules:   strictRules(cfg.RateLimitAccount),
			Metrics:        metrics,
			ErrorReporter:  reporter,
			Authenticator:  authenticator,
			Login:          loginService,
			IdempotencyExempt: func(r *http.Request) bool {
				return r.Method == http.MethodPost && r.URL.Path == "/v1/public/auth/telegram"
			},
		}, checks...),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		// WriteTimeout stays unset so future real-time streams can remain open.
	}
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
	if err := server.Serve(ctx, httpServer, listener, cfg.ShutdownTimeout); err != nil {
		logger.Error("http server stopped with error", "error", err)
		os.Exit(1)
	}
}
