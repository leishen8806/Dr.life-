package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"log/slog"

	"github.com/go-chi/chi/v5"
	api "github.com/leishen8806/Dr.life-/backend/internal/api"
	"github.com/leishen8806/Dr.life-/backend/internal/api/gen"
	"github.com/leishen8806/Dr.life-/backend/internal/catalog"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/observability"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/overload"
)

type ReadinessCheck func(context.Context) error

func NewHTTPServer(addr string, handler http.Handler, readHeaderTimeout, readTimeout, idleTimeout time.Duration) *http.Server {
	return &http.Server{
		Addr: addr, Handler: handler, ReadHeaderTimeout: readHeaderTimeout, ReadTimeout: readTimeout,
		IdleTimeout: idleTimeout, MaxHeaderBytes: 16 << 10,
	}
}

type GatewayOptions struct {
	AppEnv            string
	OriginGroups      gateway.OriginGroups
	WebhookPrefix     string
	WebhookBindings   map[string]gateway.WebhookVerifier
	WebhookMaxBody    int64
	ClientIP          gateway.ClientIPResolver
	AdminAccess       gateway.AdminAccess
	Limiter           gateway.Limiter
	Authenticator     gateway.Authenticator
	IPRules           gateway.RuleSet
	AccountRules      gateway.RuleSet
	IdempotencyExempt func(*http.Request) bool
	Idempotency       gateway.IdempotencyOptions
	BodyPolicy        gateway.BodyPolicy
	Metrics           *observability.Metrics
	ErrorReporter     *observability.ErrorReporter
	Bulkhead          *overload.Bulkhead
	Catalog           *catalog.Service
	Merchants         api.MerchantResolver
	Logger            *slog.Logger
	Login             api.TelegramLogin
	Audit             *platformaudit.Store
	AuditWriter       platformaudit.Writer
	Sessions          api.SessionManagement
	AccountProfiles   api.AccountProfileManagement
	AccountLinks      api.AccountLinkManagement
	Ledger            api.LedgerReader
	Directory         api.AccountDirectory
	AdminPermissions  api.AdminPermissionChecker
	Files             api.FileManagement
}

func NewHandler(logger *slog.Logger, checks ...ReadinessCheck) http.Handler {
	rule := gateway.Rule{Limit: 100000, Window: time.Minute}
	return NewHandlerWithGateway(logger, GatewayOptions{
		AppEnv: "dev", OriginGroups: gateway.DefaultOriginGroups("dev"),
		Limiter: gateway.NewMemoryLimiter(), IPRules: gateway.RuleSet{Default: rule, Login: rule, Withdrawal: rule, AI: rule}, AccountRules: gateway.RuleSet{Default: rule, Login: rule, Withdrawal: rule, AI: rule},
	}, checks...)
}

func NewHandlerWithGateway(logger *slog.Logger, options GatewayOptions, checks ...ReadinessCheck) http.Handler {
	router := chi.NewRouter()
	if options.AdminAccess.AppEnv == "" {
		options.AdminAccess.AppEnv = options.AppEnv
	}
	if options.Bulkhead == nil {
		options.Bulkhead = overload.New(overload.DefaultConfig())
	}
	router.Use(httpx.RequestID)
	router.Use(gateway.SecurityHeaders(options.AppEnv))
	if options.Metrics != nil {
		router.Use(options.Metrics.HTTPMiddleware)
	}
	router.Use(httpx.RequestLogger(logger, gateway.RoleFromContext))
	router.Use(observability.Recover(logger, options.ErrorReporter))
	router.Use(gateway.GroupedCORS(options.OriginGroups))
	router.Use(options.Bulkhead.Middleware)
	router.Use(gateway.LimitByIP(options.Limiter, options.ClientIP, options.IPRules))
	router.Use(options.AdminAccess.Middleware)
	router.Use(gateway.VerifyWebhooks(options.WebhookPrefix, options.WebhookMaxBody, options.WebhookBindings))
	router.Use(gateway.Authenticate(options.Authenticator, options.WebhookPrefix))
	router.Use(gateway.Authorize(options.WebhookPrefix))
	router.Use(gateway.LimitRequestBody(options.BodyPolicy))
	router.Use(gateway.LimitByAccount(options.Limiter, options.AccountRules))
	router.Use(gateway.RequireIdempotency(options.IdempotencyExempt))
	router.Use(gateway.BindIdempotency(options.Idempotency))
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeResourceNotFound))
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeMethodNotAllowed))
	})
	auditWriter := options.AuditWriter
	if auditWriter == nil {
		auditWriter = options.Audit
	}
	gen.HandlerFromMux(api.Handler{Catalog: options.Catalog, Merchants: options.Merchants, Logger: options.Logger, Login: options.Login, Sessions: options.Sessions, AccountProfiles: options.AccountProfiles, AccountLinks: options.AccountLinks, ClientIP: options.ClientIP, Audit: options.Audit, AuditWriter: auditWriter, Ledger: options.Ledger, Directory: options.Directory, AdminPermissions: options.AdminPermissions, Files: options.Files}, router)
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if options.Bulkhead.Draining() {
			httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
			return
		}
		for _, check := range checks {
			if err := check(r.Context()); err != nil {
				if logger != nil {
					logger.ErrorContext(r.Context(), "readiness check failed", "request_id", httpx.RequestIDFromContext(r.Context()))
				}
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
				return
			}
		}
		httpx.WriteOK(w, r, map[string]bool{"ready": true})
	})
	return router
}

type Drainer interface{ BeginDraining() }

func Serve(ctx context.Context, srv *http.Server, listener net.Listener, shutdownTimeout time.Duration, draining ...struct {
	Drainer
	Delay time.Duration
}) error {
	serveResult := make(chan error, 1)
	go func() { serveResult <- srv.Serve(listener) }()
	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	for _, drain := range draining {
		drain.BeginDraining()
		timer := time.NewTimer(drain.Delay)
		select {
		case <-timer.C:
		case err := <-serveResult:
			timer.Stop()
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		return err
	}
	err := <-serveResult
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
