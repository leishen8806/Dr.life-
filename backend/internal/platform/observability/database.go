package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
)

type queryStartedKey struct{}

type queryStarted struct {
	at          time.Time
	fingerprint string
	template    string
}

type QueryTracer struct {
	Threshold time.Duration
	Metrics   *Metrics
	Logger    *slog.Logger
	catalog   *slowQueryCatalog
}

const (
	defaultSlowQueryFingerprintLimit = 256
	maxSQLTemplateRunes              = 500
	slowQueryOverflowLabel           = "overflow"
)

type slowQueryCatalog struct {
	mu           sync.Mutex
	fingerprints map[string]struct{}
	limit        int
}

func NewQueryTracer(threshold time.Duration, metrics *Metrics, logger *slog.Logger) *QueryTracer {
	return newQueryTracer(threshold, metrics, logger, defaultSlowQueryFingerprintLimit)
}

func newQueryTracer(threshold time.Duration, metrics *Metrics, logger *slog.Logger, fingerprintLimit int) *QueryTracer {
	return &QueryTracer{
		Threshold: threshold,
		Metrics:   metrics,
		Logger:    logger,
		catalog: &slowQueryCatalog{
			fingerprints: make(map[string]struct{}, fingerprintLimit),
			limit:        fingerprintLimit,
		},
	}
}

func (t *QueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryStartedKey{}, queryStarted{
		at:          time.Now(),
		fingerprint: statementFingerprint(data.SQL),
		template:    sqlTemplate(data.SQL),
	})
}

func (t *QueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	started, ok := ctx.Value(queryStartedKey{}).(queryStarted)
	if !ok || t.Threshold <= 0 || time.Since(started.at) < t.Threshold {
		return
	}
	label, first := t.catalog.observe(started.fingerprint)
	if t.Metrics != nil {
		t.Metrics.slowQueries.WithLabelValues(label).Inc()
	}
	if label == slowQueryOverflowLabel {
		return
	}
	if t.Logger != nil {
		if first {
			t.Logger.InfoContext(ctx, "slow database query fingerprint registered", "fingerprint", started.fingerprint, "sql_template", started.template)
		}
		t.Logger.WarnContext(ctx, "slow database query", "fingerprint", started.fingerprint, "duration_ms", time.Since(started.at).Milliseconds(), "result", data.CommandTag.String())
	}
}

func (c *slowQueryCatalog) observe(fingerprint string) (label string, first bool) {
	if c == nil || c.limit <= 0 {
		return slowQueryOverflowLabel, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.fingerprints[fingerprint]; ok {
		return fingerprint, false
	}
	if len(c.fingerprints) >= c.limit {
		return slowQueryOverflowLabel, false
	}
	c.fingerprints[fingerprint] = struct{}{}
	return fingerprint, true
}

func NewDatabasePool(ctx context.Context, cfg config.Config, metrics *Metrics, logger *slog.Logger) (*pgxpool.Pool, error) {
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("database URL is required")
	}
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	poolConfig.MaxConns = cfg.DBMaxConns
	poolConfig.MinConns = cfg.DBMinConns
	poolConfig.ConnConfig.ConnectTimeout = cfg.DBConnectTimeout
	poolConfig.ConnConfig.Tracer = NewQueryTracer(cfg.DBSlowQuery, metrics, logger)
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	metrics.RegisterPool(pool)
	return pool, nil
}

func statementFingerprint(statement string) string {
	digest := sha256.Sum256([]byte(statement))
	return hex.EncodeToString(digest[:6])
}

func sqlTemplate(statement string) string {
	runes := []rune(statement)
	if len(runes) <= maxSQLTemplateRunes {
		return statement
	}
	return string(runes[:maxSQLTemplateRunes-3]) + "..."
}
