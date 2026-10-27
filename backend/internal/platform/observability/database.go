package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
	platformdb "github.com/leishen8806/Dr.life-/backend/internal/platform/db"
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
	pool, err := platformdb.NewPool(ctx, cfg, NewQueryTracer(cfg.DBSlowQuery, metrics, logger))
	if err != nil {
		return nil, err
	}
	metrics.RegisterPool("oltp", pool)
	return pool, nil
}

func NewReportPool(ctx context.Context, cfg config.Config, metrics *Metrics, logger *slog.Logger) (*pgxpool.Pool, error) {
	pool, err := platformdb.NewReadOnlyPool(ctx, cfg, NewQueryTracer(cfg.DBSlowQuery, metrics, logger))
	if err != nil {
		return nil, err
	}
	metrics.RegisterPool("report", pool)
	return pool, nil
}

func NewWorkerPool(ctx context.Context, cfg config.Config, metrics *Metrics, logger *slog.Logger) (*pgxpool.Pool, error) {
	pool, err := platformdb.NewWorkerPool(ctx, cfg, NewQueryTracer(cfg.DBSlowQuery, metrics, logger))
	if err != nil {
		return nil, err
	}
	metrics.RegisterPool("worker", pool)
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
