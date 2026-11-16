// Package config loads Lanvory configuration from environment variables.
package config

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Config is the resolved runtime configuration.
type Config struct {
	// Addr is the HTTP listen address, e.g. ":8090".
	Addr string
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// CookieSecure marks session/CSRF cookies Secure (enable behind HTTPS).
	CookieSecure bool
	// SessionTTL is the sliding session lifetime.
	SessionTTL time.Duration
	// Env is "production" or "development" (controls log format and CORS-free dev proxy).
	Env string
	// SeedMode: empty for production; "dev" applies the homelab seed after migrations.
	SeedMode string
	// SecretKey (LV_SECRET_KEY) encrypts integration credentials at rest.
	SecretKey string
}

// Load reads configuration from the environment with sensible local defaults.
func Load() Config {
	return Config{
		Addr:         env("LV_ADDR", "0.0.0.0:8090"),
		DatabaseURL:  firstEnv("postgres://lanvory:lanvory@localhost:5432/lanvory?sslmode=disable", "LV_DATABASE_URL", "DATABASE_URL"),
		CookieSecure: envBool("LV_COOKIE_SECURE", false),
		SessionTTL:   envDuration("LV_SESSION_TTL", 14*24*time.Hour),
		Env:          env("LV_ENV", "production"),
		SeedMode:     env("LV_SEED_MODE", ""),
		SecretKey:    env("LV_SECRET_KEY", ""),
	}
}

// firstEnv returns the first non-empty variable among keys, falling back to `fallback`.
func firstEnv(fallback string, keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return fallback
}

// Production reports whether the server runs in production mode.
func (c Config) Production() bool { return c.Env == "production" }

// Logger builds the process logger: JSON in production, readable text in development.
func (c Config) Logger() *slog.Logger {
	level := slog.LevelInfo
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if c.Production() {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
