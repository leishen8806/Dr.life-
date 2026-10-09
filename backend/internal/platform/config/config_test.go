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
	if cfg.HTTPAddr != ":8080" || cfg.LogLevel != slog.LevelInfo || cfg.ShutdownTimeout != 15*time.Second || cfg.ReadHeaderTimeout != 10*time.Second || cfg.ReadTimeout != 30*time.Second || cfg.IdleTimeout != 60*time.Second {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.DatabaseURL != "" || cfg.RedisURL != "" {
		t.Fatalf("optional URLs should be empty: %+v", cfg)
	}
}

func TestLoadParsesValuesAndRejectsInvalidTimeout(t *testing.T) {
	cfg, err := load(lookup(map[string]string{
		"APP_ENV":                     "staging",
		"HTTP_ADDR":                   "127.0.0.1:9090",
		"LOG_LEVEL":                   "debug",
		"SHUTDOWN_TIMEOUT_SECONDS":    "8",
		"READ_HEADER_TIMEOUT_SECONDS": "12",
		"READ_TIMEOUT_SECONDS":        "35",
		"IDLE_TIMEOUT_SECONDS":        "75",
		"DATABASE_URL":                "postgres://placeholder",
		"REDIS_URL":                   "redis://placeholder",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppEnv != "staging" || cfg.HTTPAddr != "127.0.0.1:9090" || cfg.LogLevel != slog.LevelDebug || cfg.ShutdownTimeout != 8*time.Second || cfg.ReadHeaderTimeout != 12*time.Second || cfg.ReadTimeout != 35*time.Second || cfg.IdleTimeout != 75*time.Second {
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
}

func lookup(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}
