package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr        string
	AppEnv          string
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
	DatabaseURL     string
	RedisURL        string
}

func Load() (Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Config, error) {
	appEnv := strings.TrimSpace(getenv("APP_ENV"))
	if appEnv == "" {
		return Config{}, fmt.Errorf("APP_ENV is required (dev, test, staging, or prod)")
	}
	switch appEnv {
	case "dev", "test", "staging", "prod":
	default:
		return Config{}, fmt.Errorf("APP_ENV must be one of dev, test, staging, prod")
	}

	httpAddr := strings.TrimSpace(getenv("HTTP_ADDR"))
	if httpAddr == "" {
		httpAddr = ":8080"
	}

	logLevel := slog.LevelInfo
	if raw := strings.TrimSpace(getenv("LOG_LEVEL")); raw != "" {
		switch strings.ToLower(raw) {
		case "debug":
			logLevel = slog.LevelDebug
		case "info":
			logLevel = slog.LevelInfo
		case "warn", "warning":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		default:
			return Config{}, fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error")
		}
	}

	shutdownSeconds := 15
	if raw := strings.TrimSpace(getenv("SHUTDOWN_TIMEOUT_SECONDS")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT_SECONDS must be a positive integer")
		}
		shutdownSeconds = parsed
	}

	return Config{
		HTTPAddr:        httpAddr,
		AppEnv:          appEnv,
		LogLevel:        logLevel,
		ShutdownTimeout: time.Duration(shutdownSeconds) * time.Second,
		DatabaseURL:     strings.TrimSpace(getenv("DATABASE_URL")),
		RedisURL:        strings.TrimSpace(getenv("REDIS_URL")),
	}, nil
}
