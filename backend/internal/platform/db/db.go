package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
)

func NewPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	return newPool(ctx, cfg.DatabaseURL, cfg, false)
}

func NewReadOnlyPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	return newPool(ctx, cfg.ReportDatabaseURL, cfg, true)
}

func newPool(ctx context.Context, databaseURL string, cfg config.Config, readOnly bool) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("database URL is required")
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	poolConfig.MaxConns = cfg.DBMaxConns
	poolConfig.MinConns = cfg.DBMinConns
	poolConfig.ConnConfig.ConnectTimeout = cfg.DBConnectTimeout
	if readOnly {
		poolConfig.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	return pool, nil
}

func Ping(pool *pgxpool.Pool) func(context.Context) error {
	return pool.Ping
}
