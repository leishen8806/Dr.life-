package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
)

var ErrAcquireTimeout = errors.New("database connection acquisition timed out")

func NewPool(ctx context.Context, cfg config.Config, tracers ...pgx.QueryTracer) (*pgxpool.Pool, error) {
	return newPool(ctx, cfg.DatabaseURL, cfg, cfg.DBMaxConns, "5s", false, tracers...)
}

func NewReadOnlyPool(ctx context.Context, cfg config.Config, tracers ...pgx.QueryTracer) (*pgxpool.Pool, error) {
	return newPool(ctx, cfg.ReportDatabaseURL, cfg, cfg.DBReportMaxConns, "30s", true, tracers...)
}

func NewWorkerPool(ctx context.Context, cfg config.Config, tracers ...pgx.QueryTracer) (*pgxpool.Pool, error) {
	return newPool(ctx, cfg.DatabaseURL, cfg, cfg.DBWorkerMaxConns, "60s", false, tracers...)
}

func newPool(ctx context.Context, databaseURL string, cfg config.Config, maxConns int32, statementTimeout string, readOnly bool, tracers ...pgx.QueryTracer) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("database URL is required")
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	poolConfig.MaxConns = maxConns
	poolConfig.MinConns = cfg.DBMinConns
	poolConfig.ConnConfig.ConnectTimeout = cfg.DBConnectTimeout
	poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = statementTimeout
	poolConfig.ConnConfig.RuntimeParams["lock_timeout"] = "3s"
	poolConfig.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "10s"
	if len(tracers) != 0 {
		poolConfig.ConnConfig.Tracer = tracers[0]
	}
	if readOnly {
		poolConfig.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	return pool, nil
}

func Acquire(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration) (*pgxpool.Conn, error) {
	acquireCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := pool.Acquire(acquireCtx)
	if errors.Is(err, context.DeadlineExceeded) && acquireCtx.Err() != nil {
		return nil, fmt.Errorf("%w: %w", ErrAcquireTimeout, context.DeadlineExceeded)
	}
	if err != nil {
		return nil, fmt.Errorf("acquire database connection: %w", err)
	}
	return conn, nil
}

func Ping(pool *pgxpool.Pool) func(context.Context) error {
	return pool.Ping
}
