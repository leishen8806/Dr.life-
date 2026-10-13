package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/textproto"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr           string
	MetricsAddr        string
	AppEnv             string
	AppRelease         string
	LogLevel           slog.Level
	ShutdownTimeout    time.Duration
	ReadHeaderTimeout  time.Duration
	ReadTimeout        time.Duration
	IdleTimeout        time.Duration
	DatabaseURL        string
	ReportDatabaseURL  string
	DBMaxConns         int32
	DBMinConns         int32
	DBConnectTimeout   time.Duration
	DBSlowQuery        time.Duration
	SentryDSN          string
	RedisURL           string
	CORSAllowedOrigins []string
	WebhookPrefix      string
	TrustedProxyHeader string
	TrustedProxyCIDRs  []string
	RateLimitIP        int
	RateLimitAccount   int
	RateLimitLogin     int
	RateLimitWithdraw  int
	RateLimitAI        int
}

func Load() (Config, error) {
	return loadWithLookup(os.Getenv, os.LookupEnv)
}

func load(getenv func(string) string) (Config, error) {
	return loadWithLookup(getenv, func(key string) (string, bool) {
		value := getenv(key)
		return value, value != ""
	})
}

func loadWithLookup(getenv func(string) string, lookupEnv func(string) (string, bool)) (Config, error) {
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
	metricsAddr := "127.0.0.1:9100"
	if raw, present := lookupEnv("METRICS_ADDR"); present {
		metricsAddr = strings.TrimSpace(raw)
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
	readHeaderSeconds, err := durationSeconds(getenv, "READ_HEADER_TIMEOUT_SECONDS", 10)
	if err != nil {
		return Config{}, err
	}
	readSeconds, err := durationSeconds(getenv, "READ_TIMEOUT_SECONDS", 30)
	if err != nil {
		return Config{}, err
	}
	idleSeconds, err := durationSeconds(getenv, "IDLE_TIMEOUT_SECONDS", 60)
	if err != nil {
		return Config{}, err
	}
	dbMaxConns, err := positiveInt(getenv, "DB_MAX_CONNS", 10)
	if err != nil {
		return Config{}, err
	}
	dbMinConns, err := positiveInt(getenv, "DB_MIN_CONNS", 2)
	if err != nil {
		return Config{}, err
	}
	if dbMinConns > dbMaxConns {
		return Config{}, fmt.Errorf("DB_MIN_CONNS must not exceed DB_MAX_CONNS")
	}
	dbConnectSeconds, err := durationSeconds(getenv, "DB_CONNECT_TIMEOUT_SECONDS", 5)
	if err != nil {
		return Config{}, err
	}
	dbSlowQueryMS, err := positiveInt(getenv, "DB_SLOW_QUERY_MS", 200)
	if err != nil {
		return Config{}, err
	}
	corsOrigins, err := parseOrigins(getenv("CORS_ALLOWED_ORIGINS"))
	if err != nil {
		return Config{}, err
	}
	publicWebhookPrefix, err := parsePublicWebhookPrefix(getenv("PUBLIC_WEBHOOK_PREFIX"))
	if err != nil {
		return Config{}, err
	}
	proxyHeader := strings.TrimSpace(getenv("TRUSTED_PROXY_HEADER"))
	proxyCIDRs, err := parseCIDRs(getenv("TRUSTED_PROXY_CIDRS"))
	if err != nil {
		return Config{}, err
	}
	if proxyHeader != "" && (textproto.CanonicalMIMEHeaderKey(proxyHeader) == "" || len(proxyCIDRs) == 0) {
		return Config{}, fmt.Errorf("TRUSTED_PROXY_HEADER requires a valid header name and TRUSTED_PROXY_CIDRS")
	}
	if proxyHeader == "" && len(proxyCIDRs) != 0 {
		return Config{}, fmt.Errorf("TRUSTED_PROXY_CIDRS requires TRUSTED_PROXY_HEADER")
	}
	limits := make([]int, 5)
	for i, item := range []struct {
		name  string
		value int
	}{{"RATE_LIMIT_IP_PER_MINUTE", 600}, {"RATE_LIMIT_ACCOUNT_PER_MINUTE", 300}, {"RATE_LIMIT_LOGIN_PER_MINUTE", 10}, {"RATE_LIMIT_WITHDRAW_PER_MINUTE", 5}, {"RATE_LIMIT_AI_PER_MINUTE", 30}} {
		limits[i], err = positiveInt(getenv, item.name, item.value)
		if err != nil {
			return Config{}, err
		}
	}

	return Config{
		HTTPAddr:           httpAddr,
		MetricsAddr:        metricsAddr,
		AppEnv:             appEnv,
		AppRelease:         strings.TrimSpace(getenv("APP_RELEASE")),
		LogLevel:           logLevel,
		ShutdownTimeout:    time.Duration(shutdownSeconds) * time.Second,
		ReadHeaderTimeout:  time.Duration(readHeaderSeconds) * time.Second,
		ReadTimeout:        time.Duration(readSeconds) * time.Second,
		IdleTimeout:        time.Duration(idleSeconds) * time.Second,
		DatabaseURL:        strings.TrimSpace(getenv("DATABASE_URL")),
		ReportDatabaseURL:  strings.TrimSpace(getenv("REPORT_DATABASE_URL")),
		DBMaxConns:         int32(dbMaxConns),
		DBMinConns:         int32(dbMinConns),
		DBConnectTimeout:   time.Duration(dbConnectSeconds) * time.Second,
		DBSlowQuery:        time.Duration(dbSlowQueryMS) * time.Millisecond,
		SentryDSN:          strings.TrimSpace(getenv("SENTRY_DSN")),
		RedisURL:           strings.TrimSpace(getenv("REDIS_URL")),
		CORSAllowedOrigins: corsOrigins,
		WebhookPrefix:      publicWebhookPrefix,
		TrustedProxyHeader: proxyHeader,
		TrustedProxyCIDRs:  proxyCIDRs,
		RateLimitIP:        limits[0],
		RateLimitAccount:   limits[1],
		RateLimitLogin:     limits[2],
		RateLimitWithdraw:  limits[3],
		RateLimitAI:        limits[4],
	}, nil
}

func parsePublicWebhookPrefix(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if value == "/" || !strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || value == "/v1" || strings.HasPrefix(value, "/v1/") || strings.ToLower(value) != value || strings.Contains(value, "//") {
		return "", fmt.Errorf("PUBLIC_WEBHOOK_PREFIX must be a lowercase non-v1 path prefix without a trailing slash")
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '/' && char != '-' && char != '_' {
			return "", fmt.Errorf("PUBLIC_WEBHOOK_PREFIX contains an invalid character")
		}
	}
	return value, nil
}

func parseOrigins(raw string) ([]string, error) {
	values, err := commaSeparated(raw)
	if err != nil {
		return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS contains an empty value")
	}
	for _, value := range values {
		parsed, parseErr := url.Parse(value)
		if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS contains an invalid origin")
		}
	}
	return values, nil
}

func parseCIDRs(raw string) ([]string, error) {
	values, err := commaSeparated(raw)
	if err != nil {
		return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS contains an empty value")
	}
	for _, value := range values {
		if _, _, parseErr := net.ParseCIDR(value); parseErr != nil {
			return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS contains an invalid CIDR")
		}
	}
	return values, nil
}

func commaSeparated(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
		if parts[i] == "" {
			return nil, fmt.Errorf("empty list item")
		}
	}
	return parts, nil
}

func positiveInt(getenv func(string) string, name string, defaultValue int) (int, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func durationSeconds(getenv func(string) string, name string, defaultValue int) (int, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}
