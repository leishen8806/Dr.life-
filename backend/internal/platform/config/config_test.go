package config

import (
	"log/slog"
	"testing"
	"time"
)

func TestLoadRequiresAndValidatesAppEnv(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
	}{
		{name: "missing"},
		{name: "invalid", env: map[string]string{"APP_ENV": "production"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := load(lookup(tc.env)); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestLoadDefaultsAndOptionalURLs(t *testing.T) {
	cfg, err := load(lookup(map[string]string{"APP_ENV": "dev"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.MetricsAddr != "127.0.0.1:9100" || cfg.LogLevel != slog.LevelInfo || cfg.ShutdownTimeout != 15*time.Second || cfg.ReadHeaderTimeout != 10*time.Second || cfg.ReadTimeout != 30*time.Second || cfg.IdleTimeout != 60*time.Second || cfg.DBMaxConns != 10 || cfg.DBMinConns != 2 || cfg.DBConnectTimeout != 5*time.Second || cfg.DBSlowQuery != 200*time.Millisecond || cfg.RateLimitIP != 600 || cfg.RateLimitAccount != 300 || cfg.RateLimitLogin != 10 || cfg.RateLimitWithdraw != 5 || cfg.RateLimitAI != 30 || cfg.LedgerReconTimezone != "Asia/Phnom_Penh" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.DatabaseURL != "" || cfg.ReportDatabaseURL != "" || cfg.RedisURL != "" {
		t.Fatalf("optional URLs should be empty: %+v", cfg)
	}
	if len(cfg.CORSAllowedOrigins) != 0 || cfg.WebhookPrefix != "" || cfg.TrustedProxyHeader != "" || len(cfg.TrustedProxyCIDRs) != 0 {
		t.Fatalf("gateway trust defaults should be empty: %+v", cfg)
	}
}

func TestLoadParsesValuesAndRejectsInvalidTimeout(t *testing.T) {
	cfg, err := load(lookup(map[string]string{
		"APP_ENV":                        "staging",
		"HTTP_ADDR":                      "127.0.0.1:9090",
		"METRICS_ADDR":                   "127.0.0.1:9200",
		"APP_RELEASE":                    "test-release",
		"LOG_LEVEL":                      "debug",
		"SHUTDOWN_TIMEOUT_SECONDS":       "8",
		"READ_HEADER_TIMEOUT_SECONDS":    "12",
		"READ_TIMEOUT_SECONDS":           "35",
		"IDLE_TIMEOUT_SECONDS":           "75",
		"DATABASE_URL":                   "postgres://placeholder",
		"REPORT_DATABASE_URL":            "postgres://report-placeholder",
		"DB_MAX_CONNS":                   "20",
		"DB_MIN_CONNS":                   "4",
		"DB_CONNECT_TIMEOUT_SECONDS":     "6",
		"DB_SLOW_QUERY_MS":               "250",
		"SENTRY_DSN":                     "https://public@example.invalid/1",
		"REDIS_URL":                      "redis://placeholder",
		"CORS_ALLOWED_ORIGINS":           "https://miniapp.settlecore.org,http://localhost:5173",
		"PUBLIC_WEBHOOK_PREFIX":          "/webhooks/telegram",
		"TRUSTED_PROXY_HEADER":           "CF-Connecting-IP",
		"TRUSTED_PROXY_CIDRS":            "192.0.2.0/24,2001:db8::/32",
		"RATE_LIMIT_IP_PER_MINUTE":       "100",
		"RATE_LIMIT_ACCOUNT_PER_MINUTE":  "200",
		"RATE_LIMIT_LOGIN_PER_MINUTE":    "9",
		"RATE_LIMIT_WITHDRAW_PER_MINUTE": "4",
		"RATE_LIMIT_AI_PER_MINUTE":       "20",
		"LEDGER_RECON_TIMEZONE":          "UTC",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppEnv != "staging" || cfg.AppRelease != "test-release" || cfg.HTTPAddr != "127.0.0.1:9090" || cfg.MetricsAddr != "127.0.0.1:9200" || cfg.SentryDSN == "" || cfg.LogLevel != slog.LevelDebug || cfg.ShutdownTimeout != 8*time.Second || cfg.ReadHeaderTimeout != 12*time.Second || cfg.ReadTimeout != 35*time.Second || cfg.IdleTimeout != 75*time.Second || cfg.ReportDatabaseURL != "postgres://report-placeholder" || cfg.DBMaxConns != 20 || cfg.DBMinConns != 4 || cfg.DBConnectTimeout != 6*time.Second || cfg.DBSlowQuery != 250*time.Millisecond || len(cfg.CORSAllowedOrigins) != 2 || cfg.WebhookPrefix != "/webhooks/telegram" || cfg.TrustedProxyHeader != "CF-Connecting-IP" || len(cfg.TrustedProxyCIDRs) != 2 || cfg.RateLimitIP != 100 || cfg.RateLimitAccount != 200 || cfg.RateLimitLogin != 9 || cfg.RateLimitWithdraw != 4 || cfg.RateLimitAI != 20 || cfg.LedgerReconTimezone != "UTC" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	for _, raw := range []string{"0", "-1", "abc"} {
		t.Run("timeout="+raw, func(t *testing.T) {
			if _, err := load(lookup(map[string]string{"APP_ENV": "test", "SHUTDOWN_TIMEOUT_SECONDS": raw})); err == nil {
				t.Fatal("expected invalid timeout error")
			}
		})
	}
	for _, key := range []string{"READ_HEADER_TIMEOUT_SECONDS", "READ_TIMEOUT_SECONDS", "IDLE_TIMEOUT_SECONDS"} {
		t.Run("invalid "+key, func(t *testing.T) {
			if _, err := load(lookup(map[string]string{"APP_ENV": "test", key: "0"})); err == nil {
				t.Fatalf("expected invalid %s error", key)
			}
		})
	}
	for _, key := range []string{"DB_MAX_CONNS", "DB_MIN_CONNS", "DB_CONNECT_TIMEOUT_SECONDS", "DB_SLOW_QUERY_MS"} {
		t.Run("invalid "+key, func(t *testing.T) {
			if _, err := load(lookup(map[string]string{"APP_ENV": "test", key: "0"})); err == nil {
				t.Fatalf("expected invalid %s error", key)
			}
		})
	}
	if _, err := load(lookup(map[string]string{"APP_ENV": "test", "DB_MAX_CONNS": "2", "DB_MIN_CONNS": "3"})); err == nil {
		t.Fatal("expected DB_MIN_CONNS greater than DB_MAX_CONNS to fail")
	}
	for name, env := range map[string]map[string]string{
		"bad origin":          {"APP_ENV": "test", "CORS_ALLOWED_ORIGINS": "*"},
		"bad cidr":            {"APP_ENV": "test", "TRUSTED_PROXY_HEADER": "CF-Connecting-IP", "TRUSTED_PROXY_CIDRS": "bad"},
		"header without cidr": {"APP_ENV": "test", "TRUSTED_PROXY_HEADER": "CF-Connecting-IP"},
		"cidr without header": {"APP_ENV": "test", "TRUSTED_PROXY_CIDRS": "127.0.0.1/32"},
		"bad rate":            {"APP_ENV": "test", "RATE_LIMIT_IP_PER_MINUTE": "0"},
		"webhook v1":          {"APP_ENV": "test", "PUBLIC_WEBHOOK_PREFIX": "/v1/public-hook"},
		"webhook root":        {"APP_ENV": "test", "PUBLIC_WEBHOOK_PREFIX": "/"},
		"webhook uppercase":   {"APP_ENV": "test", "PUBLIC_WEBHOOK_PREFIX": "/Webhooks/telegram"},
		"webhook encoding":    {"APP_ENV": "test", "PUBLIC_WEBHOOK_PREFIX": "/webhooks/%2f"},
		"bad recon timezone":  {"APP_ENV": "test", "LEDGER_RECON_TIMEZONE": "Mars/Olympus"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := load(lookup(env)); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestExplicitEmptyMetricsAddressDisablesListener(t *testing.T) {
	values := map[string]string{"APP_ENV": "test", "METRICS_ADDR": ""}
	cfg, err := loadWithLookup(lookup(values), func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetricsAddr != "" {
		t.Fatalf("MetricsAddr = %q, want disabled", cfg.MetricsAddr)
	}
}

func lookup(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}
